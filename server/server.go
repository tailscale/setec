// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// Package server implements the setec secrets server.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"expvar"
	"fmt"
	"net/http"
	"net/netip"

	"github.com/tailscale/setec/acl"
	"github.com/tailscale/setec/audit"
	"github.com/tailscale/setec/db"
	"github.com/tailscale/setec/types/api"
	"github.com/tink-crypto/tink-go/v2/tink"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/metrics"
	"tailscale.com/tailcfg"
)

// Config is the configuration for a Server.
type Config struct {
	// DBPath is the path to the secrets database.
	DBPath string
	// Key is the AEAD used to encrypt/decrypt the database.
	Key tink.AEAD
	// AuditLog is the writer to use for audit logs.
	AuditLog *audit.Writer
	// WhoIs is a function that reports an identity for a client IP
	// address. Outside of tests, it will be the WhoIs of a Tailscale
	// LocalClient.
	WhoIs func(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error)
	// Mux is the http.ServeMux on which the server registers its HTTP
	// handlers.
	Mux *http.ServeMux
}

// Server is a secrets HTTP server.
type Server struct {
	db    *db.DB
	whois func(context.Context, string) (*apitype.WhoIsResponse, error)

	// Metrics
	countCalls             *metrics.LabelMap // :: method name → count
	countCallBadRequest    *metrics.LabelMap // :: method name → count
	countCallForbidden     *metrics.LabelMap // :: method name → count
	countCallInternalError *metrics.LabelMap // :: method name → count
}

// New creates a secret server and makes it ready to serve.
func New(cfg Config) (*Server, error) {
	db, err := db.Open(cfg.DBPath, cfg.Key, cfg.AuditLog)
	if err != nil {
		return nil, fmt.Errorf("opening DB: %w", err)
	}

	ret := &Server{
		db:    db,
		whois: cfg.WhoIs,

		countCalls:             &metrics.LabelMap{Label: "method"},
		countCallBadRequest:    &metrics.LabelMap{Label: "method"},
		countCallForbidden:     &metrics.LabelMap{Label: "method"},
		countCallInternalError: &metrics.LabelMap{Label: "method"},
	}
	cfg.Mux.HandleFunc("/api/list", ret.list)
	cfg.Mux.HandleFunc("/api/get", ret.get)
	cfg.Mux.HandleFunc("/api/info", ret.info)
	cfg.Mux.HandleFunc("/api/put", ret.put)
	cfg.Mux.HandleFunc("/api/set-active", ret.setActive)
	cfg.Mux.HandleFunc("/api/delete", ret.deleteSecret)
	cfg.Mux.HandleFunc("/api/delete-version", ret.deleteVersion)

	return ret, nil
}

// Metrics returns a collection of metrics for s. THe caller is responsible for
// publishing the result to the metrics exporter.
func (s *Server) Metrics() expvar.Var {
	m := new(metrics.Set)
	m.Set("counter_api_calls", s.countCalls)
	m.Set("counter_api_bad_request", s.countCallBadRequest)
	m.Set("counter_api_forbidden", s.countCallForbidden)
	m.Set("counter_api_internal_error", s.countCallInternalError)
	return m
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	serveJSON(s, w, r, func(req api.ListRequest, id db.Caller) ([]*api.SecretInfo, error) {
		return s.db.List(id)
	})
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	serveJSON(s, w, r, func(req api.GetRequest, id db.Caller) (*api.SecretValue, error) {
		if req.Version != 0 {
			return s.db.GetVersion(id, req.Name, req.Version)
		}
		return s.db.Get(id, req.Name)
	})
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	serveJSON(s, w, r, func(req api.InfoRequest, id db.Caller) (*api.SecretInfo, error) {
		return s.db.Info(id, req.Name)
	})
}

func (s *Server) put(w http.ResponseWriter, r *http.Request) {
	serveJSON(s, w, r, func(req api.PutRequest, id db.Caller) (api.SecretVersion, error) {
		return s.db.Put(id, req.Name, req.Value)
	})
}

func (s *Server) setActive(w http.ResponseWriter, r *http.Request) {
	serveJSON(s, w, r, func(req api.SetActiveRequest, id db.Caller) (struct{}, error) {
		if err := s.db.SetActiveVersion(id, req.Name, req.Version); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, nil
	})
}

func (s *Server) deleteVersion(w http.ResponseWriter, r *http.Request) {
	serveJSON(s, w, r, func(req api.DeleteVersionRequest, id db.Caller) (struct{}, error) {
		err := s.db.DeleteVersion(id, req.Name, req.Version)
		return struct{}{}, err
	})
}

func (s *Server) deleteSecret(w http.ResponseWriter, r *http.Request) {
	serveJSON(s, w, r, func(req api.DeleteRequest, id db.Caller) (struct{}, error) {
		err := s.db.Delete(id, req.Name)
		return struct{}{}, err
	})
}

// ACLCap is the capability name used for setec ACL permissions.
const ACLCap tailcfg.PeerCapability = "https://tailscale.com/cap/secrets"

// getIdentity extracts identity and permissions from an HTTP request.
func (s *Server) getIdentity(r *http.Request) (id db.Caller, err error) {
	addrPort, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return db.Caller{}, fmt.Errorf("parsing RemoteAddr %q: %w", r.RemoteAddr, err)
	}

	who, err := s.whois(r.Context(), r.RemoteAddr)
	if err != nil {
		return db.Caller{}, fmt.Errorf("calling WhoIs: %w", err)
	}

	if who.Node.IsTagged() {
		// TODO: when we have audit logs, put together a better identity struct with more info
		id.Principal.Tags = who.Node.Tags
	} else if who.UserProfile.LoginName != "" {
		id.Principal.User = who.UserProfile.LoginName
	} else {
		return db.Caller{}, errors.New("failed to find caller identity")
	}
	id.Principal.IP = addrPort.Addr()
	id.Principal.Hostname = who.Node.Name

	id.Permissions, err = tailcfg.UnmarshalCapJSON[acl.Rule](who.CapMap, ACLCap)
	if err != nil {
		return db.Caller{}, fmt.Errorf("unmarshaling peer capabilities: %w", err)
	}

	return id, nil
}

// serveJSON calls fn to handle a JSON API request. fn is invoked with
// the request body decoded into r, and from set to the Tailscale
// identity of the caller. The response returned from fn is serialized
// as JSON back to the client.
func serveJSON[REQ any, RESP any](s *Server, w http.ResponseWriter, r *http.Request, fn func(r REQ, id db.Caller) (RESP, error)) {
	apiMethod := r.URL.Path
	s.countCalls.Add(apiMethod, 1)

	if r.Method != "POST" {
		s.countCallBadRequest.Add(apiMethod, 1)
		http.Error(w, "only POST requests allowed", http.StatusBadRequest)
		return
	}
	if c := r.Header.Get("Content-Type"); c != "application/json" {
		s.countCallBadRequest.Add(apiMethod, 1)
		http.Error(w, "request body must be json", http.StatusBadRequest)
		return
	}
	// Block any attempt to access the API from browsers. Longer term
	// we want a more carefully thought out browser security config
	// that does permit legitimate API use from a browser, but for
	// now, require that a specific Sec-* header must be set. Sec- is
	// one of the "forbidden header" prefixes that code running in
	// browsers cannot set, so no CSRF or use of JS fetch APIs can
	// satisfy this condition.
	if h := r.Header.Get("Sec-X-Tailscale-No-Browsers"); h != "setec" {
		s.countCallForbidden.Add(apiMethod, 1)
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	id, err := s.getIdentity(r)
	if err != nil {
		s.countCallInternalError.Add(apiMethod, 1)
		http.Error(w, "unable to identify caller", http.StatusInternalServerError)
		return
	}

	var req REQ
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.countCallBadRequest.Add(apiMethod, 1)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	resp, err := fn(req, id)
	if errors.Is(err, db.ErrAccessDenied) {
		s.countCallForbidden.Add(apiMethod, 1)
		http.Error(w, "access denied", http.StatusForbidden)
		return
	} else if err != nil {
		s.countCallInternalError.Add(apiMethod, 1)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	bs, err := json.Marshal(resp)
	if err != nil {
		s.countCallInternalError.Add(apiMethod, 1)
		http.Error(w, "failed to encode respnse", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(bs)
}

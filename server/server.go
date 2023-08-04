// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// Package server implements the setec secrets server.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/netip"

	"github.com/tailscale/setec/acl"
	"github.com/tailscale/setec/db"
	"github.com/tailscale/setec/types/api"
	"github.com/tink-crypto/tink-go/v2/tink"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/tailcfg"
)

// Config is the configuration for a Server.
type Config struct {
	// DBPath is the path to the secrets database.
	DBPath string
	// Key is the AEAD used to encrypt/decrypt the database.
	Key tink.AEAD
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
}

// New creates a secret server and makes it ready to serve.
func New(cfg Config) (*Server, error) {
	db, err := db.Open(cfg.DBPath, cfg.Key)
	if err != nil {
		return nil, fmt.Errorf("opening DB: %w", err)
	}

	ret := &Server{
		db:    db,
		whois: cfg.WhoIs,
	}
	cfg.Mux.HandleFunc("/api/list", ret.list)
	cfg.Mux.HandleFunc("/api/get", ret.get)
	cfg.Mux.HandleFunc("/api/info", ret.info)
	cfg.Mux.HandleFunc("/api/put", ret.put)
	cfg.Mux.HandleFunc("/api/set-active", ret.setActive)

	return ret, nil
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

const aclCap tailcfg.PeerCapability = "https://tailscale.com/cap/secrets"

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

	id.Permissions, err = tailcfg.UnmarshalCapJSON[acl.Rule](who.CapMap, aclCap)
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
	if r.Method != "POST" {
		http.Error(w, "only POST requests allowed", http.StatusBadRequest)
		return
	}
	if c := r.Header.Get("Content-Type"); c != "application/json" {
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
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	id, err := s.getIdentity(r)
	if err != nil {
		http.Error(w, "unable to identify caller", http.StatusInternalServerError)
		return
	}

	var req REQ
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Print(err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	resp, err := fn(req, id)
	if errors.Is(err, db.ErrAccessDenied) {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	} else if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	bs, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, "failed to encode respnse", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(bs)
}

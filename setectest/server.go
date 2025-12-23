// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// Package setectest implements a wrapper around setec types for testing.
//
// # Usage
//
//	// Construct a database and add some secrets.
//	db := setecttest.NewDB(t, nil)  // nil for default options
//	db.MustPut(db.Superuser, "name", "value")
//
//	// Construct a test server.
//	ss := setectest.NewServer(t, db, &setectest.ServerOptions{
//	  WhoIs: setectest.AllAccess,
//	})
//
//	// Hook up the Server to the httptest package.
//	hs := httptest.NewServer(ss.Mux)
package setectest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/tailscale/setec/acl"
	"github.com/tailscale/setec/audit"
	"github.com/tailscale/setec/server"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/tailcfg"
)

// Server is a wrapper to support running a standalone setec Server for unit
// tests with the testing package.
type Server struct {
	Actual *server.Server // the underlying server
	Mux    *http.ServeMux // the serving mux populated by the server
}

// ServerOptions are options for constructing a test server.
// A nil *ServerOptions is ready for use and provides defaults as described.
type ServerOptions struct {
	// WhoIs is a function implementing the corresponding method of a local
	// client. If nil, setectest.AllAccess is used by default.
	WhoIs func(context.Context, string) (*apitype.WhoIsResponse, error)

	// AuditLog is where audit logs are written; if nil, audit logs are
	// discarded without error.
	AuditLog *audit.Writer
}

func (o *ServerOptions) whoIs() func(context.Context, string) (*apitype.WhoIsResponse, error) {
	if o == nil || o.WhoIs == nil {
		return AllAccess
	}
	return o.WhoIs
}

func (o *ServerOptions) auditLog() *audit.Writer {
	if o == nil || o.AuditLog == nil {
		return audit.New(io.Discard)
	}
	return o.AuditLog
}

// NewServer constructs a new Server that reads data from db and persists for
// the duration of the test and subtests governed by t. When t ends, the server
// and its database are cleaned up. If opts == nil, default options are used
// (see ServerOptions).
func NewServer(t *testing.T, db *DB, opts *ServerOptions) *Server {
	t.Helper()
	mux := http.NewServeMux()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s, err := server.New(ctx, server.Config{
		DB:       db.Actual,
		AuditLog: opts.auditLog(),
		WhoIs:    opts.whoIs(),
		Mux:      mux,
	})
	if err != nil {
		t.Fatalf("Creating new server: %v", err)
	}
	return &Server{Actual: s, Mux: mux}
}

// allAccessCap is an encoded JSON capability for full access to all secrets.
var allAccessCap []tailcfg.RawMessage

func init() {
	rule, err := json.Marshal(acl.Rule{
		Action: []acl.Action{
			acl.ActionGet, acl.ActionInfo, acl.ActionPut, acl.ActionCreateVersion, acl.ActionActivate, acl.ActionDelete,
		},
		Secret: []acl.Secret{"*"},
	})
	if err != nil {
		panic(err)
	}
	allAccessCap = []tailcfg.RawMessage{tailcfg.RawMessage(rule)}
}

// AllAccess is a WhoIs function implementation that returns a successful
// response containing a capability for full access to all secrets.
func AllAccess(ctx context.Context, addr string) (*apitype.WhoIsResponse, error) {
	return &apitype.WhoIsResponse{
		Node: &tailcfg.Node{Name: "example.com"},
		UserProfile: &tailcfg.UserProfile{
			ID: 666, LoginName: "user@example.com", DisplayName: "Example User",
		},
		CapMap: tailcfg.PeerCapMap{server.ACLCap: allAccessCap},
	}, nil
}

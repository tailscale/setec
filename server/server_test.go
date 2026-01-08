// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/tailscale/setec/acl"
	"github.com/tailscale/setec/audit"
	"github.com/tailscale/setec/client/setec"
	"github.com/tailscale/setec/db"
	"github.com/tailscale/setec/internal/tinktestutil"
	"github.com/tailscale/setec/server"
	"github.com/tailscale/setec/setectest"
	"github.com/tailscale/setec/types/api"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/tailcfg"
)

func TestNew(t *testing.T) {
	ctx := t.Context()
	t.Run("NoDB", func(t *testing.T) {
		d, err := server.New(ctx, server.Config{})
		if err == nil {
			t.Errorf("New with no DB: got %+v, want error", d)
		}
	})
	t.Run("PathKey", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "test.db")
		_, err := server.New(ctx, server.Config{
			DBPath:   path,
			Key:      &tinktestutil.DummyAEAD{Name: t.Name()},
			AuditLog: audit.New(io.Discard),
			Mux:      http.NewServeMux(),
		})
		if err != nil {
			t.Errorf("New: unexpected error: %v", err)
		}
	})
	t.Run("DB", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "test.db")
		kdb, err := db.Open(path, &tinktestutil.DummyAEAD{Name: t.Name()}, audit.New(io.Discard))
		if err != nil {
			t.Fatalf("Open database: %v", err)
		}
		if _, err := server.New(ctx, server.Config{
			DB:  kdb,
			Mux: http.NewServeMux(),
		}); err != nil {
			t.Errorf("New: unexpected error: %v", err)
		}
	})
}

func TestServerGetChanged(t *testing.T) {
	d := setectest.NewDB(t, nil)
	v1 := d.MustPut(d.Superuser, "test", "v1") // active
	v2 := d.MustPut(d.Superuser, "test", "v2")

	ss := setectest.NewServer(t, d, nil)
	hs := httptest.NewServer(ss.Mux)
	defer hs.Close()

	ctx := t.Context()
	cli := setec.Client{Server: hs.URL, DoHTTP: hs.Client().Do}

	// Case 1: Fetch the active value of the secret (v1).
	sv1, err := cli.Get(ctx, "test")
	if err != nil {
		t.Fatalf("Get test: %v", err)
	} else if sv1.Version != v1 {
		t.Errorf("Get test: got version %v, want %v", sv1.Version, v1)
	}

	// Case 2: Fetch only if the value changed (which it did not).
	sv2, err := cli.GetIfChanged(ctx, "test", sv1.Version)
	if !errors.Is(err, api.ErrValueNotChanged) {
		t.Errorf("GetIfChanged: got (%v, %v), want %v", sv2, err, api.ErrValueNotChanged)
	}

	// Now change the value on the server...
	if err := cli.Activate(ctx, "test", v2); err != nil {
		t.Fatalf("SetActiveVersion %v: unexpected error: %v", v2, err)
	}

	// Case 3: Fetch only if the value changed (which it did).
	sv3, err := cli.GetIfChanged(ctx, "test", sv1.Version)
	if err != nil {
		t.Errorf("GetIfChanged: unexpected error: %v", err)
	} else if sv3.Version != v2 {
		t.Errorf("GetIfChanged: got version %v, want %v", sv3.Version, v2)
	}
}

func TestServerStatus(t *testing.T) {
	d := setectest.NewDB(t, nil)
	ov1 := d.MustPut(d.Superuser, "ok/test", "v1") // active
	ov2 := d.MustPut(d.Superuser, "ok/test", "v2")
	nv1 := d.MustPut(d.Superuser, "no/test", "no") // active

	// Synthesize a selective access capability that permits read of secrets
	// beginning with "ok/".
	rule, err := json.Marshal(acl.Rule{
		Action: []acl.Action{acl.ActionGet, acl.ActionInfo, acl.ActionDelete},
		Secret: []acl.Secret{"ok/*"},
	})
	if err != nil {
		t.Fatalf("Create access grant: %v", err)
	}
	whois := &apitype.WhoIsResponse{
		Node: &tailcfg.Node{Name: "example.com"},
		UserProfile: &tailcfg.UserProfile{
			ID: 31337, LoginName: "elite@example.com", DisplayName: "Leet Q. Haxor",
		},
		CapMap: tailcfg.PeerCapMap{server.ACLCap: []tailcfg.RawMessage{tailcfg.RawMessage(rule)}},
	}

	ss := setectest.NewServer(t, d, &setectest.ServerOptions{
		WhoIs: func(context.Context, string) (*apitype.WhoIsResponse, error) {
			return whois, nil
		},
	})
	hs := httptest.NewServer(ss.Mux)
	defer hs.Close()

	ctx := t.Context()
	cli := setec.Client{Server: hs.URL, DoHTTP: hs.Client().Do}

	// Note: Conditional get is exercised by TestServerGetChanged above.

	// Case 1: Access denied for get of no/test.
	if sv, err := cli.GetVersion(ctx, "no/test", nv1); !errors.Is(err, api.ErrAccessDenied) {
		t.Errorf("GetVersion %v: got (%v, %v), want error %v", nv1, sv, err, api.ErrAccessDenied)
	}

	// Case 2: Not found for get of non-existing ok/test version.
	if sv, err := cli.GetVersion(ctx, "ok/test", ov2+1); !errors.Is(err, api.ErrNotFound) {
		t.Errorf("GetVersion %v: got (%v, %v), want error %v", ov2+1, sv, err, api.ErrNotFound)
	}

	// Case 3: Access denied for put of ok/test version.
	if sv, err := cli.Put(ctx, "ok/test", []byte("ohai")); !errors.Is(err, api.ErrAccessDenied) {
		t.Errorf("Put ok/test: got (%v, %v), want error %v", sv, err, api.ErrAccessDenied)
	}

	// Case 4: Internal error for delete of ok/test active version.
	if err := cli.DeleteVersion(ctx, "ok/test", ov1); err == nil {
		t.Errorf("DeleteVersion %v: unexpected success", ov1)
	} else {
		t.Logf("DeleteVersion %v: got expected error: %v", ov1, err)
	}

	// Case 5: Success for delete of ok/test inactive version.
	if err := cli.DeleteVersion(ctx, "ok/test", ov2); err != nil {
		t.Errorf("DeleteVersion %v: unexpected error %v", ov2, err)
	}
}

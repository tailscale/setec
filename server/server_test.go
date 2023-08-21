// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package server_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/tailscale/setec/client/setec"
	"github.com/tailscale/setec/setectest"
	"github.com/tailscale/setec/types/api"
)

func TestServerGetChanged(t *testing.T) {
	d := setectest.NewDB(t, nil)
	v1 := d.MustPut(d.Superuser, "test", "v1") // active
	v2 := d.MustPut(d.Superuser, "test", "v2")

	ss := setectest.NewServer(t, d, nil)
	hs := httptest.NewServer(ss.Mux)
	defer hs.Close()

	ctx := context.Background()
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

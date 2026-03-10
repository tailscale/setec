// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package setec_test

import (
	"net/http/httptest"
	"testing"

	"github.com/tailscale/setec/client/setec"
	"github.com/tailscale/setec/setectest"
	"github.com/tailscale/setec/types/api"
)

func TestKeyring(t *testing.T) {
	d := setectest.NewDB(t, nil)
	v1 := d.MustPut(d.Superuser, "apple", "a1")
	v2 := d.MustPut(d.Superuser, "apple", "a2")
	v3 := d.MustPut(d.Superuser, "apple", "a3")
	d.MustActivate(d.Superuser, "apple", v3)

	ts := setectest.NewServer(t, d, nil)
	hs := httptest.NewServer(ts.Mux)
	defer hs.Close()

	cli := setec.Client{Server: hs.URL, DoHTTP: hs.Client().Do}

	r, err := cli.GetKeyring(t.Context(), "apple")
	if err != nil {
		t.Fatalf("GetKeyring failed: %v", err)
	}

	mustActive := func(wantV api.SecretVersion, want string) {
		gotV, data := r.Active()
		if gotV != wantV || string(data) != want {
			t.Errorf("Active: got %v, %q; want %v, %q", gotV, data, wantV, want)
		}
	}
	mustGet := func(v api.SecretVersion, want string) {
		got, ok := r.Get(v)
		if !ok || string(got) != want {
			t.Errorf("Get(%v): got %q, %v; want %q, %v", v, got, ok, want, true)
		}
	}
	mustNotSee := func(v api.SecretVersion) {
		got, ok := r.Get(v)
		if ok || string(got) != "" {
			t.Errorf(`Get(%v): got %q, %v; want "", true`, v, got, ok)
		}
	}

	// Verify that the active version is the one we expect.
	mustActive(v3, "a3")

	// Verify that we can fetch the other versions.
	mustGet(v1, "a1")
	mustGet(v2, "a2")

	// Verify that we cannot fetch a hitherto unseen version.
	mustNotSee(4)
	mustNotSee(999)

	// Add a new version. Until we do an update we should not see it yet.
	v4 := d.MustPut(d.Superuser, "apple", "a4")
	mustNotSee(v4) // yet

	// Now do an update, and verify that we see the new version.
	if err := r.Update(t.Context()); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	mustGet(v4, "a4")

	// Note, however, that the new version is not active yet.
	mustActive(v3, "a3")

	// Activate the new version, update, and verify we see that change.
	d.MustActivate(d.Superuser, "apple", v4)
	if err := r.Update(t.Context()); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	mustActive(v4, "a4")
}

// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package setec

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tailscale/setec/setectest"
)

func TestWatcher(t *testing.T) {
	d := setectest.NewDB(t, nil)
	d.MustPut(d.Superuser, "green", "eggs and ham") // active
	v2 := d.MustPut(d.Superuser, "green", "grow the rushes oh")

	ts := setectest.NewServer(t, d, nil)
	hs := httptest.NewServer(ts.Mux)
	defer hs.Close()

	ctx := context.Background()
	cli := Client{Server: hs.URL, DoHTTP: hs.Client().Do}

	pollTicker := setectest.NewFakeTicker()
	st, err := NewStore(ctx, StoreConfig{
		Client:     cli,
		Secrets:    []string{"green"},
		PollTicker: pollTicker,
	})
	if err != nil {
		t.Fatalf("NewStore: unexpected error: %v", err)
	}
	defer st.Close()

	// With lookups disabled, an unknown watcher reports an error.
	if w, err := st.lookupWatcher(ctx, "nonesuch"); err == nil {
		t.Errorf("Lookup: got %v, want error", w)
	}

	// Observe the initial value of the secret.
	w, err := st.lookupWatcher(ctx, "green")
	if err != nil {
		t.Errorf("Initial value: unexpected error: %v", err)
	} else if got, want := string(w.Get()), "eggs and ham"; got != want {
		t.Errorf("Initial value: got %q, want %q", got, want)
	}

	// The secret gets updated...
	if err := cli.Activate(ctx, "green", v2); err != nil {
		t.Fatalf("Activate to %v: unexpected error: %v", v2, err)
	}

	// The next poll occurs...
	pollTicker.Poll()

	// The watcher should get notified in a timely manner.
	select {
	case <-w.Ready():
		t.Logf("✓ A new version of the secret is available")
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for a watcher update")
	}

	if got, want := string(w.Get()), "grow the rushes oh"; got != want {
		t.Errorf("Updated value: got %q, want %q", got, want)
	}

	// With no updates, the watchers should not appear ready.
	select {
	case <-w.Ready():
		t.Error("Watcher is unexpectedly ready after no update")
	case <-time.After(100 * time.Millisecond):
		// OK
	}
}

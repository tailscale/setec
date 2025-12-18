// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package setec_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tailscale/setec/client/setec"
	"github.com/tailscale/setec/setectest"
	"github.com/tailscale/setec/types/api"
)

const testSecrets = `{
  "big/bad/apple": {
     "secret": {"Version": 1, "Value": "c2F1Y2U="}
  },
  "pear": {
     "unrelated": true
  },
  "plum": {
     "secret": {"Version": 2, "TextValue": "tartlet"}
  },
  "little/tasty/cherry": {
     "secret": {"Version": 3, "Value": "cGll"}
  },
  "": {"secret": {"Version": 10, "Value": "dW5zZWVu"}},
  "durian": {
     "secret": {"Version": 0}
  }
}`

func TestFileStore(t *testing.T) {
	secPath := filepath.Join(t.TempDir(), "secrets.json")
	if err := os.WriteFile(secPath, []byte(testSecrets), 0600); err != nil {
		t.Fatalf("Write test data: %v", err)
	}

	ctx := context.Background()
	fc, err := setec.NewFileClient(secPath)
	if err != nil {
		t.Fatalf("NewFileClient: unexpected error: %v", err)
	}

	// Verify that the client implementation does what it should.
	t.Run("Client", func(t *testing.T) {
		// Get a value we expect.
		if v, err := fc.Get(ctx, "big/bad/apple"); err != nil {
			t.Errorf("Get apple: unexpected error: %v", err)
		} else if string(v.Value) != "sauce" {
			t.Errorf("Get apple: got %q, want sauce", v.Value)
		}

		// Get a value we don't expect.
		if v, err := fc.Get(ctx, "pear"); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("Get pear: got (%v, %v), want %v", v, err, api.ErrNotFound)
		}
		if v, err := fc.Get(ctx, "durian"); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("Get durian: got (%v, %v), want %v", v, err, api.ErrNotFound)
		}

		// Get a value that has not changed.
		if v, err := fc.GetIfChanged(ctx, "little/tasty/cherry", 3); !errors.Is(err, api.ErrValueNotChanged) {
			t.Errorf("GetIfChanged cherry 3: got (%v, %v), want %v", v, err, api.ErrValueNotChanged)
		}

		// Get a value that has changed.
		if v, err := fc.GetIfChanged(ctx, "little/tasty/cherry", 2); err != nil {
			t.Errorf("GetIfChanged cherry 2: unexpected error: %v", err)
		} else if string(v.Value) != "pie" {
			t.Errorf("Get cherry 2: got %q, want pie", v.Value)
		}
	})

	// Verify that a store using the FileClient works.
	t.Run("Store", func(t *testing.T) {
		st, err := setec.NewStore(ctx, setec.StoreConfig{
			Client:       fc,
			Secrets:      []string{"big/bad/apple", "plum", "little/tasty/cherry"},
			PollInterval: -1,
		})
		if err != nil {
			t.Fatalf("NewStore: unexpected error: %v", err)
		}
		defer st.Close()
		checkSecretValue(t, st, "big/bad/apple", "sauce")
		checkSecretValue(t, st, "plum", "tartlet")
		checkSecretValue(t, st, "little/tasty/cherry", "pie")
	})

	// Verify that a store using the FileClient gives up at startup if secrets
	// are missing, instead of blocking in a retry loop.
	t.Run("Missing", func(t *testing.T) {
		st, err := setec.NewStore(ctx, setec.StoreConfig{
			Client:  fc,
			Secrets: []string{"big/bad/apple", "pear", "red/cabbage"},
		})
		const want = "2 unavailable secrets"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("NewStore: got (%v, %v), want error %q", st, err, want)
		} else {
			t.Logf("Got expected error: %v", err)
		}
	})
}

func TestFileClientCacheCompatibility(t *testing.T) {
	// Verify that the FileClient is able to consume the format written by the
	// FileCache. To do this, create a store and force it to generate its cache,
	// then open a new store that reads that cache via a FileClient.

	d := setectest.NewDB(t, nil)
	d.MustPut(d.Superuser, "apple", "a1")  // active
	d.MustPut(d.Superuser, "apple", "a2")  // present but not (yet) active
	d.MustPut(d.Superuser, "pear", "p1")   // active
	d.MustPut(d.Superuser, "cherry", "c1") // present but not (any longer) active
	d.MustActivate(d.Superuser, "cherry", d.MustPut(d.Superuser, "cherry", "c2"))

	// Set up the file cache.
	cpath := filepath.Join(t.TempDir(), "cache.json")
	fcache, err := setec.NewFileCache(cpath)
	if err != nil {
		t.Fatalf("Create file cache: %v", err)
	}

	ts := setectest.NewServer(t, d, nil)
	hs := httptest.NewServer(ts.Mux)
	defer hs.Close()

	st, err := setec.NewStore(t.Context(), setec.StoreConfig{
		Client:  setec.Client{Server: hs.URL, DoHTTP: hs.Client().Do},
		Cache:   fcache,
		Secrets: []string{"apple", "pear", "cherry"},
		Logf:    t.Logf,
	})
	if err != nil {
		t.Fatalf("Create store: %v", err)
	}
	if err := st.Refresh(t.Context()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	st.Close()

	// Load the cache as a client.
	fclient, err := setec.NewFileClient(cpath)
	if err != nil {
		t.Fatalf("New file client: %v", err)
	}

	st, err = setec.NewStore(t.Context(), setec.StoreConfig{
		Client:  fclient,
		Secrets: []string{"apple", "pear", "cherry"},
		Logf:    t.Logf,
	})
	if err != nil {
		t.Fatalf("Create store: %v", err)
	}
	defer st.Close()

	checkSecretValue(t, st, "apple", "a1")
	checkSecretValue(t, st, "pear", "p1")
	checkSecretValue(t, st, "cherry", "c2")
}

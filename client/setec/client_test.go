// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package setec_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tailscale/setec/client/setec"
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
     "secret": {"Version": 2, "Value": "dGFydGxldA=="}
  },
  "little/tasty/cherry": {
     "secret": {"Version": 3, "Value": "cGll"}
  },
  "": {"secret": {"Version": 10, "Value": "dW5zZWVu"}}
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
}

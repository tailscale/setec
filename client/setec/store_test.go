// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package setec_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tailscale/setec/client/setec"
	"github.com/tailscale/setec/types/api"
	"tailscale.com/types/logger"
)

// testServer is a trivial fake for the parts of the server required by the
// store implementation.
type testServer struct {
	secrets map[string][]*api.SecretValue
}

func (ts testServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/get" {
		http.Error(w, "unsupported request", http.StatusInternalServerError)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var req api.GetRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	svs, ok := ts.secrets[req.Name]
	if ok {
		for _, sv := range svs {
			if req.Version == 0 || req.Version == sv.Version {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(&sv)
				return
			}
		}
	}
	http.Error(w, fmt.Sprintf("%q not found", req.Name), http.StatusBadRequest)
}

func mustMemCache(t *testing.T, data string) *setec.MemCache {
	t.Helper()
	var mc setec.MemCache
	if err := mc.Write([]byte(data)); err != nil {
		t.Fatalf("Initialize MemCache: %v", err)
	}
	return &mc
}

func TestStore(t *testing.T) {
	ts := testServer{secrets: map[string][]*api.SecretValue{
		"alpha": {{Value: []byte("ok"), Version: 1}},
		"bravo": {
			{Value: []byte("no"), Version: 2},
			{Value: []byte("yes"), Version: 1},
		},
	}}
	s := httptest.NewServer(ts)
	defer s.Close()

	ctx := context.Background()
	cli := setec.Client{Server: s.URL, DoHTTP: s.Client().Do}

	t.Run("NewStore_missingURL", func(t *testing.T) {
		st, err := setec.NewStore(ctx, setec.StoreConfig{})
		if err == nil {
			st.Close()
			t.Errorf("Got %+v, want error", st)
		} else {
			t.Logf("Got expected error: %v", err)
		}
	})

	t.Run("NewStore_emptySecrets", func(t *testing.T) {
		st, err := setec.NewStore(ctx, setec.StoreConfig{Client: cli})
		if err == nil {
			st.Close()
			t.Errorf("Got %+v, want error", st)
		} else {
			t.Logf("Got expected error: %v", err)
		}
	})

	t.Run("NewStore_missingSecret", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		st, err := setec.NewStore(ctx, setec.StoreConfig{
			Client:  cli,
			Secrets: []string{"nonesuch"},
			Logf:    logger.Discard,
		})
		if err == nil {
			st.Close()
			t.Errorf("Got %+v, want error", st)
		} else {
			t.Logf("Got expected error: %v", err)
		}
	})

	t.Run("NewStoreOK", func(t *testing.T) {
		st, err := setec.NewStore(ctx, setec.StoreConfig{
			Client:  cli,
			Secrets: []string{"alpha", "bravo"},
			Logf:    logger.Discard,
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer st.Close()

		// We should be able to get a secret we requested.
		if f := st.Secret("alpha"); f == nil {
			t.Error("Lookup alpha: result is nil")
		} else if got, want := string(f.Get()), "ok"; got != want {
			t.Errorf("Lookup alpha: got %q, want %q", got, want)
		}

		// We should not be able to get a secret we didn't request.
		if f := st.Secret("nonesuch"); f != nil {
			t.Errorf("Lookup nonesuch: got %v, want nil", f)
		}
	})
}

func TestCachedStore(t *testing.T) {
	const cacheData = `{"alpha":{"Value":"Zm9vYmFy","Version":100}}`

	// Populate a memory cache with an "old" value of a secret.
	mc := mustMemCache(t, cacheData)

	// Connect to a service which has a newer value of the same secret, and
	// verify that initially we see the cached value.
	s := httptest.NewServer(testServer{secrets: map[string][]*api.SecretValue{
		"alpha": {
			{Value: []byte("bazquux"), Version: 200}, // a newer value
			{Value: []byte("foobar"), Version: 100},  // the value in cache
		}}})
	defer s.Close()
	cli := setec.Client{Server: s.URL, DoHTTP: s.Client().Do}

	ctx := context.Background()
	st, err := setec.NewStore(ctx, setec.StoreConfig{
		Client:       cli,
		Secrets:      []string{"alpha"},
		Cache:        mc,
		PollInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewServer: unexpected error: %v", err)
	}
	defer st.Close()

	alpha := st.Secret("alpha")
	if alpha == nil {
		t.Fatal("Lookup alpha: secret not found")
	}

	if got, want := string(alpha.Get()), "foobar"; got != want {
		t.Fatalf("Lookup alpha: got %q, want %q", got, want)
	}

	// After the poller has had a chance to observe the new version, verify that
	// we see it without having to update explicitly.
	time.Sleep(100 * time.Millisecond)
	if got, want := string(alpha.Get()), "bazquux"; got != want {
		t.Fatalf("Lookup alpha: got %q, want %q", got, want)
	}

	// Check that the cache got updated with the new value.
	const newCache = `{"alpha":{"Value":"YmF6cXV1eA==","Version":200}}`

	if got := mc.String(); got != newCache {
		t.Errorf("Cache value:\ngot  %#q\nwant %#q", got, newCache)
	}
}

func TestBadCache(t *testing.T) {
	s := httptest.NewServer(testServer{secrets: map[string][]*api.SecretValue{
		"alpha": {{Value: []byte("foobar"), Version: 100}},
	}})
	defer s.Close()
	cli := setec.Client{Server: s.URL, DoHTTP: s.Client().Do}
	ctx := context.Background()

	// Validate that errors in reading and decoding the cache do not prevent the
	// store from starting up if it is otherwise OK.
	tests := []struct {
		name  string
		cache setec.Cache
	}{
		{"ReadFailed", badCache{}},
		{"DecodeFailed", mustMemCache(t, `{"bad":JSON*#$&(@`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, err := setec.NewStore(ctx, setec.StoreConfig{
				Client:  cli,
				Secrets: []string{"alpha"},
				Cache:   tc.cache,
			})
			if err != nil {
				t.Fatalf("NewStore: unexpected error: %v", err)
			}

			// Despite a cache load error, we should have gotten a value.
			if got, want := string(st.Secret("alpha").Get()), "foobar"; got != want {
				t.Fatalf("Lookup alpha: got %q, want %q", got, want)
			}
		})
	}
}

type badCache struct{}

func (badCache) Write([]byte) error    { return errors.New("write failed") }
func (badCache) Read() ([]byte, error) { return nil, errors.New("read failed") }

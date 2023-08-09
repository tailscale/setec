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
	"sync"
	"testing"
	"time"

	"github.com/tailscale/setec/client/setec"
	"github.com/tailscale/setec/types/api"
	"tailscale.com/types/logger"
)

// testServer is a trivial fake for the parts of the server required by the
// store implementation.
type testServer struct {
	mu   sync.Mutex
	data map[string][]*api.SecretValue
}

// Add adds data as the active version of the named secret.
func (ts *testServer) Add(name, data string, version api.SecretVersion) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.data == nil {
		ts.data = make(map[string][]*api.SecretValue)
	}
	ts.data[name] = append([]*api.SecretValue{{
		Value:   []byte(data),
		Version: version,
	}}, ts.data[name]...)
}

func (ts *testServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	ts.mu.Lock()
	defer ts.mu.Unlock()
	svs, ok := ts.data[req.Name]
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

func checkSecretValue(t *testing.T, st *setec.Store, name, want string) setec.Secret {
	t.Helper()
	f := st.Secret(name)
	if f == nil {
		t.Fatalf("Secret %q not found", name)
	}
	if got := string(f.Get()); got != want {
		t.Fatalf("Secret %q: got %q, want %q", name, got, want)
	}
	return f
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
	ts := new(testServer)
	ts.Add("alpha", "ok", 1)
	ts.Add("bravo", "yes", 1)
	ts.Add("bravo", "no", 2)

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
		checkSecretValue(t, st, "alpha", "ok")

		// We should not be able to get a secret we didn't request.
		if f := st.Secret("nonesuch"); f != nil {
			t.Errorf("Lookup nonesuch: got %v, want nil", f)
		}
	})
}

func TestCachedStore(t *testing.T) {
	const cacheData = `{"alpha":{"Value":"Zm9vYmFy","Version":100}}`

	// Make a poll ticker so we can control the poll schedule.
	pollTicker := newFakeTicker()

	// Populate a memory cache with an "old" value of a secret.
	mc := mustMemCache(t, cacheData)

	// Connect to a service which has a newer value of the same secret, and
	// verify that initially we see the cached value.
	ts := new(testServer)
	ts.Add("alpha", "foobar", 100)
	ts.Add("alpha", "bazquux", 200)
	s := httptest.NewServer(ts)
	defer s.Close()
	cli := setec.Client{Server: s.URL, DoHTTP: s.Client().Do}

	ctx := context.Background()
	st, err := setec.NewStore(ctx, setec.StoreConfig{
		Client:     cli,
		Secrets:    []string{"alpha"},
		Cache:      mc,
		PollTicker: pollTicker,
	})
	if err != nil {
		t.Fatalf("NewServer: unexpected error: %v", err)
	}
	defer st.Close()

	alpha := checkSecretValue(t, st, "alpha", "foobar")

	// After the poller has had a chance to observe the new version, verify that
	// we see it without having to update explicitly.
	pollTicker.Poll()

	if got, want := string(alpha.Get()), "bazquux"; got != want {
		t.Fatalf("Lookup alpha: got %q, want %q", got, want)
	}

	// Check that the cache got updated with the new value.
	const newCache = `{"alpha":{"Value":"YmF6cXV1eA==","Version":200}}`

	if got := mc.String(); got != newCache {
		t.Errorf("Cache value:\ngot  %#q\nwant %#q", got, newCache)
	}

	// Basic consistency checks on metrics.
	t.Run("VerifyMetrics", func(t *testing.T) {
		m := st.Metrics()
		check := func(name, want string) {
			if metric := m.Get(name); metric == nil {
				t.Errorf("Metric %q not found", name)
			} else if got := metric.String(); got != want {
				t.Errorf("Metric %q: got %q, want %q", name, got, want)
			}
		}
		check("counter_poll_initiated", "1")
		check("counter_poll_errors", "0")
		check("counter_secret_fetch", "2")
	})
}

func TestBadCache(t *testing.T) {
	ts := new(testServer)
	ts.Add("alpha", "foobar", 100)
	s := httptest.NewServer(ts)
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
			checkSecretValue(t, st, "alpha", "foobar")
		})
	}
}

func TestSlowInit(t *testing.T) {
	ts := new(testServer)
	s := httptest.NewServer(ts)
	defer s.Close()

	ctx := context.Background()
	cli := setec.Client{Server: s.URL, DoHTTP: s.Client().Do}

	errc := make(chan error)
	checkNotReady := func() {
		select {
		case err := <-errc:
			t.Fatalf("Store should not be ready (err=%v)", err)
		case <-time.After(time.Millisecond):
			// OK
		}
	}

	var st *setec.Store
	go func() {
		defer close(errc)
		var err error
		st, err = setec.NewStore(ctx, setec.StoreConfig{
			Client:  cli,
			Secrets: []string{"minsc", "boo"},
		})
		errc <- err
	}()

	// Initially the server has no secrets, so NewStore should wait.
	checkNotReady()

	// A value for one of the secrets arrives, but we still await the other.
	ts.Add("boo", "go for the eyes", 1)
	checkNotReady()

	// A value for an unrelated secret arrives, and does not affect us.
	ts.Add("dynaheir", "my spell has no effect", 5)
	checkNotReady()

	// A value for the other missing secret arrives.
	ts.Add("minsc", "full plate and packing steel", 1)

	// Now the store should become ready.
	select {
	case <-time.After(15 * time.Second):
		t.Fatal("Timed out waiting for store to finish initializing")
	case err := <-errc:
		if err != nil {
			t.Errorf("NewStore reported an error: %v", err)
		}
	}
	defer st.Close()

	checkSecretValue(t, st, "minsc", "full plate and packing steel")
	checkSecretValue(t, st, "boo", "go for the eyes")
}

type badCache struct{}

func (badCache) Write([]byte) error    { return errors.New("write failed") }
func (badCache) Read() ([]byte, error) { return nil, errors.New("read failed") }

func newFakeTicker() *fakeTicker {
	return &fakeTicker{ch: make(chan time.Time), done: make(chan struct{})}
}

type fakeTicker struct {
	ch   chan time.Time
	done chan struct{}
}

func (fakeTicker) Stop()                    {}
func (f fakeTicker) Chan() <-chan time.Time { return f.ch }
func (f *fakeTicker) Done()                 { f.done <- struct{}{} }

// Poll signals the ticker, then waits for Done to be invoked.
func (f *fakeTicker) Poll() {
	f.ch <- time.Now()
	<-f.done
}

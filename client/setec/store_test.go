// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package setec_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tailscale/setec/client/setec"
	"github.com/tailscale/setec/setectest"
	"tailscale.com/types/logger"
)

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

func TestStore(t *testing.T) {
	d := setectest.NewDB(t, nil)
	d.MustPut(d.Superuser, "alpha", "ok")
	d.MustPut(d.Superuser, "bravo", "yes")
	d.MustPut(d.Superuser, "bravo", "no")

	ts := setectest.NewServer(t, d, nil)
	hs := httptest.NewServer(ts.Mux)
	defer hs.Close()

	ctx := context.Background()
	cli := setec.Client{Server: hs.URL, DoHTTP: hs.Client().Do}

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

	t.Run("NewStore_noLookup", func(t *testing.T) {
		st, err := setec.NewStore(ctx, setec.StoreConfig{
			Client:  cli,
			Secrets: []string{"alpha"},
			Logf:    logger.Discard,
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if s, err := st.LookupSecret("bravo"); err == nil {
			t.Errorf("Lookup(bravo): got %q, want error", s.Get())
		}
		if w, err := st.LookupWatcher("bravo"); err == nil {
			t.Errorf("Lookup(brav0: got %q, want error", w.Get())
		}
	})
}

func TestCachedStore(t *testing.T) {
	const cacheData = `{"alpha":{"secret":{"Value":"Zm9vYmFy","Version":1},"lastAccess":"0"}}`

	// Make a poll ticker so we can control the poll schedule.
	pollTicker := newFakeTicker()

	// Populate a memory cache with an "old" value of a secret.
	mc := setec.NewMemCache(cacheData)

	// Connect to a service which has a newer value of the same secret, and
	// verify that initially we see the cached value.
	d := setectest.NewDB(t, nil)
	d.MustPut(d.Superuser, "alpha", "foobar")
	v2 := d.MustPut(d.Superuser, "alpha", "bazquux")
	d.MustActivate(d.Superuser, "alpha", v2)

	ts := setectest.NewServer(t, d, nil)
	hs := httptest.NewServer(ts.Mux)
	defer hs.Close()

	ctx := context.Background()
	cli := setec.Client{Server: hs.URL, DoHTTP: hs.Client().Do}

	st, err := setec.NewStore(ctx, setec.StoreConfig{
		Client:     cli,
		Secrets:    []string{"alpha"},
		Cache:      mc,
		PollTicker: pollTicker,
		TimeNow:    func() time.Time { return time.Unix(1, 0) }, // fixed time
		Logf:       logger.Discard,
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
	const newCache = `{"alpha":{"secret":{"Value":"YmF6cXV1eA==","Version":2},"lastAccess":"1"}}`

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
	d := setectest.NewDB(t, nil)
	d.MustPut(d.Superuser, "alpha", "foobar")

	ts := setectest.NewServer(t, d, nil)
	hs := httptest.NewServer(ts.Mux)
	defer hs.Close()

	ctx := context.Background()
	cli := setec.Client{Server: hs.URL, DoHTTP: hs.Client().Do}

	// Validate that errors in reading and decoding the cache do not prevent the
	// store from starting up if it is otherwise OK.
	tests := []struct {
		name  string
		cache setec.Cache
	}{
		{"ReadFailed", badCache{}},
		{"DecodeFailed", setec.NewMemCache(`{"bad":JSON*#$&(@`)},
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
	ts := setectest.NewServer(t, setectest.NewDB(t, nil), nil)
	hs := httptest.NewServer(ts.Mux)
	defer hs.Close()

	ctx := context.Background()
	cli := setec.Client{Server: hs.URL, DoHTTP: hs.Client().Do}

	errc := make(chan error)
	checkNotReady := func() {
		select {
		case err := <-errc:
			t.Fatalf("Store should not be ready (err=%v)", err)
		case <-time.After(time.Millisecond):
			// OK
		}
	}
	mustPut := func(key, value string) {
		if _, err := cli.Put(ctx, key, []byte(value)); err != nil {
			t.Fatalf("Put %q: unexpected error: %v", key, err)
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
	mustPut("boo", "go for the eyes")
	checkNotReady()

	// A value for an unrelated secret arrives, and does not affect us.
	mustPut("dynaheir", "my spell has no effect")
	checkNotReady()

	// A value for the other missing secret arrives.
	mustPut("minsc", "full plate and packing steel")

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

func TestWatcher(t *testing.T) {
	d := setectest.NewDB(t, nil)
	d.MustPut(d.Superuser, "green", "eggs and ham") // active
	v2 := d.MustPut(d.Superuser, "green", "grow the rushes oh")

	ts := setectest.NewServer(t, d, nil)
	hs := httptest.NewServer(ts.Mux)
	defer hs.Close()

	ctx := context.Background()
	cli := setec.Client{Server: hs.URL, DoHTTP: hs.Client().Do}

	pollTicker := newFakeTicker()
	st, err := setec.NewStore(ctx, setec.StoreConfig{
		Client:     cli,
		Secrets:    []string{"green"},
		PollTicker: pollTicker,
	})
	if err != nil {
		t.Fatalf("NewStore: unexpected error: %v", err)
	}
	defer st.Close()

	// Observe the initial value of the secret.
	w := st.Watcher("green")
	if got, want := string(w.Get()), "eggs and ham"; got != want {
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

func TestLookup(t *testing.T) {
	d := setectest.NewDB(t, nil)
	d.MustPut(d.Superuser, "red", "badge of courage") // active
	d.MustPut(d.Superuser, "green", "eggs and ham")
	d.MustPut(d.Superuser, "blue", "dolphins")

	ts := setectest.NewServer(t, d, nil)
	hs := httptest.NewServer(ts.Mux)
	defer hs.Close()

	ctx := context.Background()
	cli := setec.Client{Server: hs.URL, DoHTTP: hs.Client().Do}

	// Case 1: We can create a store with no secrets if AllowLookup is true.
	if _, err := setec.NewStore(ctx, setec.StoreConfig{
		Client:      cli,
		AllowLookup: true,
	}); err != nil {
		t.Errorf("NewStore with AllowLookup: unexpected error: %v", err)
	}

	// Set up a store that knows about "red", but not "green" or "blue" (yet)
	st, err := setec.NewStore(ctx, setec.StoreConfig{
		Client:      cli,
		Secrets:     []string{"red"},
		AllowLookup: true,
		Logf:        logger.Discard,
	})
	if err != nil {
		t.Fatalf("NewStore: unexpected error: %v", err)
	}

	// Case 2: We can get a secret for "red" directly, but "green" is unknown.
	if s := st.Secret("red"); s == nil {
		t.Error("Secret(red): no value found")
	}
	if s := st.Secret("green"); s != nil {
		t.Errorf("Secret(green): unexpected success %q", s.Get())
	}

	// Case 3: We can look up a secret for "green".
	if s, err := st.LookupSecret("green"); err != nil {
		t.Errorf("Lookup(green): unexepcted error: %v", err)
	} else if got, want := string(s.Get()), "eggs and ham"; got != want {
		t.Errorf("Lookup(green): got %q, want %q", got, want)
	}

	// Case 4: We can look up a watcher for "blue".
	if w, err := st.LookupWatcher("blue"); err != nil {
		t.Errorf("Lookup(blue): unexpected error: %v", err)
	} else if got, want := string(w.Get()), "dolphins"; got != want {
		t.Errorf("Lookup(blue): got %q, want %q", got, want)
	}

	// Case 5: We still can't lookup a non-existent secret.
	if s, err := st.LookupSecret("orange"); err == nil {
		t.Errorf("Lookup(orange): got %q, want error", s.Get())
	} else {
		t.Logf("Lookup(orange) correctly failed: %v", err)
	}
}

func TestCacheExpiry(t *testing.T) {
	d := setectest.NewDB(t, nil)
	d.MustPut(d.Superuser, "apple", "malus pumila")
	d.MustPut(d.Superuser, "pear", "pyrus communis")
	d.MustPut(d.Superuser, "plum", "prunus americana")
	d.MustPut(d.Superuser, "cherry", "prunus avium")

	ts := setectest.NewServer(t, d, nil)
	hs := httptest.NewServer(ts.Mux)
	defer hs.Close()

	ctx := context.Background()
	cli := setec.Client{Server: hs.URL, DoHTTP: hs.Client().Do}

	mc := new(setec.MemCache)
	apparentTime := time.Unix(1, 0)
	advance := func(d time.Duration) {
		apparentTime = apparentTime.Add(d)
	}

	// Start a store, access an undeclared secret, and cache.
	t.Run("Setup", func(t *testing.T) {
		st, err := setec.NewStore(ctx, setec.StoreConfig{
			Client:      cli,
			Cache:       mc,                        // currently empty
			AllowLookup: true,                      // allow undeclared secrets
			Secrets:     []string{"apple", "pear"}, // declared secrets
			TimeNow:     func() time.Time { return apparentTime },
		})
		if err != nil {
			t.Fatalf("NewStore: unexpected error: %v", err)
		}
		defer st.Close()

		advance(25 * time.Second)
		if _, err := st.LookupSecret("plum"); err != nil {
			t.Fatalf("Lookup(plum): unexpected error: %v", err)
		}
		advance(25 * time.Second)
		if _, err := st.LookupSecret("cherry"); err != nil {
			t.Fatalf("Lookup(cherry): unexpected error: %v", err)
		}
	})

	t.Run("Probe", func(t *testing.T) {
		pt := newFakeTicker()

		st, err := setec.NewStore(ctx, setec.StoreConfig{
			Client:      cli,
			Cache:       mc,                // holds the output from setup
			AllowLookup: true,              // allow undeclared secrets
			Secrets:     []string{"apple"}, // declared secrets
			ExpiryAge:   30 * time.Second,  // expiry age
			PollTicker:  pt,
			TimeNow:     func() time.Time { return apparentTime },
		})
		if err != nil {
			t.Fatalf("NewStore: unexpected error: %v", err)
		}
		defer st.Close()

		// At this moment we have:
		//
		//    secret | decl | accessed
		//    apple  | yes  | 1
		//    pear   | no   | 1
		//    plum   | no   | 26
		//    cherry | no   | 51   << we are here
		//
		// We advance by 20 seconds to 71. Now, apple, pear, and plum are outside
		// the expiry window, while cherry is not. We then perform a poll.
		//
		// Since apple is declared, it cannot expire. Pear and plum are
		// undeclared and should be removed. Cherry is not yet old enough, so it
		// should not be removed.
		advance(20 * time.Second)
		pt.Poll()

		if st.Secret("apple") == nil {
			t.Error("Secret(apple) is missing, should be here")
		}
		if f := st.Secret("pear"); f != nil {
			t.Errorf("Secret(pear): got %q, want not found", f.Get())
		}
		if f := st.Secret("plum"); f != nil {
			t.Errorf("Secret(plum): got %q, want not found", f.Get())
		}
		if st.Secret("cherry") == nil {
			t.Errorf("Secret(cherry) is missing, should be here")
		}
	})

	t.Logf("Final cache: %s", mc.String())
}

func TestFileCache(t *testing.T) {
	t.Run("OK", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "sub/cache.file")
		fc, err := setec.NewFileCache(path)
		if err != nil {
			t.Fatalf("NewFileCache: unexpected error: %v", err)
		}
		if err := fc.Write([]byte("xyzzy")); err != nil {
			t.Fatalf("Write cache: unexpected error: %v", err)
		}
		got, err := fc.Read()
		if err != nil {
			t.Fatalf("Read cache: unexpected error: %v", err)
		} else if string(got) != "xyzzy" {
			t.Fatalf("Read cache: got %q, want xyzzy", got)
		}
	})

	t.Run("Create", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "path/to/cache.file")
		if _, err := setec.NewFileCache(path); err != nil {
			t.Fatalf("NewFileCache: unexpected error: %v", err)
		}
	})

	t.Run("FailMkdir", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "blocker"), []byte("ok"), 0600); err != nil {
			t.Fatalf("Create blocking file: %v", err)
		}

		// The blocker file should prevent us creating the cache directory.
		fc, err := setec.NewFileCache(filepath.Join(dir, "blocker/foo/cache.file"))
		if err == nil {
			t.Errorf("NewFileCache: got %v, wanted error", fc)
		}
	})

	t.Run("FailType", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "cache.file")
		if err := os.Symlink("whatever", path); err != nil {
			t.Fatalf("Create blocking symlink: %v", err)
		}

		// The blocker prevents us from using the cache file.
		fc, err := setec.NewFileCache(path)
		if err == nil {
			t.Errorf("NewFileCache: got %v, wanted error", fc)
		}
	})
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

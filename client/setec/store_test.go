// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package setec_test

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/creachadair/mds/mtest"
	"github.com/tailscale/setec/client/setec"
	"github.com/tailscale/setec/setectest"
	"github.com/tailscale/setec/types/api"
	"tailscale.com/types/logger"
)

func checkSecretValue(t *testing.T, st *setec.Store, name, want string) setec.Secret {
	t.Helper()
	f := st.Secret(name)
	if f == nil {
		t.Fatalf("Secret %q not found", name)
	}
	if got := f.Get(); !bytes.Equal(got, []byte(want)) {
		t.Fatalf("Secret %q: got %q, want %q", name, got, want)
	}
	if got := f.GetString(); got != want {
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

	ctx := t.Context()
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

		// With lookups disabled, an undeclared secret panics.
		mtest.MustPanicf(t, func() { st.Secret("nonesuch") },
			"Expected panic for an unknown secret")
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
		if s, err := st.LookupSecret(ctx, "bravo"); err == nil {
			t.Errorf("Lookup(bravo): got %q, want error", s.Get())
		}
	})

	t.Run("NewStore_emptyName", func(t *testing.T) {
		st, err := setec.NewStore(ctx, setec.StoreConfig{
			Client:      cli,
			Secrets:     []string{""},
			Logf:        logger.Discard,
			AllowLookup: true,
		})
		if err == nil {
			t.Fatalf("NewStore(empty): got %+v, want error", st)
		}
	})

	t.Run("NewStore_dupName", func(t *testing.T) {
		// Regression: A duplicate name can poison the initialization map, so we
		// need to deduplicate as part of collection.
		st, err := setec.NewStore(ctx, setec.StoreConfig{
			Client:  cli,
			Secrets: []string{"alpha", "alpha"},
			Logf:    logger.Discard,
		})
		if err != nil {
			t.Fatalf("NewStore(dup): unexpected error: %v", err)
		}
		t.Logf("NewStore: got secret %q", st.Secret("alpha").GetString())
	})
}

func TestCachedStore(t *testing.T) {
	const cacheData = `{"alpha":{"secret":{"Value":"Zm9vYmFy","Version":1},"lastAccess":"0"}}`

	// Make a poll ticker so we can control the poll schedule.
	pollTicker := setectest.NewFakeTicker()

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

	ctx := t.Context()
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

	if got, want := alpha.GetString(), "bazquux"; got != want {
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
		check("counter_secret_fetch", "3")
	})
}

func TestBadCache(t *testing.T) {
	d := setectest.NewDB(t, nil)
	d.MustPut(d.Superuser, "alpha", "foobar")

	ts := setectest.NewServer(t, d, nil)
	hs := httptest.NewServer(ts.Mux)
	defer hs.Close()

	ctx := t.Context()
	cli := setec.Client{Server: hs.URL, DoHTTP: hs.Client().Do}

	// Validate that errors in reading and decoding the cache do not prevent the
	// store from starting up if it is otherwise OK.
	tests := []struct {
		name  string
		cache setec.Cache
	}{
		{"ReadFailed", badCache{}},
		{"DecodeFailed", setec.NewMemCache(`{"bad":JSON*#$&(@`)},
		{"InvalidCache", setec.NewMemCache(`{"alpha":{"Value":"blah", "Version":100}}`)},
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

	ctx := t.Context()
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

func TestUpdater(t *testing.T) {
	d := setectest.NewDB(t, nil)
	d.MustPut(d.Superuser, "label", "malarkey") // active
	v2 := d.MustPut(d.Superuser, "label", "dog-faced pony soldier")

	ts := setectest.NewServer(t, d, nil)
	hs := httptest.NewServer(ts.Mux)
	defer hs.Close()

	ctx := t.Context()
	cli := setec.Client{Server: hs.URL, DoHTTP: hs.Client().Do}

	pollTicker := setectest.NewFakeTicker()
	st, err := setec.NewStore(ctx, setec.StoreConfig{
		Client:     cli,
		Secrets:    []string{"label"},
		PollTicker: pollTicker,
	})
	if err != nil {
		t.Fatalf("NewStore: unexpected error: %v", err)
	}
	defer st.Close()

	// Set up an updater that tracks a string against the secret named "label".
	t.Run("Dynamic", func(t *testing.T) {
		u, err := setec.NewUpdater(ctx, st, "label", func(secret []byte) (*closeable[string], error) {
			return &closeable[string]{Value: string(secret)}, nil
		})
		if err != nil {
			t.Fatalf("NewUpdater: unexpected error: %v", err)
		}
		checkValue := func(label, want string) {
			if got := u.Get().Value; got != want {
				t.Errorf("%s: got %q, want %q", label, got, want)
			}
		}

		checkValue("Initial value", `malarkey`)
		last := u.Get()
		if last.Closed {
			t.Error("Initial value is closed early")
		}

		// The secret gets updated...
		if err := cli.Activate(ctx, "label", v2); err != nil {
			t.Fatalf("Activate to %v: unexpected error: %v", v2, err)
		}
		pollTicker.Poll()

		// The next get should see the updated value.
		checkValue("Updated value", `dog-faced pony soldier`)

		// The previous value should have been closed.
		if !last.Closed {
			t.Errorf("Initial value was not closed: %v", last)
		}

		last = u.Get()
		pollTicker.Poll()

		// The next get should not see a change.
		checkValue("Updated value", `dog-faced pony soldier`)
		if last.Closed {
			t.Errorf("Update value was closed: %v", last)
		}
	})

	t.Run("Static", func(t *testing.T) {
		const testValue = "I am the chosen one"
		u := setec.StaticUpdater(testValue)

		if got := u.Get(); got != testValue {
			t.Errorf("Get: got %q, want %q", got, testValue)
		}
	})
}

func TestLookup(t *testing.T) {
	d := setectest.NewDB(t, nil)
	d.MustPut(d.Superuser, "red", "badge of courage") // active
	d.MustPut(d.Superuser, "green", "eggs and ham")
	d.MustPut(d.Superuser, "blue", "dolphins")

	ts := setectest.NewServer(t, d, nil)
	hs := httptest.NewServer(ts.Mux)
	defer hs.Close()

	ctx := t.Context()
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
	if s, err := st.LookupSecret(ctx, "green"); err != nil {
		t.Errorf("Lookup(green): unexepcted error: %v", err)
	} else if got, want := string(s.Get()), "eggs and ham"; got != want {
		t.Errorf("Lookup(green): got %q, want %q", got, want)
	}

	// Case 4: We still can't lookup a non-existent secret.
	if s, err := st.LookupSecret(ctx, "orange"); err == nil {
		t.Errorf("Lookup(orange): got %q, want error", s.Get())
	} else {
		t.Logf("Lookup(orange) correctly failed: %v", err)
	}

	// With lookup enabled, unknown secrets report nil.
	if f := st.Secret("nonesuch"); f != nil {
		t.Errorf("Lookup(nonesuch): got %v, want nil", f)
	}
}

func TestVersionedSecret(t *testing.T) {
	d := setectest.NewDB(t, nil)

	ts := setectest.NewServer(t, d, nil)
	hs := httptest.NewServer(ts.Mux)
	defer hs.Close()

	ctx := t.Context()
	cli := setec.Client{Server: hs.URL, DoHTTP: hs.Client().Do}
	err := cli.CreateVersion(ctx, "old", 2, []byte("blue"))
	if err != nil {
		t.Fatalf("CreateVersion for old: unexpected error: %s", err)
	}

	// Make a poll ticker so we can control the poll schedule.
	pollTicker := setectest.NewFakeTicker()

	const cacheData = `{"old":{"secret":{"Value":"Ymx1ZQ==","Version":2},"lastAccess":"0"}}`

	st, err := setec.NewStore(ctx, setec.StoreConfig{
		Client:      cli,
		AllowLookup: true,
		Logf:        logger.Discard,
		PollTicker:  pollTicker,
		Cache:       setec.NewMemCache(cacheData),
		Secrets:     []string{"old"},
	})
	if err != nil {
		t.Fatalf("NewStore: unexpected error: %v", err)
	}
	defer st.Close()

	// Read old secret first to make sure active version is readable.
	os := st.Secret("old")
	if string(os.GetString()) != "blue" {
		t.Fatalf("Old secret should have been blue but was %q", os.GetString())
	}

	const secretName = "secret_name"
	vs := st.VersionedSecret(secretName)
	err = vs.CreateVersion(ctx, 0, []byte("red"))
	if err == nil {
		t.Fatalf("CreateVersion 0 should have failed")
	}

	err = vs.CreateVersion(ctx, 1, []byte("red")) // This first version automatically becomes active
	if err != nil {
		t.Fatalf("CreateVersion 1: unexpected error: %v", err)
	}
	s1, err := vs.GetVersion(ctx, 1)
	if err != nil {
		t.Fatalf("GetVersion 1: unexpected error: %v", err)
	}
	if string(s1) != "red" {
		t.Fatalf("Version 1 should have been red but was %q", string(s1))
	}

	// Verify that it's possible to get the active secret too
	sa := st.Secret(secretName)
	if string(sa()) != "red" {
		t.Fatalf("Active version should be red but was %q", string(sa()))
	}

	err = vs.CreateVersion(ctx, 2, []byte("green"))
	if err != nil {
		t.Fatalf("CreateVersion 2: unexpected error: %v", err)
	}
	err = vs.CreateVersion(ctx, 2, []byte("orange"))
	if !errors.Is(err, api.ErrVersionClaimed) {
		t.Fatalf("CreateVersion 2 again: should have failed with ErrVersionClaimed, but resulted in: %s", err)
	}
	s2, err := vs.GetVersion(ctx, 2)
	if err != nil {
		t.Fatalf("GetVersion 2: unexpected error: %v", err)
	}
	if string(s2) != "green" {
		t.Fatalf("Version 2 should have been green but was %q", string(s2))
	}
	if sa.GetString() != "green" {
		t.Fatalf("Active version should have changed to green but was %q", sa.GetString())
	}

	s1b, err := vs.GetVersion(ctx, 1)
	if err != nil {
		t.Fatalf("GetVersion 1: unexpected error: %v", err)
	}
	if string(s1b) != "red" {
		t.Fatalf("Version 1 should still be red but was %q", string(s1b))
	}
	if sa.GetString() != "green" {
		t.Fatalf("Active version should still be green but was %q", string(sa.GetString()))
	}

	// Activate an older version on the server and make sure poll picks up the change
	err = cli.Activate(ctx, secretName, 1)
	if err != nil {
		t.Fatalf("ActivateVersion: unexpected error: %v", err)
	}
	pollTicker.Poll()
	if sa.GetString() != "red" {
		t.Fatalf("Active version should have reverted to red but was %q", string(sa.GetString()))
	}

	// Delete a version on the server and make sure poll picks up the change
	err = cli.DeleteVersion(ctx, secretName, 2)
	if err != nil {
		t.Fatalf("DeleteVersion: unexpected error: %v", err)
	}
	pollTicker.Poll()
	_, err = vs.GetVersion(ctx, 2)
	if !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("Expected version 2 to be not found, but got error: %s", err)
	}

	// Create a new version on the server and make sure poll picks up the change
	err = cli.CreateVersion(ctx, secretName, 3, []byte("lime"))
	if err != nil {
		t.Fatalf("CreateVersion: unexpected error: %v", err)
	}
	pollTicker.Poll()
	s3, err := vs.GetVersion(ctx, 3)
	if err != nil {
		t.Fatalf("GetVersion: unexpected error: %v", err)
	}
	if string(s3) != "lime" {
		t.Fatalf("Version 3 should have been lime but was %q", string(s3))
	}
	if sa.GetString() != "lime" {
		t.Fatalf("Active version should have changed to lime but was %q", string(sa.GetString()))
	}
}

func TestVersionedSecretUnsupportedClient(t *testing.T) {
	secPath := filepath.Join(t.TempDir(), "secrets.json")
	if err := os.WriteFile(secPath, []byte("{}"), 0600); err != nil {
		t.Fatalf("Write test data: %v", err)
	}

	ctx := t.Context()
	cli, err := setec.NewFileClient(secPath)
	if err != nil {
		t.Fatalf("NewFileClient: unexpected error: %v", err)
	}

	st, err := setec.NewStore(ctx, setec.StoreConfig{
		Client:      cli,
		AllowLookup: true,
		Logf:        logger.Discard,
	})
	if err != nil {
		t.Fatalf("NewStore: unexpected error: %v", err)
	}

	mtest.MustPanicf(t, func() {
		st.VersionedSecret("secret_name")
	}, "VersionedSecret should have panicked with unsupported client")
}

func TestVersionedSecretLookupsDisabled(t *testing.T) {
	secPath := filepath.Join(t.TempDir(), "secrets.json")
	if err := os.WriteFile(secPath, []byte(testSecrets), 0600); err != nil {
		t.Fatalf("Write test data: %v", err)
	}

	ctx := t.Context()
	cli, err := setec.NewFileClient(secPath)
	if err != nil {
		t.Fatalf("NewFileClient: unexpected error: %v", err)
	}

	st, err := setec.NewStore(ctx, setec.StoreConfig{
		Client:      cli,
		AllowLookup: false,
		Secrets:     []string{"plum"},
		Logf:        logger.Discard,
	})
	if err != nil {
		t.Fatalf("NewStore: unexpected error: %v", err)
	}

	mtest.MustPanicf(t, func() {
		st.VersionedSecret("secret_name")
	}, "VersionedSecret should have panicked with lookups disabled")
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

	ctx := t.Context()
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
		if _, err := st.LookupSecret(ctx, "plum"); err != nil {
			t.Fatalf("Lookup(plum): unexpected error: %v", err)
		}
		advance(25 * time.Second)
		if _, err := st.LookupSecret(ctx, "cherry"); err != nil {
			t.Fatalf("Lookup(cherry): unexpected error: %v", err)
		}
	})

	t.Run("Probe", func(t *testing.T) {
		pt := setectest.NewFakeTicker()

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

func TestNewFileCache(t *testing.T) {
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

func TestNilSecret(t *testing.T) {
	var s setec.Secret

	if got := s.Get(); got != nil {
		t.Errorf("(nil).Get: got %v, want nil", got)
	}
	if got := s.GetString(); got != "" {
		t.Errorf(`(nil).GetString: got %q, want ""`, got)
	}
}

type badCache struct{}

func (badCache) Write([]byte) error    { return errors.New("write failed") }
func (badCache) Read() ([]byte, error) { return nil, errors.New("read failed") }

type closeable[T any] struct {
	Value  T
	Closed bool
}

func (c *closeable[T]) Close() error {
	c.Closed = true
	return nil
}

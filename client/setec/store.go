// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package setec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"expvar"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/tailscale/setec/types/api"
	"golang.org/x/sync/singleflight"
	"tailscale.com/types/logger"
)

// Store is a store that provides named secrets.
type Store struct {
	client      Client // API client
	logf        logger.Logf
	cache       Cache
	allowLookup bool
	newTicker   func(time.Duration) Ticker
	timeNow     func() time.Time
	single      singleflight.Group

	// Undeclared secrets not accessed in at least this long are eligible to be
	// purged from the cache. If zero, no expiry is performed.
	expiryAge time.Duration

	active struct {
		// Lock exclusive to read or modify the contents of the maps.
		// Secret values are not mutated once installed in the map, so it is safe
		// to continue to read them after releasing the lock.
		sync.Mutex

		m map[string]*cachedSecret // :: secret name → active value
		f map[string]Secret        // :: secret name → fetch function
		w map[string][]Watcher     // :: secret name → watchers
	}

	ctx    context.Context    // governs the polling task and lookups
	cancel context.CancelFunc // stops the polling task
	done   <-chan struct{}    // closed when the poller is finished

	// Metrics
	countPolls       expvar.Int   // polls initiated
	countPollErrors  expvar.Int   // errors in polling the service
	countSecretFetch expvar.Int   // count of secret value fetches
	latestPoll       expvar.Float // fractional seconds since Unix epoch, UTC
}

// Metrics returns a map of metrics for s. The caller is responsible for
// publishing the map to the metrics exporter.
func (s *Store) Metrics() *expvar.Map {
	m := new(expvar.Map)
	m.Set("counter_poll_initiated", &s.countPolls)
	m.Set("counter_poll_errors", &s.countPollErrors)
	m.Set("counter_secret_fetch", &s.countSecretFetch)
	m.Set("timestamp_latest_poll", &s.latestPoll)
	return m
}

// StoreConfig is the configuration for Store.
type StoreConfig struct {
	// Client is the API client used to fetch secrets from the service.
	// The service URL must be non-empty.
	Client Client

	// Secrets are the names of secrets this Store should retrieve.
	//
	// Unless AllowLookup is true, only secrets named here or in the Structs
	// field can be read out of the store and an error is reported if no secrets
	// are listed.
	Secrets []string

	// Structs are optional struct values with tagged fields that should be
	// populated with secrets from the store at initialization time.
	//
	// Unless AllowLookup is true, only secrets named here or in the Secrets
	// field can be read out of the store and an error is reported if no secrets
	// are listed.
	Structs []Struct

	// AllowLookup instructs the store to allow the caller to look up secrets
	// not known to the store at the time of construction. If false, only
	// secrets pre-declared in the Secrets and Structs slices can be fetched,
	// and the Lookup and LookupWatcher methods will report an error for all
	// un-listed secrets.
	AllowLookup bool

	// Cache, if non-nil, is a cache that persists secrets locally.
	//
	// Depending on the implementation, local caching may degrade security
	// slightly by making secrets easier to get at, but in return allows the
	// Store to initialize and run during outages of the secrets management
	// service.
	//
	// If no cache is provided, the Store caches secrets in-memory for the
	// lifetime of the process only.
	Cache Cache

	// PollInterval is the interval at which the store will poll the service for
	// updated secret values. If zero or negative, a default value is used.
	// This field is ignored if PollTicker is set.
	PollInterval time.Duration

	// ExpiryAge is a duration beyond which undeclared secrets that have not
	// been accessed in that time are eligible for expiration from the cache.
	// A zero value means secrets do not expire.
	ExpiryAge time.Duration

	// Logf is a logging function where text logs should be sent.  If nil, logs
	// are written to the standard log package.
	Logf logger.Logf

	// PollTicker, if set is a ticker that is used to control the scheduling of
	// update polls. If nil, a time.Ticker is used based on the PollInterval.
	PollTicker Ticker

	// TimeNow, if set, is a function that reports a Time to be treated as the
	// current wallclock time.  If nil, time.Now is used.
	TimeNow func() time.Time
}

func (c StoreConfig) logger() logger.Logf {
	if c.Logf == nil {
		return log.Printf
	}
	return c.Logf
}

func (c StoreConfig) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return 1 * time.Hour
	}
	return c.PollInterval
}

func (c StoreConfig) cache() Cache { return c.Cache }

func (c StoreConfig) newTicker() func(time.Duration) Ticker {
	if c.PollTicker == nil {
		return func(d time.Duration) Ticker {
			return stdTicker{Ticker: time.NewTicker(d)}
		}
	}
	return func(time.Duration) Ticker { return c.PollTicker }
}

func (c StoreConfig) timeNow() func() time.Time {
	if c.TimeNow == nil {
		return time.Now
	}
	return c.TimeNow
}

// NewStore creates a secret store with the given configuration.  The service
// URL of the client must be set.
//
// NewStore blocks until all the secrets named in cfg.Secrets are available for
// retrieval by the Secret method, or ctx ends.  The context passed to NewStore
// is only used for initializing the store. If a cache is provided, cached
// values are accepted even if stale, as long as there is a value for each of
// the secrets in cfg.
func NewStore(ctx context.Context, cfg StoreConfig) (*Store, error) {
	if cfg.Client.Server == "" {
		return nil, errors.New("no service URL is set")
	}

	secrets, structs, err := cfg.secretNames()
	if err != nil {
		return nil, err
	} else if len(secrets) == 0 && !cfg.AllowLookup {
		return nil, errors.New("no secrets are listed")
	}

	s := &Store{
		client:      cfg.Client,
		logf:        cfg.logger(),
		cache:       cfg.cache(),
		allowLookup: cfg.AllowLookup,
		newTicker:   cfg.newTicker(),
		timeNow:     cfg.timeNow(),
		expiryAge:   cfg.ExpiryAge,
	}

	// Initialize the active versions maps.
	s.active.m = make(map[string]*cachedSecret)
	s.active.f = make(map[string]Secret)
	s.active.w = make(map[string][]Watcher)

	// If we have a cache, try to load data from there first.
	data, err := s.loadCache()
	if err != nil {
		// If we fail to load the cache, treat it as empty.
		s.logf("WARNING: error loading cache: %v (continuing)", err)
	} else if len(data) != 0 {
		// If we fail to decode the cache, treat it as empty.
		if err := json.Unmarshal(data, &s.active.m); err != nil {
			s.logf("WARNING: error decoding cache: %v (continuing)", err)
			clear(s.active.m) // reset
		} else if !s.isActiveSetValid() {
			s.logf("WARNING: cache is not valid; discarding it")
			clear(s.active.m) // reset
		}
	}

	// If there are any configured secrets that weren't cached, stub them in.
	// Any that we loaded from the cache, mark as declared.
	//
	// If we find any missing secrets, we should also perform a cache flush
	// after completing initialization, so that we will have a cache of the
	// latest data in case we restart before the next poll.
	var wantFlush bool
	for _, name := range secrets {
		if _, ok := s.active.m[name]; ok {
			s.active.m[name].Declared = true
		} else {
			s.active.m[name] = nil
			wantFlush = true
		}
	}

	// Ensure we have values for all requested secrets.
	if err := s.initializeActive(ctx); err != nil {
		return nil, err
	}
	if wantFlush {
		if err := s.flushCacheLocked(); err != nil {
			s.logf("WARNING: error flushing cache: %v", err)
		}
	}

	// Plumb secrets in to struct fields, if necessary.
	for _, fi := range structs {
		if err := fi.Apply(ctx, s); err != nil {
			return nil, fmt.Errorf("apply secrets to struct: %w", err)
		}
	}

	// Start a background task to refresh secrets.
	pctx, cancel := context.WithCancel(context.Background())
	s.ctx = pctx
	s.cancel = cancel

	done := make(chan struct{})
	s.done = done

	go s.run(pctx, cfg.pollInterval(), done)

	return s, nil
}

// Close stops the background task polling for updates and waits for it to
// exit.
func (s *Store) Close() error {
	s.cancel()
	<-s.done
	return nil
}

// Refresh synchronously checks for new versions of all the secrets currently
// known by s. It blocks until the refresh is complete or until ctx ends.
//
// Updates are managed automatically when a Store is created and by the polling
// mechanism, but a caller may invoke Refresh directly if it wants to check for
// new secret values at a specific moment.
func (s *Store) Refresh(ctx context.Context) error {
	ch := s.single.DoChan("poll", func() (any, error) {
		s.countPolls.Add(1)
		s.latestPoll.Set(float64(time.Now().UTC().UnixMilli()) / 1000)
		updates := make(map[string]*api.SecretValue)
		if err := s.poll(ctx, updates); err != nil {
			s.countPollErrors.Add(1)
			return nil, fmt.Errorf("[store] update poll failed: %w", err)
		}
		if err := s.applyUpdates(updates); err != nil {
			return nil, fmt.Errorf("[store] applying updates failed: %w", err)
		}
		return nil, nil
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-ch:
		return res.Err
	}
}

// Secret returns a fetcher for the named secret. It returns nil if name does
// not correspond to one of the secrets known by s.
func (s *Store) Secret(name string) Secret {
	s.active.Lock()
	defer s.active.Unlock()
	return s.secretLocked(name)
}

func (s *Store) secretLocked(name string) Secret {
	if _, ok := s.active.m[name]; !ok {
		return nil // unknown secret
	}
	f, ok := s.active.f[name]
	if !ok {
		f = func() []byte {
			s.active.Lock()
			defer s.active.Unlock()

			// Since the caller is actively requesting the value of the secret,
			// update the last-accessed timestamp. This also applies to accesses
			// via a Watcher, since the watchers wrap the underlying Secret.
			s.countSecretFetch.Add(1)
			cs := s.active.m[name]
			cs.LastAccess = s.timeNow().Unix()
			return cs.Secret.Value
		}
		s.active.f[name] = f
	}
	return f
}

// LookupSecret returns a fetcher for the named secret. If name is already
// known by s, this is equivalent to Secret; otherwise, s attempts to fetch the
// latest active version of the secret from the service and either adds it to
// the collection or reports an error.  LookupSecret does not automatically
// retry in case of errors.
func (s *Store) LookupSecret(ctx context.Context, name string) (Secret, error) {
	if f := s.Secret(name); f != nil {
		return f, nil
	} else if !s.allowLookup {
		return nil, errors.New("lookup is not enabled")
	}
	return s.lookupSecretInternal(ctx, name)
}

// lookupSecretInternal fetches the specified secret from the service and,
// if successful, installs it into the active set.
// The caller must not hold the s.active lock; the call to the service is
// performed outside the lock to avoid stalling other readers.
func (s *Store) lookupSecretInternal(ctx context.Context, name string) (Secret, error) {
	// When lookups are enabled, multiple goroutines may race for the right to
	// grab and cache a given secret, so singleflight the lookup for each secret
	// under its own marker. The "lookup:" prefix here ensures we don't collide
	// with the "poll" label used by periodic updates.
	//
	// Note that the winner of the race on the singleflight may time out early,
	// in which case we want to retry (up to a safety limit) when we discover
	// the result was due to a context cancellation other than our own.
	for ctx.Err() == nil {
		v, err, _ := s.single.Do("lookup:"+name, func() (any, error) {
			// If the winning caller's context doesn't already have a deadline,
			// impose a safety fallback so requests do not stall forever if the
			// infrastructure is farkakte.
			dctx := ctx
			if _, ok := ctx.Deadline(); !ok {
				var cancel context.CancelFunc
				dctx, cancel = context.WithTimeout(ctx, 5*time.Minute)
				defer cancel()
			}

			sv, err := s.client.Get(dctx, name)
			if err != nil {
				return nil, fmt.Errorf("lookup %q: %w", name, err)
			}

			s.active.Lock()
			defer s.active.Unlock()
			s.active.m[name] = &cachedSecret{Secret: sv, LastAccess: s.timeNow().Unix()}
			if err := s.flushCacheLocked(); err != nil {
				s.logf("WARNING: error flushing cache: %v", err)
			}
			s.logf("[store] added new undeclared secret %q", name)
			return s.secretLocked(name), nil
		})
		if err == nil {
			return v.(Secret), nil
		} else if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			return nil, err
		}
		// Cancelled or deadline exceeed.  If it was us, we'll fall off the loop.
	}
	return nil, ctx.Err()
}

// Watcher returns a watcher for the named secret. It returns a zero Watcher if
// name does not correspond to one of the secrets known by s.
func (s *Store) Watcher(name string) Watcher {
	s.active.Lock()
	defer s.active.Unlock()
	secret := s.secretLocked(name)
	if secret == nil {
		return Watcher{}
	}
	w := Watcher{ready: make(chan struct{}, 1), secret: secret}
	s.active.w[name] = append(s.active.w[name], w)
	return w
}

// LookupWatcher returns a watcher for the named secret. If name is already
// known by s, this is equivalent to Watcher; otherwise s attempts to fetch the
// latest active version of the secret from the service and either adds it to
// the collection or reports an error.
// LookupWatcher does not automatically retry in case of errors.
func (s *Store) LookupWatcher(ctx context.Context, name string) (Watcher, error) {
	s.active.Lock()
	defer s.active.Unlock()
	var secret Secret
	if _, ok := s.active.m[name]; ok {
		secret = s.secretLocked(name) // OK, we already have it
	} else if !s.allowLookup {
		return Watcher{}, errors.New("lookup is not enabled")
	} else {
		// We must release the lock to fetch from the server; do this in a
		// closure to ensure lock discipline is restored in case of a panic.
		got, err := func() (Secret, error) {
			s.active.Unlock() // NOTE: This order is intended.
			defer s.active.Lock()
			return s.lookupSecretInternal(ctx, name)
		}()
		if err != nil {
			return Watcher{}, err
		}
		secret = got
	}

	w := Watcher{ready: make(chan struct{}, 1), secret: secret}
	s.active.w[name] = append(s.active.w[name], w)
	return w, nil
}

// A Secret is a function that fetches the current active value of a secret.
// The caller should not cache the value returned; the function does not block
// and will always report a valid (if possibly stale) result.
//
// The Secret retains ownership of the bytes returned, but the store will never
// modify the contents of the secret, so it is safe to share the slice without
// copying as long as the caller does not modify them.
type Secret func() []byte

// Get returns the current active value of the secret.  It is a legibility
// alias for calling the function.
func (s Secret) Get() []byte { return s() }

// GetString returns a copy of the current active value of the secret as a
// string.
func (s Secret) GetString() string { return string(s()) }

// StaticSecret returns a Secret that vends a static string value.
// This is useful as a placeholder for development, migration, and testing.
// The value reported by a static secret never changes.
func StaticSecret(value string) Secret {
	return func() []byte { return []byte(value) }
}

// StaticFile returns a Secret that vends the contents of path.  The contents
// of the file are returned exactly as stored.
//
// This is useful as a placeholder for development, migration, and testing.
// The value reported by this secret is the contents of path at the
// time this function is called, and never changes.
func StaticFile(path string) (Secret, error) {
	bs, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading static secret: %w", err)
	}
	return func() []byte { return bs }, nil
}

// StaticTextFile returns a secret that vends the contents of path, which are
// treated as text with leading and trailing whitespace trimmed.
//
// This is useful as a placeholder for development, migration, and testing.
// The value reported by a static secret never changes.
func StaticTextFile(path string) (Secret, error) {
	bs, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading static secret: %w", err)
	}
	text := bytes.TrimSpace(bs)
	return func() []byte { return text }, nil
}

// hasExpired reports whether cs is an undeclared secret whose last access time
// was longer ago than the expiry window.
func (s *Store) hasExpired(cs *cachedSecret) bool {
	if cs.Declared {
		return false // declared secrets do not expire
	} else if s.expiryAge <= 0 {
		return false // no expiry age is defined
	}
	age := s.timeNow().UTC().Sub(cs.lastAccessTime())
	return age > s.expiryAge
}

// snapshotActive captures a point-in-time snapshot of the active names and
// versions of all secrets known to the store. This permits an update poll to
// do the time-consuming lookups outside the lock.
func (s *Store) snapshotActive() map[string]secretState {
	s.active.Lock()
	defer s.active.Unlock()
	m := make(map[string]secretState)
	for name, cs := range s.active.m {
		m[name] = secretState{
			expired: s.hasExpired(cs),
			version: cs.Secret.Version,
		}
	}
	return m
}

// poll polls the service for the active version of each secret in s.active.m.
// It adds an entry to updates for each name that needs to be updated:
// If the named secret has expired, the value is nil.
// Otherwise, the value is a new secret version for that secret.
func (s *Store) poll(ctx context.Context, updates map[string]*api.SecretValue) error {
	var errs []error
	for name, sv := range s.snapshotActive() {
		// If the secret has expired, mark it for deletion.
		if sv.expired {
			updates[name] = nil // nil means "delete me"
			continue
		}

		got, err := s.client.GetIfChanged(ctx, name, sv.version)
		if errors.Is(err, api.ErrValueNotChanged) {
			continue // all is well, but nothing to update
		} else if err != nil {
			errs = append(errs, err)
			continue
		}
		if got.Version != sv.version {
			updates[name] = got
		}
	}
	return errors.Join(errs...)
}

// run runs a polling loop at approximately the given interval until ctx ends,
// then closes done. It should be run in a separate goroutine.
func (s *Store) run(ctx context.Context, interval time.Duration, done chan<- struct{}) {
	defer close(done)

	// Jitter polls by ±10% of the total interval to avert a thundering herd.
	jitter := time.Duration(rand.Intn(2*int(interval)/10) - (int(interval) / 10))

	t := s.newTicker(interval + jitter)
	defer t.Stop()
	doPoll := t.Chan()

	s.logf("[store] begin update poll (interval=%v)", interval+jitter)
	for {
		select {
		case <-ctx.Done():
			s.logf("[store] stopping update poller")
			s.active.Lock()
			defer s.active.Unlock()
			if err := s.flushCacheLocked(); err != nil {
				s.logf("WARNING: error flushing cache: %v", err)
			}
			return
		case <-doPoll:
			if err := s.Refresh(ctx); err != nil {
				s.logf("%s (continuing)", err)
			}
			t.Done()
		}
	}
}

// applyUpdates applies the specified updates to the secret values, and if a
// cache is present flushes the data to the cache.
func (s *Store) applyUpdates(updates map[string]*api.SecretValue) error {
	if len(updates) == 0 {
		return nil // nothing to do
	}
	s.active.Lock()
	defer s.active.Unlock()
	for name, sv := range updates {
		if sv == nil {
			// This is an undeclared secret that has expired.
			// If there are no handles referring to it, remove it.
			if _, ok := s.active.f[name]; ok {
				// This secret has an outstanding handle. Since watchers package
				// secrets, this covers both.
				//
				// This should be a rare case. It could happen if the caller grabs an
				// undeclared secret and then does not access it for a very long time,
				// some weeks say, during which the program does not restart. At that
				// point we may notice the secret has expired (because undeclared and
				// not touched), but we don't want to break the handle.
				continue
			}
			delete(s.active.m, name)
			s.logf("[store] removing expired undeclared secret %q", name)
			continue
		}

		// This is a new value for an unexpired secret.
		// Note that new values do not update access times.
		s.active.m[name].Secret = sv

		// Wake up any watchers pending on new values for this secret.
		for _, w := range s.active.w[name] {
			w.notify()
		}
	}
	return s.flushCacheLocked()
}

func (s *Store) flushCacheLocked() error {
	if s.cache == nil {
		return nil
	}
	data, err := json.Marshal(s.active.m)
	if err != nil {
		return fmt.Errorf("encoding state: %w", err)
	} else if err := s.cache.Write(data); err != nil {
		return fmt.Errorf("updating cache: %w", err)
	}
	return nil
}

func (s *Store) loadCache() ([]byte, error) {
	if s.cache == nil {
		return nil, nil
	}
	return s.cache.Read()
}

// isActiveSetValid reports whether the current active set is valid.
// This is used during initializtion to check cache validity.
func (s *Store) isActiveSetValid() bool {
	for key, cs := range s.active.m {
		if key == "" || cs == nil || cs.Secret == nil {
			return false
		}
	}
	return true
}

func sleepFor(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// initializeActive Waits for active versions to exist for all the requested
// secrets.  Cached versions, even if stale, are OK at this point; they'll get
// refreshed by the update poll.  Any error reported by this method should be
// treated as fatal.
func (s *Store) initializeActive(ctx context.Context) error {
	const baseRetryInterval = 1 * time.Millisecond
	retryWait := baseRetryInterval

	for {
		var missing int
		for name, cs := range s.active.m {
			if cs != nil {
				continue
			}
			sv, err := s.client.Get(ctx, name)
			if err == nil {
				s.active.m[name] = &cachedSecret{
					Secret:     sv,
					LastAccess: s.timeNow().Unix(),
					Declared:   true,

					// The secret in s.active.m is only nil at initialization if
					// name was declared in the StoreConfig and not found in the
					// cache; hence this is a declared secret.
				}
				continue
			} else if ctx.Err() != nil {
				return err // context ended, give up
			}
			s.logf("[store] error fetching %q: %v (retrying)", name, err)
			missing++
		}
		if missing == 0 {
			return nil // succeeded for all values
		}

		// Otherwise, wait a bit and try again, with gentle backoff.
		sleepFor(ctx, retryWait)
		if retryWait < 4*time.Second {
			retryWait += retryWait // caps at 4096ms
		}
	}
}

// A Ticker is used to inject time control in to the polling loop of a store.
type Ticker interface {
	// Chan returns a channel upon which time values are delivered to signal
	// that a poll is required.
	Chan() <-chan time.Time

	// Stop signals that the ticker should stop and deliver no more values.
	Stop()

	// Done is invoked when a signaled poll is complete.
	Done()

	// TODO(creachadair): If we want to plumb in time hints from the service, we
	// can also expose the Reset method here.
}

type stdTicker struct{ *time.Ticker }

func (s stdTicker) Chan() <-chan time.Time { return s.Ticker.C }
func (stdTicker) Done()                    {}

// A Watcher monitors the current active value of a secret, and allows the user
// to be notified when the value of the secret changes.
type Watcher struct {
	ready  chan struct{}
	secret Secret
}

// Get returns the current active value of the secret.
func (w Watcher) Get() []byte { return w.secret.Get() }

// Ready returns a channel that delivers a value when the current active
// version of the secret has changed. The channel is never closed.
//
// The ready channel is a level trigger. The Watcher does not queue multiple
// notifications, and if the caller does not drain the channel subsequent
// notifications will be dropped.
func (w Watcher) Ready() <-chan struct{} { return w.ready }

func (w Watcher) notify() {
	select {
	case w.ready <- struct{}{}:
	default:
	}
}

// NewUpdater creates a new Updater that tracks updates to a value based on new
// secret versions delivered to w.  The newValue function returns a new value
// of the type based on its argument, a secret value.
//
// The initial value is constructed by calling newValue with the current secret
// version in w at the time NewUpdater is called.  Calls to the Get method
// update the value as needed when w changes.
//
// The updater synchronizes calls to Get and newValue, so the callback can
// safely interact with shared state without additional locking.
func NewUpdater[T any](w Watcher, newValue func([]byte) T) *Updater[T] {
	return &Updater[T]{
		newValue: newValue,
		w:        w,
		value:    newValue(w.Get()),
	}
}

// An Updater tracks a value whose state depends on a secret, together with a
// watcher for updates to the secret. The caller provides a function to update
// the value when a new version of the secret is delivered, and the Updater
// manages access and updates to the value.
type Updater[T any] struct {
	newValue func([]byte) T
	w        Watcher
	mu       sync.Mutex
	value    T
}

// Get fetches the current value of u, first updating it if the secret has
// changed.  It is safe to call Get concurrently from multiple goroutines.
func (u *Updater[T]) Get() T {
	u.mu.Lock()
	defer u.mu.Unlock()
	select {
	case <-u.w.Ready():
		u.value = u.newValue(u.w.Get())
	default:
		// no change, use the existing value
	}
	return u.value
}

type cachedSecret struct {
	Secret     *api.SecretValue `json:"secret"`
	LastAccess int64            `json:"lastAccess,string"`
	Declared   bool             `json:"-"` // not persisted

	// Access time is seconds since the Unix epoch in UTC.
}

// lastAccessTime reports the last accessed time of c as a time in UTC.
// It returns the zero time if the last access is 0.
func (c *cachedSecret) lastAccessTime() time.Time {
	if c.LastAccess == 0 {
		return time.Time{}
	}
	return time.Unix(c.LastAccess, 0).UTC()
}

// secretState is the current state of a secret captured during a snapshot.
// The version is the currently-cached version of the secret.
// If expired == true, the secret has expired and should be removed.
type secretState struct {
	expired bool
	version api.SecretVersion
}

// Struct describes a struct value with tagged fields that should be populated
// with secrets from a Store.
type Struct struct {
	// Value must be a non-nil pointer to a value of struct type, having at
	// least one field tagged with a "setec" field tag.
	// See Fields for a description of the tag format.
	Value any

	// Prefix is an optional prefix that should be prepended to each secret
	// described by a tag in Value to obtain the secret name to look up.
	Prefix string
}

func (c StoreConfig) secretNames() ([]string, []*Fields, error) {
	sec := c.Secrets
	var svs []*Fields
	for _, s := range c.Structs {
		fs, err := ParseFields(s.Value, s.Prefix)
		if err != nil {
			return nil, nil, fmt.Errorf("parse struct fields: %w", err)
		}

		// We don't need to deduplicate here, the constructor merges all the
		// names into a map.
		sec = append(sec, fs.Secrets()...)
		svs = append(svs, fs)
	}
	return sec, svs, nil
}

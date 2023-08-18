// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package setec

import (
	"context"
	"encoding/json"
	"errors"
	"expvar"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/tailscale/setec/types/api"
	"tailscale.com/types/logger"
)

// Store is a store that provides named secrets.
type Store struct {
	client    Client // API client
	logf      logger.Logf
	cache     Cache
	newTicker func(time.Duration) Ticker

	active struct {
		// Lock exclusive to modify the contents of the maps.
		// Lock shared to read the keys and values.
		// Values are not mutated once installed in the map.
		sync.RWMutex

		m map[string]*api.SecretValue // :: secret name → active value
		f map[string]Secret           // :: secret name → fetch function
	}

	cancel context.CancelFunc // stops the polling task
	done   <-chan struct{}    // closed when the poller is finished

	// Metrics
	countPolls       expvar.Int    // polls initiated
	countPollErrors  expvar.Int    // errors in polling the service
	countSecretFetch expvar.Int    // count of secret value fetches
	latestPoll       expvar.String // RFC3339Nano, in UTC
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

	// Secrets are the names of the secrets this Store should retrieve. Only
	// secrets named here can be read out of the store. An error is reported if
	// no secrets are listed.
	Secrets []string

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

	// Logf is a logging function where text logs should be sent.  If nil, logs
	// are written to the standard log package.
	Logf logger.Logf

	// PollTicker, if set is a ticker that is used to control the scheduling of
	// update polls. If nil, a time.Ticker is used based on the PollInterval.
	PollTicker Ticker
}

func (c StoreConfig) logger() logger.Logf {
	if c.Logf == nil {
		return log.Printf
	}
	return c.Logf
}

func (c StoreConfig) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return 1 * time.Minute
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
	} else if len(cfg.Secrets) == 0 {
		return nil, errors.New("no secrets are listed")
	}

	s := &Store{
		client:    cfg.Client,
		logf:      cfg.logger(),
		cache:     cfg.cache(),
		newTicker: cfg.newTicker(),
	}

	// Initialize the active versions maps.
	s.active.m = make(map[string]*api.SecretValue)
	s.active.f = make(map[string]Secret)

	// If we have a cache, try to load data from there first.
	data, err := s.loadCache()
	if err != nil {
		// If we fail to load the cache, treat it as empty.
		s.logf("WARNING: error loading cache: %v (continuing)", err)
	} else if len(data) != 0 {
		// If we fail to decode the cache, treat it as empty.
		if err := json.Unmarshal(data, &s.active.m); err != nil {
			s.logf("WARNING: error decoding cache: %v (continuing)", err)
			s.active.m = make(map[string]*api.SecretValue) // reset
		}
	}

	// If there are any configured secrets that weren't cached, stub them in.
	for _, name := range cfg.Secrets {
		if _, ok := s.active.m[name]; !ok {
			s.active.m[name] = nil
		}
	}

	// Ensure we have values for all requested secrets.
	if err := s.initializeActive(ctx); err != nil {
		return nil, err
	}

	// Start a background task to refresh secrets.
	pctx, cancel := context.WithCancel(context.Background())
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

// Secret returns a fetcher for the named secret. It returns nil if name does
// not correspond to one of the secrets known by s.
func (s *Store) Secret(name string) Secret {
	s.active.Lock()
	defer s.active.Unlock()
	if _, ok := s.active.m[name]; !ok {
		return nil // unknown secret
	}
	f, ok := s.active.f[name]
	if !ok {
		f = func() []byte {
			s.active.RLock()
			defer s.active.RUnlock()
			s.countSecretFetch.Add(1)
			return s.active.m[name].Value
		}
		s.active.f[name] = f
	}
	return f
}

// A Secret is a function that fetches the current active value of a secret.
// The caller should not cache the value returned; the function does not block
// and will always report a valid (if possibly stale) result.
type Secret func() []byte

// Get returns the current active value of the secret.  It is a legibility
// alias for calling the function.
func (s Secret) Get() []byte { return s() }

// poll polls the service for the active version of each secret in s.active.m.
// It populates updates with any secret values that have changed.
func (s *Store) poll(ctx context.Context, updates map[string]*api.SecretValue) error {
	s.active.RLock()
	defer s.active.RUnlock()
	var errs []error
	for name, cv := range s.active.m {
		sv, err := s.client.GetIfChanged(ctx, name, cv.Version)
		if errors.Is(err, api.ErrValueNotChanged) {
			continue // all is well, but nothing to update
		} else if err != nil {
			errs = append(errs, err)
			continue
		}
		if cv == nil || sv.Version != cv.Version {
			updates[name] = sv
		}
	}
	return errors.Join(errs...)
}

// run runs a polling loop at approximately the given interval until ctx ends,
// then closes done. It should be run in a separate goroutine.
func (s *Store) run(ctx context.Context, interval time.Duration, done chan<- struct{}) {
	defer close(done)

	// Jitter polls by ±10% of the total interval to avert a thundering herd.
	jitter := time.Duration(rand.Intn(int(interval)/20) - (int(interval) / 10))

	t := s.newTicker(interval + jitter)
	defer t.Stop()
	doPoll := t.Chan()

	s.logf("[store] begin update poll (interval=%v)", interval+jitter)
	for {
		select {
		case <-ctx.Done():
			s.logf("[store] stopping update poller")
			s.active.RLock()
			defer s.active.RUnlock()
			if err := s.flushCacheLocked(); err != nil {
				s.logf("WARNING: error flushing cache: %v", err)
			}
			return
		case <-doPoll:
			s.countPolls.Add(1)
			s.latestPoll.Set(time.Now().Format(time.RFC3339Nano))
			updates := make(map[string]*api.SecretValue)
			if err := s.poll(ctx, updates); err != nil {
				s.countPollErrors.Add(1)
				s.logf("[store] update poll failed: %v (continuing)", err)
			}
			if err := s.applyUpdates(updates); err != nil {
				s.logf("[store] applying updates failed: %v (continuing)", err)
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
		s.active.m[name] = sv
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
		for name, sv := range s.active.m {
			if sv != nil {
				continue
			}
			sv, err := s.client.Get(ctx, name)
			if err == nil {
				s.active.m[name] = sv
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

// A PollTicker is used to inject time control in to the polling loop of a
// store.
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

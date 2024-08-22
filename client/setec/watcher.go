// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package setec

import (
	"context"
	"errors"
)

// A watcher monitors the current active value of a secret, and allows the user
// to be notified when the value of the secret changes.
type watcher struct {
	ready chan struct{}
	Secret
}

// Ready returns a channel that delivers a value when the current active
// version of the secret has changed. The channel is never closed.
//
// The ready channel is a level trigger. The watcher does not queue multiple
// notifications, and if the caller does not drain the channel subsequent
// notifications will be dropped.
func (w watcher) Ready() <-chan struct{} { return w.ready }

func (w watcher) notify() {
	select {
	case w.ready <- struct{}{}:
	default:
	}
}

// lookupWatcher returns a watcher for the named secret. If name is already
// known by s, this is equivalent to watcher; otherwise s attempts to fetch the
// latest active version of the secret from the service and either adds it to
// the collection or reports an error.
// lookupWatcher does not automatically retry in case of errors.
func (s *Store) lookupWatcher(ctx context.Context, name string) (watcher, error) {
	s.active.Lock()
	defer s.active.Unlock()
	var secret Secret
	if _, ok := s.active.m[name]; ok {
		secret = s.secretLocked(name) // OK, we already have it
	} else if !s.allowLookup {
		return watcher{}, errors.New("lookup is not enabled")
	} else {
		// We must release the lock to fetch from the server; do this in a
		// closure to ensure lock discipline is restored in case of a panic.
		got, err := func() (Secret, error) {
			s.active.Unlock() // NOTE: This order is intended.
			defer s.active.Lock()
			return s.lookupSecretInternal(ctx, name)
		}()
		if err != nil {
			return watcher{}, err
		}
		secret = got
	}

	w := watcher{ready: make(chan struct{}, 1), Secret: secret}
	s.active.w[name] = append(s.active.w[name], w)
	return w, nil
}

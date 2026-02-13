// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

//lint:file-ignore U1000 This is work in progress.

package setec

import (
	"sync"

	"github.com/tailscale/setec/types/api"
)

/*
   TODO:
   - Cache needs to be able to hold multiple values.
   - Keep track of single- vs keyring-valued secrets.
   - Update polling logic to use GetAll for keyrings.
   - Add settings.
*/

// A Keyring represents a a collection of one or more available versions of a
// named secret.
type Keyring struct {
	name string

	mu     sync.Mutex
	active int                // index into values
	values []*api.SecretValue // in increasing order by version
}

// Get returns the key at index i of the keyring. Index 0 always refers to the
// current active version, and further indexes refer to other versions in order
// from newest to oldest.
//
// If i >= r.Len(), Get returns nil. If i < 0, Get panics.
//
// The Keyring retains ownership of the bytes returned, but the store will
// never modify the contents of the secret, so it is safe to share the slice
// without copying as long as the caller does not modify them.
func (r *Keyring) Get(i int) []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case i < 0:
		panic("index is negative")
	case i < len(r.values):
		return r.values[i].Value
	default:
		return nil
	}
}

// GetString returns a copy of the key at index i of the keyring as a string.
// If i >= r.Len(), GetString returns "". Otherwise, GetString behaves as Get.
func (r *Keyring) GetString(i int) string { return string(r.Get(i)) }

// Len reports the number of keys stored in r. The length is always positive,
// counting the active version.
func (r *Keyring) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.values)
}

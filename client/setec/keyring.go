// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package setec

import (
	"context"
	"fmt"
	"sync"

	"github.com/tailscale/setec/types/api"
)

// A Keyring represents multiple versions of a single key, each of which can be
// accessed separately.
type Keyring struct {
	// These fields are immutable after construction.
	name   string
	client Client

	mu       sync.Mutex // protectes the fields below
	versions map[api.SecretVersion]*api.SecretValue
	active   api.SecretVersion
}

// Active reports the version and value of the active secret in the keyring.
func (r *Keyring) Active() (api.SecretVersion, []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.versions[r.active]
	return v.Version, v.Value
}

// Get reports whether the keyring contains the specified version of the
// secret, and if so the value of that version.
func (r *Keyring) Get(version api.SecretVersion) ([]byte, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.versions[version]
	if !ok {
		return nil, false
	}
	return v.Value, true
}

// Update updates the contents of r in-place to match the values stored
// on the service. Additional versions will be fetched and added; however,
// versions deleted from the server will not be removed from the keyring.
func (r *Keyring) Update(ctx context.Context) error {
	info, err := r.client.Info(ctx, r.name)
	if err != nil {
		return err
	}

	// Buffer new versions so that we don't modify the keyring until we know the
	// update has succeeded completely.
	var added []*api.SecretValue
	for _, v := range info.Versions {
		if _, ok := r.Get(v); ok {
			continue // we already have this one, don't fetch it again
		}
		sv, err := r.client.GetVersion(ctx, r.name, v)
		if err != nil {
			return fmt.Errorf("get %q version %d: %w", r.name, v, err)
		}
		added = append(added, sv)
	}

	// Reaching here, we have all the new versions, and possibly a new active
	// version as well.
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, av := range added {
		r.versions[av.Version] = av
	}
	r.active = info.ActiveVersion
	return nil
}

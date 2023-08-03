// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// Package db provides a secrets database that is encrypted at rest.
//
// The database is encrypted at rest using a Data Encryption Key
// (DEK). The DEK is stored alongside the database, but is itself
// encrypted at rest using a Key Encryption Key (KEK). In production,
// the KEK should be stored in a key management system like AWS KMS.
//
// This layering of encryption means access to the remote KMS is
// required at Open time, to decrypt the local DEK that in turn can
// decrypt the database proper. But once the DEK has been decrypted
// locally, we can decrypt and re-encrypt the database at will
// (e.g. to save changes) without having a dependency on a remote
// system.
package db

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/tailscale/setec/acl"
	"github.com/tailscale/setec/types/api"
	"github.com/tink-crypto/tink-go/v2/tink"
	"golang.org/x/exp/slices"
)

// DB is an encrypted secrets database.
type DB struct {
	mu sync.Mutex
	kv *kv
}

// We might store some of setec's configuration in the secrets
// database. To do this reliably, reserve a name prefix of secrets
// that have additional semantics (like config validation on put), for
// internal use.
const configPrefix = "_internal/"

var (
	// ErrAccessDenied is the error returned by DB methods when the
	// caller lacks necessary permissions.
	ErrAccessDenied = errors.New("access denied")
	// ErrNotFound is the error returned by DB methods when the
	// database lacks a necessary secret or secret version.
	ErrNotFound = errors.New("not found")
)

// Open loads the secrets database at path, decrypting it using key.
// If no database exists at path, a new empty database is created.
func Open(path string, key tink.AEAD) (*DB, error) {
	kv, err := openOrCreateKV(path, key)
	if err != nil {
		return nil, err
	}

	ret := &DB{
		kv: kv,
	}

	return ret, nil
}

// List returns secret metadata for all secrets on which at least one
// member of 'from' has acl.ActionList permissions.
func (db *DB) List(caps acl.Rules) ([]*api.SecretInfo, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	var ret []*api.SecretInfo
	for _, name := range db.kv.list() {
		if !caps.Allow(acl.ActionList, name) {
			continue
		}
		info, err := db.kv.info(name)
		if err != nil {
			return nil, err
		}
		ret = append(ret, info)
	}
	slices.SortFunc(ret, func(a, b *api.SecretInfo) int { return strings.Compare(a.Name, b.Name) })
	return ret, nil
}

// Info returns metadata for the given secret.
func (db *DB) Info(name string, caps acl.Rules) (*api.SecretInfo, error) {
	if !caps.Allow(acl.ActionList, name) {
		return nil, ErrAccessDenied
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	return db.kv.info(name)
}

// Get returns a secret's active value.
func (db *DB) Get(name string, caps acl.Rules) (*api.SecretValue, error) {
	if !caps.Allow(acl.ActionGet, name) {
		return nil, ErrAccessDenied
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	return db.kv.get(name)
}

// GetVersion returns a secret's value at a specific version.
func (db *DB) GetVersion(name string, version api.SecretVersion, caps acl.Rules) (*api.SecretValue, error) {
	if !caps.Allow(acl.ActionGet, name) {
		return nil, ErrAccessDenied
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	return db.kv.getVersion(name, version)
}

// Put writes value to the secret called name. If the secret already
// exists, value is saved as a new inactive version. Otherwise, value
// is saved as the initial version of the secret and immediately set
// active. On success, returns the secret version for the new value.
func (db *DB) Put(name string, value []byte, caps acl.Rules) (api.SecretVersion, error) {
	if name == "" {
		return 0, errors.New("empty secret name")
	}
	if !caps.Allow(acl.ActionPut, name) {
		return 0, ErrAccessDenied
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	if strings.HasPrefix(name, configPrefix) {
		return db.putConfigLocked(name, value)
	}
	return db.kv.put(name, value)
}

func (db *DB) putConfigLocked(name string, value []byte) (api.SecretVersion, error) {
	switch name {
	default:
		return 0, fmt.Errorf("unknown config value %q", name)
	}
}

// SetActiveVersion changes the active version of the secret called
// name to version.
func (db *DB) SetActiveVersion(name string, version api.SecretVersion, caps acl.Rules) error {
	if name == "" {
		return errors.New("empty secret name")
	}
	if !caps.Allow(acl.ActionSetActive, name) {
		return ErrAccessDenied
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	if strings.HasPrefix(name, configPrefix) {
		return db.activateConfigLocked(name, version)
	}
	return db.kv.setActive(name, version)
}

func (db *DB) activateConfigLocked(name string, version api.SecretVersion) error {
	switch name {
	default:
		return fmt.Errorf("unknown config value %q", name)
	}
}

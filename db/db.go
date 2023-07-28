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
	mu  sync.Mutex
	kv  *kv
	acl *acl.Policy
}

// We store some of setec's configuration in the secrets database. To
// do this reliably, reserve a name prefix of secrets that have
// additional semantics (like config validation on put), for internal
// use.
const (
	configPrefix = "_internal/"
	ConfigACL    = "_internal/acl"
)

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
	if ns := len(kv.list()); ns > 0 && !kv.has(ConfigACL) {
		return nil, fmt.Errorf("database has %d secrets, but no ACL", ns)
	}

	if !kv.has(ConfigACL) {
		return ret, nil
	}

	rawACL, err := kv.get(ConfigACL)
	if err != nil {
		return nil, fmt.Errorf("reading ACLs from database: %w", err)
	} else if pol, err := acl.Compile(rawACL.Value); err != nil {
		return nil, fmt.Errorf("compiling ACLs: %w", err)
	} else {
		ret.acl = pol
	}

	return ret, nil
}

func (db *DB) isMissingACL() bool { return db.acl == nil }

// checkACLLocked reports whether from can do action on secret.
func (db *DB) checkACLLocked(from []string, secret string, action acl.Action) bool {
	if db.isMissingACL() {
		// The only action permitted when no ACL file exists, is to
		// put an initial ACL file.
		if secret == ConfigACL && action == acl.ActionPut {
			return true
		}
		return false
	}
	return db.acl.Allow(from, secret, action)
}

// List returns secret metadata for all secrets on which at least one
// member of 'from' has acl.ActionList permissions.
func (db *DB) List(from []string) ([]*api.SecretInfo, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.isMissingACL() {
		return nil, ErrAccessDenied
	}

	var ret []*api.SecretInfo
	for _, name := range db.kv.list() {
		if !db.checkACLLocked(from, name, acl.ActionList) {
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
func (db *DB) Info(name string, from []string) (*api.SecretInfo, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if !db.checkACLLocked(from, name, acl.ActionList) {
		return nil, ErrAccessDenied
	}

	return db.kv.info(name)
}

// Get returns a secret's active value.
func (db *DB) Get(name string, from []string) (*api.SecretValue, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if !db.checkACLLocked(from, name, acl.ActionGet) {
		return nil, ErrAccessDenied
	}

	return db.kv.get(name)
}

// GetVersion returns a secret's value at a specific version.
func (db *DB) GetVersion(name string, version api.SecretVersion, from []string) (*api.SecretValue, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if !db.checkACLLocked(from, name, acl.ActionGet) {
		return nil, ErrAccessDenied
	}

	return db.kv.getVersion(name, version)
}

// Put writes value to the secret called name. If the secret already
// exists, value is saved as a new inactive version. Otherwise, value
// is saved as the initial version of the secret and immediately set
// active. On success, returns the secret version for the new value.
func (db *DB) Put(name string, value []byte, from []string) (api.SecretVersion, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if !db.checkACLLocked(from, name, acl.ActionPut) {
		return 0, ErrAccessDenied
	}

	if strings.HasPrefix(name, configPrefix) {
		return db.putConfigLocked(name, value)
	}

	return db.kv.put(name, value)
}

func (db *DB) putConfigLocked(name string, value []byte) (api.SecretVersion, error) {
	switch name {
	case ConfigACL:
		pol, err := acl.Compile(value)
		if err != nil {
			return 0, fmt.Errorf("invalid ACL file: %w", err)
		}
		first := !db.kv.has(name)
		ver, err := db.kv.put(name, value)
		if err != nil {
			return 0, err
		}
		// Initial put implicitly sets the new version active.
		if first {
			db.acl = pol
		}
		return ver, err
	default:
		return 0, fmt.Errorf("unknown config value %q", name)
	}
}

// SetActiveVersion changes the active version of the secret called
// name to version.
func (db *DB) SetActiveVersion(name string, version api.SecretVersion, from []string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if !db.checkACLLocked(from, name, acl.ActionSetActive) {
		return ErrAccessDenied
	}

	if strings.HasPrefix(name, configPrefix) {
		return db.activateConfigLocked(name, version)
	}

	return db.kv.setActive(name, version)
}

func (db *DB) activateConfigLocked(name string, version api.SecretVersion) error {
	switch name {
	case ConfigACL:
		raw, err := db.kv.getVersion(name, version)
		if err != nil {
			return err
		}
		pol, err := acl.Compile(raw.Value)
		if err != nil {
			return fmt.Errorf("invalid ACL file: %w", err)
		}
		if err := db.kv.setActive(name, version); err != nil {
			return err
		}
		db.acl = pol
		return nil
	default:
		return fmt.Errorf("unknown config value %q", name)
	}
}

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
	"github.com/tailscale/setec/audit"
	"github.com/tailscale/setec/types/api"
	"github.com/tink-crypto/tink-go/v2/tink"
	"golang.org/x/exp/slices"
	"tailscale.com/util/multierr"
)

// DB is an encrypted secrets database.
type DB struct {
	mu       sync.Mutex
	kv       *kv
	auditLog *audit.Writer
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
func Open(path string, key tink.AEAD, auditLog *audit.Writer) (*DB, error) {
	if auditLog == nil {
		return nil, errors.New("must provide an audit.Writer to db.Open")
	}

	kv, err := openOrCreateKV(path, key)
	if err != nil {
		return nil, err
	}

	ret := &DB{
		kv:       kv,
		auditLog: auditLog,
	}

	return ret, nil
}

// Caller encapsulates a caller identity. It is required by all database
// methods. The contents of Caller should be derived from a tailsale WhoIs
// API call.
type Caller struct {
	// Principal is the caller identity that gets written to audit
	// logs.
	Principal audit.Principal
	// Permissions are the permissions the caller has.
	Permissions acl.Rules
}

// checkAndLog verifies that caller can perform action on secret, and
// writes an appropriate audit log entry.
// The caller must not perform the requested operation if an error is
// returned.
func (db *DB) checkAndLog(caller Caller, action acl.Action, secret string, secretVersion api.SecretVersion) error {
	var errs []error
	authorized := caller.Permissions.Allow(action, secret)
	if !authorized {
		errs = append(errs, ErrAccessDenied)
	}
	err := db.auditLog.WriteEntries(&audit.Entry{
		Principal:     caller.Principal,
		Action:        action,
		Secret:        secret,
		SecretVersion: secretVersion,
		Authorized:    authorized,
	})
	if err != nil {
		errs = append(errs, fmt.Errorf("writing audit log: %w", err))
	}
	return multierr.New(errs...)
}

// List returns secret metadata for all secrets on which at least one
// member of 'from' has acl.ActionList permissions.
func (db *DB) List(caller Caller) ([]*api.SecretInfo, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	// List is unusual, because we don't check a permission
	// upfront. Instead, we return the output of Info() for every
	// secret the caller can access.
	//
	// To avoid spamming the audit log, we record a single audit entry
	// to reflect that List took place, then do per-secret permission
	// checks to construct the response without generating individual
	// audit entries there.
	err := db.auditLog.WriteEntries(&audit.Entry{
		Principal:  caller.Principal,
		Action:     acl.ActionInfo,
		Authorized: true,
	})
	if err != nil {
		return nil, fmt.Errorf("writing audit log: %w", err)
	}

	var ret []*api.SecretInfo
	for _, name := range db.kv.list() {
		if !caller.Permissions.Allow(acl.ActionInfo, name) {
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
func (db *DB) Info(caller Caller, name string) (*api.SecretInfo, error) {
	if err := db.checkAndLog(caller, acl.ActionInfo, name, 0); err != nil {
		return nil, err
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	return db.kv.info(name)
}

// Get returns a secret's active value.
func (db *DB) Get(caller Caller, name string) (*api.SecretValue, error) {
	if err := db.checkAndLog(caller, acl.ActionGet, name, 0); err != nil {
		return nil, err
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	return db.kv.get(name)
}

// GetVersion returns a secret's value at a specific version.
func (db *DB) GetVersion(caller Caller, name string, version api.SecretVersion) (*api.SecretValue, error) {
	if err := db.checkAndLog(caller, acl.ActionGet, name, version); err != nil {
		return nil, err
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	return db.kv.getVersion(name, version)
}

// Put writes value to the secret called name. If the secret already
// exists, value is saved as a new inactive version. Otherwise, value
// is saved as the initial version of the secret and immediately set
// active. On success, returns the secret version for the new value.
func (db *DB) Put(caller Caller, name string, value []byte) (api.SecretVersion, error) {
	if name == "" {
		return 0, errors.New("empty secret name")
	}
	if err := db.checkAndLog(caller, acl.ActionPut, name, 0); err != nil {
		return 0, err
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
func (db *DB) SetActiveVersion(caller Caller, name string, version api.SecretVersion) error {
	if name == "" {
		return errors.New("empty secret name")
	}
	if err := db.checkAndLog(caller, acl.ActionSetActive, name, version); err != nil {
		return err
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

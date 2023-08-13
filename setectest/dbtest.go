// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package setectest

import (
	"io"
	"net/netip"
	"path/filepath"
	"testing"

	"github.com/tailscale/setec/acl"
	"github.com/tailscale/setec/audit"
	"github.com/tailscale/setec/db"
	"github.com/tailscale/setec/types/api"
	"github.com/tink-crypto/tink-go/v2/testutil"
	"github.com/tink-crypto/tink-go/v2/tink"
)

// superuser returns a db.Caller that has full access to all operations on all
// secrete. Each call constructs a fresh value so the caller can make changes
// safely without aliasing the slices.
func superuser() db.Caller {
	return db.Caller{
		Principal: audit.Principal{
			User:     "flynn",
			IP:       netip.MustParseAddr("1.2.3.4"),
			Hostname: "mcp",
		},
		Permissions: acl.Rules{
			acl.Rule{
				Action: []acl.Action{
					acl.ActionGet, acl.ActionInfo, acl.ActionPut, acl.ActionSetActive, acl.ActionDelete,
				},
				Secret: []acl.Secret{"*"},
			},
		},
	}
}

// DB is a wrapper around a setec database to simplify creating database
// instances for unit tests with the testing package.
type DB struct {
	t *testing.T

	Path      string    // the path of the database file
	Key       tink.AEAD // the key-encryption key (dummy)
	Actual    *db.DB    // the underlying database
	Superuser db.Caller // a pre-defined super-user for all secrets & operations
}

// DBOptions are options for constructing a test database.
// A nil *Options is ready for use and provides defaults as described.
type DBOptions struct {
	// AuditLog is where audit logs are written; if nil, audit logs are
	// discarded without error.
	AuditLog *audit.Writer
}

func (o *DBOptions) auditWriter() *audit.Writer {
	if o == nil || o.AuditLog == nil {
		return audit.New(io.Discard)
	}
	return o.AuditLog
}

// NewDB constructs a new empty DB that persists for the duration of the test
// and subtests governed by t. When t ends, the database is cleaned up.
// If opts == nil, default options are used (see DBOptions).
func NewDB(t *testing.T, opts *DBOptions) *DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	key := &testutil.DummyAEAD{Name: "setectest.DB." + t.Name()}
	adb, err := db.Open(path, key, opts.auditWriter())
	if err != nil {
		t.Fatalf("Creating test DB: %v", err)
	}
	return &DB{
		t:         t,
		Path:      path,
		Key:       key,
		Actual:    adb,
		Superuser: superuser(),
	}
}

// MustPut adds the specified secret to the database or fails.
func (db *DB) MustPut(caller db.Caller, name, value string) api.SecretVersion {
	db.t.Helper()

	v, err := db.Actual.Put(caller, name, []byte(value))
	if err != nil {
		db.t.Fatalf("Put %q=%q failed: %v", name, value, err)
	}
	return v
}

// MustGet returns the active version of the named secret or fails.
func (db *DB) MustGet(caller db.Caller, name string) *api.SecretValue {
	db.t.Helper()

	v, err := db.Actual.Get(caller, name)
	if err != nil {
		db.t.Fatalf("Get %q failed: %v", name, err)
	}
	return v
}

// MustGetVersion returns the specified version of the named secret or fails.
func (db *DB) MustGetVersion(caller db.Caller, name string, version api.SecretVersion) *api.SecretValue {
	db.t.Helper()

	v, err := db.Actual.GetVersion(caller, name, version)
	if err != nil {
		db.t.Fatalf("GetVersion %v of %q failed: %v", version, name, err)
	}
	return v
}

// MustSetActiveVersion sets the active version of the named secret or fails.
func (db *DB) MustSetActiveVersion(caller db.Caller, name string, version api.SecretVersion) {
	db.t.Helper()

	if err := db.Actual.SetActiveVersion(caller, name, version); err != nil {
		db.t.Fatalf("SetActiveVersion %v of %q failed: %v", version, name, err)
	}
}

// MustList lists the contents of the database or fails.
func (db *DB) MustList(caller db.Caller) []*api.SecretInfo {
	db.t.Helper()

	vs, err := db.Actual.List(caller)
	if err != nil {
		db.t.Fatalf("List failed: %v", err)
	}
	return vs
}

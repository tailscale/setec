// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package db_test

import (
	"bytes"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/tailscale/setec/acl"
	"github.com/tailscale/setec/audit"
	"github.com/tailscale/setec/db"
	"github.com/tailscale/setec/types/api"
	"github.com/tink-crypto/tink-go/v2/testutil"
	"github.com/tink-crypto/tink-go/v2/tink"
)

func TestCreate(t *testing.T) {
	tdb := newTestDB(t)

	// Verify that the DB was created, and save its bytes to verify
	// that the next open just reads, without mutation.
	bs, err := os.ReadFile(tdb.Path)
	if err != nil {
		t.Fatalf("reading back database: %v", err)
	}

	if _, err = db.Open(tdb.Path, tdb.KEK); err != nil {
		t.Fatalf("opening test DB: %v", err)
	}

	bs2, err := os.ReadFile(tdb.Path)
	if err != nil {
		t.Fatalf("reading back database: %v", err)
	}

	if !bytes.Equal(bs, bs2) {
		t.Fatalf("reread after create mutated on-disk database")
	}
}

type testDB struct {
	Path string
	DB   *db.DB
	KEK  tink.AEAD
}

func newTestDB(t *testing.T) *testDB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	aead := &testutil.DummyAEAD{
		Name: "TestKV-" + t.Name(),
	}
	database, err := db.Open(path, aead)
	if err != nil {
		t.Fatalf("creating test DB: %v", err)
	}
	return &testDB{path, database, aead}
}

func superuser() db.Caller {
	return db.Caller{
		Principal: audit.Principal{
			User:     "flynn",
			IP:       netip.MustParseAddr("1.2.3.4"),
			Hostname: "mcp",
		},
		Permissions: acl.Rules{
			acl.Rule{
				Action: []acl.Action{acl.ActionGet, acl.ActionList, acl.ActionPut, acl.ActionSetActive, acl.ActionDelete},
				Secret: []acl.Secret{"*"},
			},
		},
	}
}

func TestList(t *testing.T) {
	d := newTestDB(t)
	id := superuser()

	checkList := func(d *db.DB, want []*api.SecretInfo) {
		t.Helper()
		l, err := d.List(id)
		if err != nil {
			t.Fatalf("listing secrets: %v", err)
		}
		if diff := cmp.Diff(l, want); diff != "" {
			t.Fatalf("unexpected secret list (-got+want):\n%s", diff)
		}
	}

	checkList(d.DB, []*api.SecretInfo(nil))

	if _, err := d.DB.Put(id, "test", []byte("foo")); err != nil {
		t.Fatalf("putting secret: %v", err)
	}
	checkList(d.DB, []*api.SecretInfo{
		{
			Name:          "test",
			Versions:      []api.SecretVersion{1},
			ActiveVersion: 1,
		},
	})

	if _, err := d.DB.Put(id, "test", []byte("bar")); err != nil {
		t.Fatalf("putting secret: %v", err)
	}
	checkList(d.DB, []*api.SecretInfo{
		{
			Name:          "test",
			Versions:      []api.SecretVersion{1, 2},
			ActiveVersion: 1,
		},
	})

	if _, err := d.DB.Put(id, "test2", []byte("quux")); err != nil {
		t.Fatalf("putting secret: %v", err)
	}
	checkList(d.DB, []*api.SecretInfo{
		{
			Name:          "test",
			Versions:      []api.SecretVersion{1, 2},
			ActiveVersion: 1,
		},
		{
			Name:          "test2",
			Versions:      []api.SecretVersion{1},
			ActiveVersion: 1,
		},
	})

	if err := d.DB.SetActiveVersion(id, "test", api.SecretVersion(2)); err != nil {
		t.Fatalf("setting active version: %v", err)
	}
	checkList(d.DB, []*api.SecretInfo{
		{
			Name:          "test",
			Versions:      []api.SecretVersion{1, 2},
			ActiveVersion: 2,
		},
		{
			Name:          "test2",
			Versions:      []api.SecretVersion{1},
			ActiveVersion: 1,
		},
	})

	d2, err := db.Open(d.Path, d.KEK)
	if err != nil {
		t.Fatalf("reopening database: %v", err)
	}
	checkList(d2, []*api.SecretInfo{
		{
			Name:          "test",
			Versions:      []api.SecretVersion{1, 2},
			ActiveVersion: 2,
		},
		{
			Name:          "test2",
			Versions:      []api.SecretVersion{1},
			ActiveVersion: 1,
		},
	})
}

func TestGet(t *testing.T) {
	d := newTestDB(t)
	id := superuser()

	seen := map[api.SecretVersion][]byte{}
	for i := 0; i < 10; i++ {
		s := []byte(strconv.Itoa(i))
		ver, err := d.DB.Put(id, "test", s)
		if err != nil {
			t.Fatalf("putting secret %d: %v", i, err)
		}
		if seen[ver] != nil {
			t.Fatalf("multiple puts returned version %d", i)
		}
		seen[ver] = s
	}

	sec, err := d.DB.Get(id, "test")
	if err != nil {
		t.Fatalf("getting secret: %v", err)
	}
	if want := []byte("0"); !bytes.Equal(sec.Value, want) {
		t.Fatalf("active secret is %q, want %q", sec.Value, want)
	}

	for v, want := range seen {
		sec, err = d.DB.GetVersion(id, "test", v)
		if err != nil {
			t.Fatalf("getting secret version %d: %v", v, err)
		}
		if !bytes.Equal(sec.Value, want) {
			t.Fatalf("secret version %d is %q, want %q", v, sec.Value, want)
		}

		if err := d.DB.SetActiveVersion(id, "test", v); err != nil {
			t.Fatalf("setting %d as active: %v", v, err)
		}
		sec, err = d.DB.Get(id, "test")
		if err != nil {
			t.Fatalf("getting active secret: %v", err)
		}
		if !bytes.Equal(sec.Value, want) {
			t.Fatalf("active secret is %q, want %q", sec.Value, want)
		}
	}

	d2, err := db.Open(d.Path, d.KEK)
	if err != nil {
		t.Fatalf("reopening database: %v", err)
	}

	for v, want := range seen {
		sec, err = d2.GetVersion(id, "test", v)
		if err != nil {
			t.Fatalf("getting secret version %d: %v", v, err)
		}
		if !bytes.Equal(sec.Value, want) {
			t.Fatalf("secret version %d is %q, want %q", v, sec.Value, want)
		}
	}
}

func TestPut(t *testing.T) {
	d := newTestDB(t)
	id := superuser()

	const testName = "test-secret-name"
	mustPut := func(v []byte) api.SecretVersion {
		t.Helper()
		id, err := d.DB.Put(id, testName, v)
		if err != nil {
			t.Fatalf("Put %q: unexpected error: %v", testName, err)
		}
		return id
	}
	mustGetVersion := func(version api.SecretVersion, want string) *api.SecretValue {
		t.Helper()
		got, err := d.DB.GetVersion(id, testName, version)
		if err != nil {
			t.Fatalf("Get %q version %v: unexpected error: %v", testName, id, err)
		} else if !bytes.Equal(got.Value, []byte(want)) {
			t.Fatalf("Get %q version %v: got %q, want %q", testName, id, got.Value, want)
		}
		return got
	}

	testValue1 := []byte("test value 1")
	testValue2 := []byte("test value 2")

	// Putting a new value should assign a fresh version.
	ver1 := mustPut(testValue1)

	// Re-putting the same value should report the same version.
	ver2 := mustPut(testValue1)
	if ver1 != ver2 {
		t.Fatalf("Put %q again: got %v, want %v", testName, ver2, ver1)
	}

	// Putting a different value must give a new version.
	ver3 := mustPut(testValue2)
	if ver3 == ver1 {
		t.Fatalf("Put %q fresh value: got %v, want a new version", testName, ver3)
	}

	// Putting the original value gets us a new version again.
	ver4 := mustPut(testValue1)
	if ver4 == ver3 {
		t.Fatalf("Put %q fresh value: got %v, want a new version", testName, ver4)
	}

	// The values stored in the database should not alias the input.  The caller
	// may reuse the buffer after storing it.

	testValue1[len(testValue1)-1] = 'Q' // test value Q
	testValue2[len(testValue2)-1] = '?' // test value ?

	v1 := mustGetVersion(ver1, "test value 1")
	v2 := mustGetVersion(ver3, "test value 2")

	// Mutating the values returned by the database should not affect what the
	// database has stored.
	v1.Value[0] = 'Q'
	v2.Value[0] = '?'

	mustGetVersion(ver1, "test value 1")
	mustGetVersion(ver3, "test value 2")
}

// TODO(corp/13375): tests that verify ACL enforcement. Not
// implementing yet because the structure and behavior of ACLs is
// about to change a bunch, and I'd like to not have to implement the
// tests twice.

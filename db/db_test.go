// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package db_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/tailscale/setec/db"
	"github.com/tailscale/setec/types/api"
	"github.com/tink-crypto/tink-go/v2/testutil"
	"github.com/tink-crypto/tink-go/v2/tink"
)

func TestCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	aead := &testutil.DummyAEAD{
		Name: "TestKVCreate",
	}
	if _, err := db.Open(path, aead); err != nil {
		t.Fatalf("creating test DB: %v", err)
	}

	// Verify that the DB was created, and save its bytes to verify
	// that the next open just reads, without mutation.
	bs, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back database: %v", err)
	}

	if _, err = db.Open(path, aead); err != nil {
		t.Fatalf("opening test DB: %v", err)
	}

	bs2, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back database: %v", err)
	}

	if !bytes.Equal(bs, bs2) {
		t.Fatalf("reread after create mutated on-disk database")
	}
}

func TestNoACLNoService(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	aead := &testutil.DummyAEAD{
		Name: "TestNoACLNoService",
	}
	d, err := db.Open(path, aead)
	if err != nil {
		t.Fatal(err)
	}

	from := []string{"root@tailscale.com"}

	if _, err := d.List(from); !errors.Is(err, db.ErrAccessDenied) {
		t.Fatalf("List with no ACLs: got error %v, want %v", err, db.ErrAccessDenied)
	}
	if _, err := d.Get("test", from); !errors.Is(err, db.ErrAccessDenied) {
		t.Fatalf("Get with no ACLs: got error %v, want %v", err, db.ErrAccessDenied)
	}
	if _, err := d.GetVersion("test", 42, from); !errors.Is(err, db.ErrAccessDenied) {
		t.Fatalf("GetVersion with no ACLs: got error %v, want %v", err, db.ErrAccessDenied)
	}
	if v, err := d.Put("", []byte("ouch"), from); err == nil {
		t.Fatalf("Put with empty secret name: got %+v, want error", v)
	}
	if _, err := d.Put("test", []byte("123"), from); !errors.Is(err, db.ErrAccessDenied) {
		t.Fatalf("Put with no ACLs: got error %v, want %v", err, db.ErrAccessDenied)
	}
	if err := d.SetActiveVersion("test", 42, from); !errors.Is(err, db.ErrAccessDenied) {
		t.Fatalf("SetActiveVersion with no ACLs: got error %v, want %v", err, db.ErrAccessDenied)
	}

	const acl = `{
  "rules": [{
    "principal": ["root@tailscale.com"],
    "action": ["get", "list", "put", "set-active", "delete"],
    "secret": ["*"],
  }],
}`
	aclVer, err := d.Put(db.ConfigACL, []byte(acl), from)
	if err != nil {
		t.Fatalf("setting ACL: %v", err)
	}
	if want := api.SecretVersion(1); aclVer != want {
		t.Fatalf("initial ACL version is %d, want %d", aclVer, want)
	}

	if _, err := d.List(from); err != nil {
		t.Fatalf("List with ACLs: %v", err)
	}
	ver, err := d.Put("test", []byte("123"), from)
	if err != nil {
		t.Fatalf("Put with ACLs: %v", err)
	}
	if _, err := d.Get("test", from); err != nil {
		t.Fatalf("Get with ACLs: %v", err)
	}
	if _, err := d.GetVersion("test", ver, from); err != nil {
		t.Fatalf("GetVersion with ACLs: %v", err)
	}
	v2, err := d.Put("test", []byte("234"), from)
	if err != nil {
		t.Fatalf("Put with ACLs: %v", err)
	}
	if err := d.SetActiveVersion("test", v2, from); err != nil {
		t.Fatalf("SetActiveVersion with ACLs: %v", err)
	}
}

type testDB struct {
	Path string
	DB   *db.DB
	KEK  tink.AEAD
}

func dbWithACL(t *testing.T, acl string) *testDB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	aead := &testutil.DummyAEAD{
		Name: "TestKVCreate",
	}
	database, err := db.Open(path, aead)
	if err != nil {
		t.Fatalf("creating test DB: %v", err)
	}
	if _, err := database.Put(db.ConfigACL, []byte(acl), []string{}); err != nil {
		t.Fatalf("setting ACL: %v", err)
	}
	return &testDB{path, database, aead}
}

func dbWithFullAccess(t *testing.T) (database *testDB, from []string) {
	t.Helper()
	const acl = `{
  "rules": [{
    "principal": ["root@tailscale.com"],
    "action": ["get", "list", "put", "set-active", "delete"],
    "secret": ["*"],
  }],
}`
	return dbWithACL(t, acl), []string{"root@tailscale.com"}
}

func TestList(t *testing.T) {
	d, from := dbWithFullAccess(t)

	checkList := func(d *db.DB, want []*api.SecretInfo) {
		t.Helper()
		l, err := d.List(from)
		if err != nil {
			t.Fatalf("listing secrets: %v", err)
		}
		if diff := cmp.Diff(l, want); diff != "" {
			t.Fatalf("unexpected secret list (-got+want):\n%s", diff)
		}
	}

	checkList(d.DB, []*api.SecretInfo{
		{
			Name:          db.ConfigACL,
			Versions:      []api.SecretVersion{1},
			ActiveVersion: 1,
		},
	})

	if _, err := d.DB.Put("test", []byte("foo"), from); err != nil {
		t.Fatalf("putting secret: %v", err)
	}
	checkList(d.DB, []*api.SecretInfo{
		{
			Name:          db.ConfigACL,
			Versions:      []api.SecretVersion{1},
			ActiveVersion: 1,
		},
		{
			Name:          "test",
			Versions:      []api.SecretVersion{1},
			ActiveVersion: 1,
		},
	})

	if _, err := d.DB.Put("test", []byte("bar"), from); err != nil {
		t.Fatalf("putting secret: %v", err)
	}
	checkList(d.DB, []*api.SecretInfo{
		{
			Name:          db.ConfigACL,
			Versions:      []api.SecretVersion{1},
			ActiveVersion: 1,
		},
		{
			Name:          "test",
			Versions:      []api.SecretVersion{1, 2},
			ActiveVersion: 1,
		},
	})

	if _, err := d.DB.Put("test2", []byte("quux"), from); err != nil {
		t.Fatalf("putting secret: %v", err)
	}
	checkList(d.DB, []*api.SecretInfo{
		{
			Name:          db.ConfigACL,
			Versions:      []api.SecretVersion{1},
			ActiveVersion: 1,
		},
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

	if err := d.DB.SetActiveVersion("test", api.SecretVersion(2), from); err != nil {
		t.Fatalf("setting active version: %v", err)
	}
	checkList(d.DB, []*api.SecretInfo{
		{
			Name:          db.ConfigACL,
			Versions:      []api.SecretVersion{1},
			ActiveVersion: 1,
		},
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
			Name:          db.ConfigACL,
			Versions:      []api.SecretVersion{1},
			ActiveVersion: 1,
		},
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
	d, from := dbWithFullAccess(t)

	seen := map[api.SecretVersion][]byte{}
	for i := 0; i < 10; i++ {
		s := []byte(strconv.Itoa(i))
		ver, err := d.DB.Put("test", s, from)
		if err != nil {
			t.Fatalf("putting secret %d: %v", i, err)
		}
		if seen[ver] != nil {
			t.Fatalf("multiple puts returned version %d", i)
		}
		seen[ver] = s
	}

	sec, err := d.DB.Get("test", from)
	if err != nil {
		t.Fatalf("getting secret: %v", err)
	}
	if want := []byte("0"); !bytes.Equal(sec.Value, want) {
		t.Fatalf("active secret is %q, want %q", sec.Value, want)
	}

	for v, want := range seen {
		sec, err = d.DB.GetVersion("test", v, from)
		if err != nil {
			t.Fatalf("getting secret version %d: %v", v, err)
		}
		if !bytes.Equal(sec.Value, want) {
			t.Fatalf("secret version %d is %q, want %q", v, sec.Value, want)
		}

		if err := d.DB.SetActiveVersion("test", v, from); err != nil {
			t.Fatalf("setting %d as active: %v", v, err)
		}
		sec, err = d.DB.Get("test", from)
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
		sec, err = d2.GetVersion("test", v, from)
		if err != nil {
			t.Fatalf("getting secret version %d: %v", v, err)
		}
		if !bytes.Equal(sec.Value, want) {
			t.Fatalf("secret version %d is %q, want %q", v, sec.Value, want)
		}
	}
}

// TODO(corp/13375): tests that verify ACL enforcement. Not
// implementing yet because the structure and behavior of ACLs is
// about to change a bunch, and I'd like to not have to implement the
// tests twice.

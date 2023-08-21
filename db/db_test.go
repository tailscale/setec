// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package db_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/tailscale/setec/audit"
	"github.com/tailscale/setec/db"
	"github.com/tailscale/setec/setectest"
	"github.com/tailscale/setec/types/api"
)

func TestCreate(t *testing.T) {
	tdb := setectest.NewDB(t, nil)

	// Verify that the DB was created, and save its bytes to verify
	// that the next open just reads, without mutation.
	bs, err := os.ReadFile(tdb.Path)
	if err != nil {
		t.Fatalf("reading back database: %v", err)
	}

	if _, err = db.Open(tdb.Path, tdb.Key, audit.New(io.Discard)); err != nil {
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

func TestList(t *testing.T) {
	d := setectest.NewDB(t, nil)
	id := d.Superuser

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

	checkList(d.Actual, []*api.SecretInfo(nil))

	d.MustPut(id, "test", "foo")
	checkList(d.Actual, []*api.SecretInfo{
		{
			Name:          "test",
			Versions:      []api.SecretVersion{1},
			ActiveVersion: 1,
		},
	})

	d.MustPut(id, "test", "bar")
	checkList(d.Actual, []*api.SecretInfo{
		{
			Name:          "test",
			Versions:      []api.SecretVersion{1, 2},
			ActiveVersion: 1,
		},
	})

	d.MustPut(id, "test2", "quux")
	checkList(d.Actual, []*api.SecretInfo{
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

	d.MustActivate(id, "test", 2)
	checkList(d.Actual, []*api.SecretInfo{
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

	d2, err := db.Open(d.Path, d.Key, audit.New(io.Discard))
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
	d := setectest.NewDB(t, nil)
	id := d.Superuser

	seen := map[api.SecretVersion][]byte{}
	for i := 0; i < 10; i++ {
		s := strconv.Itoa(i)
		ver := d.MustPut(id, "test", s)
		if seen[ver] != nil {
			t.Fatalf("multiple puts returned version %d", i)
		}
		seen[ver] = []byte(s)
	}

	sec := d.MustGet(id, "test")
	if want := []byte("0"); !bytes.Equal(sec.Value, want) {
		t.Fatalf("active secret is %q, want %q", sec.Value, want)
	}

	for v, want := range seen {
		sec := d.MustGetVersion(id, "test", v)
		if !bytes.Equal(sec.Value, want) {
			t.Fatalf("secret version %d is %q, want %q", v, sec.Value, want)
		}

		d.MustActivate(id, "test", v)
		sec = d.MustGet(id, "test")
		if !bytes.Equal(sec.Value, want) {
			t.Fatalf("active secret is %q, want %q", sec.Value, want)
		}
	}

	d2, err := db.Open(d.Path, d.Key, audit.New(io.Discard))
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
	d := setectest.NewDB(t, nil)
	id := d.Superuser

	const testName = "test-secret-name"
	mustGetVersion := func(version api.SecretVersion, want string) *api.SecretValue {
		t.Helper()
		got := d.MustGetVersion(id, testName, version)
		if !bytes.Equal(got.Value, []byte(want)) {
			t.Fatalf("Get %q version %v: got %q, want %q", testName, id, got.Value, want)
		}
		return got
	}

	testValue1 := []byte("test value 1")
	testValue2 := []byte("test value 2")

	// Putting a new value should assign a fresh version.
	ver1 := d.MustPut(id, testName, string(testValue1))

	// Re-putting the same value should report the same version.
	ver2 := d.MustPut(id, testName, string(testValue1))
	if ver1 != ver2 {
		t.Fatalf("Put %q again: got %v, want %v", testName, ver2, ver1)
	}

	// Putting a different value must give a new version.
	ver3 := d.MustPut(id, testName, string(testValue2))
	if ver3 == ver1 {
		t.Fatalf("Put %q fresh value: got %v, want a new version", testName, ver3)
	}

	// Putting the original value gets us a new version again.
	ver4 := d.MustPut(id, testName, string(testValue1))
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

func TestDelete(t *testing.T) {
	d := setectest.NewDB(t, nil)
	id := d.Superuser

	const testName = "test-secret-name"
	v1 := d.MustPut(id, testName, "ver1")

	// Case 1: Deleting a secret that isn't there should succeed.
	if err := d.Actual.Delete(id, "nonesuch"); err != nil {
		t.Errorf("Delete nonesuch: unexpected error: %v", err)
	}

	// Case 2: Deleting a secret that exists should succeed.
	if err := d.Actual.Delete(id, testName); err != nil {
		t.Errorf("Delete %q: got %v, want nil", testName, err)
	}

	// Case 3: After deleting, we cannot retrieve the secret.
	if got, err := d.Actual.GetVersion(id, testName, v1); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("GetVersion %v: got (%v, %v), want %v", v1, got, err, db.ErrNotFound)
	}
}

func TestDeleteVersion(t *testing.T) {
	d := setectest.NewDB(t, nil)
	id := d.Superuser

	const testName = "test-secret-name"
	v1 := d.MustPut(id, testName, "version1") // active
	v2 := d.MustPut(id, testName, "version2")

	// Case 1: Deleting a non-existent version fails.
	if err := d.Actual.DeleteVersion(id, testName, 1000); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("DeleteVersion 1000: got %v, want %v", err, db.ErrNotFound)
	}

	// Case 2: Deleting the active version fails.
	if err := d.Actual.DeleteVersion(id, testName, v1); err == nil {
		t.Errorf("DeleteVersion %v: got nil, want error", v1)
	}

	// Case 3: Deleting an inactive version succeeds.
	if err := d.Actual.DeleteVersion(id, testName, v2); err != nil {
		t.Errorf("DeleteVersion %v: got %v, want nil", v2, err)
	}

	// Case 4: The deleted version can no longer be retrieved.
	if got, err := d.Actual.GetVersion(id, testName, v2); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("GetVersion %v: got (%v, %v), want error %v", v2, got, err, db.ErrNotFound)
	}

	// Case 5: The active version still exists.
	d.MustGetVersion(id, testName, v1)
}

// TODO(corp/13375): tests that verify ACL enforcement. Not
// implementing yet because the structure and behavior of ACLs is
// about to change a bunch, and I'd like to not have to implement the
// tests twice.

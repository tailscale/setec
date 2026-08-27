// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package audit_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/tailscale/setec/acl"
	"github.com/tailscale/setec/audit"
)

func TestWriter(t *testing.T) {
	out := new(testWriter)

	w := audit.NewWriter(out)

	entries := []*audit.Entry{
		{
			Principal: audit.Principal{
				Hostname: "foo",
				IP:       netip.MustParseAddr("1.2.3.4"),
				User:     "flynn",
			},
			Action:        "get",
			Secret:        "mcp/core/tron",
			SecretVersion: 4,
		},
		{
			Principal: audit.Principal{
				Hostname: "bar",
				IP:       netip.MustParseAddr("2.3.4.5"),
				User:     "dillinger",
			},
			Action:        "delete",
			Secret:        "mcp/core/tron",
			SecretVersion: 0,
		},
	}

	if err := w.WriteEntries(entries...); err != nil {
		t.Fatalf("writing audit log entries: %v", err)
	}
	// Verify that WriteEntries set ID and Time
	for i, e := range entries {
		if e.ID == 0 {
			t.Fatalf("ID was not set on entry %d", i+1)
		}
		if e.Time.IsZero() {
			t.Fatalf("Time was not set on entry %d", i+1)
		}
	}

	// Verify that close properly calls both Sync and Close if they are
	// implemented.
	out.syncErr = errors.New("sync failed")
	w.Close()
	if !out.synced {
		t.Error("After Close: Sync was not called")
	}
	if !out.closed {
		t.Errorf("After Close: Close was not called")
	}

	dec := json.NewDecoder(&out.Buffer)
	var got []*audit.Entry
	for i := 0; i < len(entries); i++ {
		var ent *audit.Entry
		if err := dec.Decode(&ent); err != nil {
			t.Fatalf("decoding audit entry %d: %v", i+1, err)
		}
		got = append(got, ent)
	}

	if diff := cmp.Diff(got, entries, cmp.Comparer(addrEqual)); diff != "" {
		t.Fatalf("wrong audit log data on read-back (-got+want):\n%s", diff)
	}
}

type testWriter struct {
	bytes.Buffer
	syncErr        error
	synced, closed bool
}

func (t *testWriter) Sync() error  { t.synced = true; return t.syncErr }
func (t *testWriter) Close() error { t.closed = true; return nil }

func addrEqual(x, y netip.Addr) bool { return x == y }

func TestReader(t *testing.T) {
	// To test the audit.Reader, encode some fixed entries, then verify that
	// reading them back in produces the same values.
	base := time.Now()
	entries := []*audit.Entry{{
		ID:   123,
		Time: base,
		Principal: audit.Principal{
			Hostname: "window",
			IP:       netip.MustParseAddr("1.2.3.4"),
			User:     "anathema",
		},
		Action:        acl.ActionGet,
		Authorized:    true,
		Secret:        "grey/mousie",
		SecretVersion: 1,
	}, {
		ID:   456,
		Time: base.Add(3 * time.Second),
		Principal: audit.Principal{
			Hostname: "bookshelf",
			IP:       netip.MustParseAddr("2.3.4.5"),
			User:     "zuul",
		},
		Action:        acl.ActionPut,
		Authorized:    true,
		Secret:        "brown/rabbit",
		SecretVersion: 4,
	}, {
		ID:   789,
		Time: base.Add(5 * time.Second),
		Principal: audit.Principal{
			Hostname: "fireplace",
			IP:       netip.MustParseAddr("3.4.5.6"),
			Tags:     []string{"tag:asha", "tag:athena"},
		},
		Action:        acl.ActionActivate,
		Authorized:    false,
		Secret:        "white/mushroom",
		SecretVersion: 101,
	}}

	// Write the test log entries out into a memory buffer.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for i, e := range entries {
		if err := enc.Encode(e); err != nil {
			t.Fatalf("Encode entry %d: %v", i+1, err)
		}
	}

	// Scan back through the buffer to decode the entries.
	var got []*audit.Entry
	for e, err := range audit.NewReader(&buf).All() {
		if err != nil {
			t.Errorf("Next entry: unexpected error: %v", err)
			continue
		}
		got = append(got, e)
	}
	if diff := cmp.Diff(got, entries, cmpopts.EquateComparable(netip.Addr{})); diff != "" {
		t.Errorf("Read results (-got, +want):\n%s", diff)
	}
}

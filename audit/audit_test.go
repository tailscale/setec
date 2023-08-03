// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package audit_test

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/tailscale/setec/audit"
)

func TestWriter(t *testing.T) {
	var out bytes.Buffer

	w := audit.New(&out)

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

	dec := json.NewDecoder(&out)
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

func addrEqual(x, y netip.Addr) bool { return x == y }

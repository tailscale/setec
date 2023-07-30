// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package acl_test

import (
	"testing"

	"github.com/tailscale/setec/acl"
)

func TestACL(t *testing.T) {
	const src = `
{
  "rules": [
    {
      "principal": ["tag:control"],
      "action": ["get"],
      "secret": ["control/foo", "control/bar"],
    },
    {
      "principal": ["group:debug", "tag:lab", "c@ts.net"],
      "action": ["get"],
      "secret": ["quux"],
    },
    {
      "principal": ["group:admins"],
      "action": ["get", "list", "put", "set-active", "delete"],
      "secret": ["*"],
    },
    {
      "principal": ["c@ts.net"],
      "action": ["get", "list", "put", "set-active", "delete"],
      "secret": ["dev/*", "prod/*/some-secret"],
    },
  ],
}`
	pol, err := acl.Compile([]byte(src))
	if err != nil {
		t.Fatal(err)
	}

	type testCase struct {
		from   []string
		secret string
		action acl.Action
		want   bool
	}
	allow := func(action, secret string, from ...string) testCase {
		return testCase{from, secret, acl.Action(action), true}
	}
	deny := func(action, secret string, from ...string) testCase {
		return testCase{from, secret, acl.Action(action), false}
	}
	tests := []testCase{
		allow("get", "control/foo", "tag:control"),
		allow("get", "control/foo", "tag:other", "tag:control"),
		allow("get", "control/bar", "tag:control"),
		deny("get", "control/quux", "tag:control"),
		deny("get", "control/foo", "tag:other"),
		deny("list", "control/foo", "tag:control"),
		deny("put", "control/foo", "tag:control"),
		deny("set-active", "control/foo", "tag:control"),
		deny("delete", "control/foo", "tag:control"),
		deny("put", "control/other", "tag:control"),

		allow("get", "quux", "c@ts.net", "group:debug"),
		allow("get", "quux", "tag:lab"),
		allow("get", "quux", "tag:other", "tag:lab"),
		allow("get", "quux", "tag:other", "tag:server", "tag:lab"),
		allow("get", "quux", "c@ts.net"),
		deny("put", "quux", "c@ts.net"),
		deny("put", "quux", "tag:server"),
		deny("put", "quux", "tag:other"),

		allow("get", "control/foo", "a@ts.net", "group:admins"),
		allow("put", "control/foo", "b@ts.net", "group:admins"),
		allow("list", "quux", "b@ts.net", "group:admins"),
		allow("set-active", "quux", "b@ts.net", "group:admins"),
		allow("delete", "quux", "b@ts.net", "group:admins"),
		deny("get", "control/foo", "a@ts.net"),
		deny("get", "control/foo", "b@ts.net", "group:unrelated"),

		allow("get", "dev/foo", "c@ts.net"),
		allow("put", "dev/foo", "c@ts.net"),
		allow("list", "dev/bar", "c@ts.net"),
		allow("set-active", "dev/bar", "c@ts.net"),
		allow("delete", "dev/bar", "c@ts.net"),
		allow("get", "prod/foo/some-secret", "c@ts.net"),
		allow("put", "prod/foo/some-secret", "c@ts.net"),
		allow("list", "prod/quux/some-secret", "c@ts.net"),
		allow("set-active", "prod/other/some-secret", "c@ts.net"),
		allow("delete", "prod/wat/some-secret", "c@ts.net"),
		deny("get", "prod/other/suffix", "c@ts.net"),
		deny("get", "prod/some-secret", "c@ts.net"),
	}

	for _, test := range tests {
		if got := pol.Allow(test.from, test.secret, test.action); got != test.want {
			t.Errorf("Allow(%v, %q, %q) = %v, want %v", test.from, test.secret, test.action, got, test.want)
		}
	}
}

func TestInvalidPolicy(t *testing.T) {
	reject := map[string]string{
		"gibberish":        `gthgoht34h89`,
		"unknown-field":    `{"blork": 42}`,
		"wrong-rules-type": `{"rules": "short and stout"}`,
		"unknown-action": `{
  "rules": [{
    "action": ["oscillate-the-overthruster"],
    "secret": ["*"],
    "principal": ["a@ts.net"],
  }]
}`,
	}

	for n, r := range reject {
		if _, err := acl.Compile([]byte(r)); err == nil {
			t.Errorf("parse %q succeeded, want error", n)
		}
	}
}

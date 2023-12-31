// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package acl_test

import (
	"testing"

	"github.com/tailscale/setec/acl"
)

func TestACL(t *testing.T) {
	rules := acl.Rules{
		acl.Rule{
			Action: []acl.Action{acl.ActionGet},
			Secret: []acl.Secret{"control/foo", "control/bar"},
		},
		acl.Rule{
			Action: []acl.Action{acl.ActionInfo, acl.ActionPut, acl.ActionActivate},
			Secret: []acl.Secret{"*"},
		},
		acl.Rule{
			Action: []acl.Action{acl.ActionDelete},
			Secret: []acl.Secret{"dev/*"},
		},
	}

	type testCase struct {
		secret string
		action acl.Action
		want   bool
	}
	allow := func(action, secret string) testCase {
		return testCase{secret, acl.Action(action), true}
	}
	deny := func(action, secret string) testCase {
		return testCase{secret, acl.Action(action), false}
	}
	tests := []testCase{
		allow("get", "control/foo"),
		allow("get", "control/bar"),
		deny("get", "control/quux"),
		deny("get", "something/else"),
		deny("get", "dev/foo"),

		allow("info", "control/foo"),
		allow("info", "control/bar"),
		allow("info", "control/quux"),
		allow("info", "something/else"),
		allow("info", "dev/foo"),

		allow("put", "control/foo"),
		allow("put", "control/bar"),
		allow("put", "control/quux"),
		allow("put", "something/else"),
		allow("put", "dev/foo"),

		allow("activate", "control/foo"),
		allow("activate", "control/bar"),
		allow("activate", "control/quux"),
		allow("activate", "something/else"),
		allow("activate", "dev/foo"),

		allow("delete", "dev/foo"),
		allow("delete", "dev/bar/quux"),
		deny("delete", "control/foo"),
		deny("delete", "control/bar"),
		deny("delete", "something/else"),
		deny("delete", "dev"),
	}

	for _, test := range tests {
		if got := rules.Allow(test.action, test.secret); got != test.want {
			t.Errorf("Allow(%q, %q) = %v, want %v", test.action, test.secret, got, test.want)
		}
	}
}

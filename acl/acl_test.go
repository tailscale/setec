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
			Action: []acl.Action{acl.ActionList, acl.ActionPut, acl.ActionSetActive},
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

		allow("list", "control/foo"),
		allow("list", "control/bar"),
		allow("list", "control/quux"),
		allow("list", "something/else"),
		allow("list", "dev/foo"),

		allow("put", "control/foo"),
		allow("put", "control/bar"),
		allow("put", "control/quux"),
		allow("put", "something/else"),
		allow("put", "dev/foo"),

		allow("set-active", "control/foo"),
		allow("set-active", "control/bar"),
		allow("set-active", "control/quux"),
		allow("set-active", "something/else"),
		allow("set-active", "dev/foo"),

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

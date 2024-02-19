// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// Package acl implements ACL evaluation for access to a secrets
// database.
//
// ACL policies are provided by tailscale peer capabilities.
package acl

import (
	"fmt"
	"regexp"
	"strings"
)

// Action is an action on secrets that is subject to access control.
type Action string

const (
	// ActionGet ("get" in the API) denotes permission to fetch the contents of a secret.
	//
	// Note: ActionGet does not imply ActionInfo, or vice versa.
	ActionGet = Action("get")

	// ActionInfo ("info" in the API) denotes permission to read the metadata
	// for a secret, including available and active version numbers, but not the
	// secret values.
	ActionInfo = Action("info")

	// ActionPut ("put" in the API) denotes permission to put a new value of a
	// secret.
	ActionPut = Action("put")

	// ActionActivate ("activate" in the API) denotes permission to set one one
	// of of the available versions of a secret as the active one.
	ActionActivate = Action("activate")

	// ActionDelete ("delete" in the API) denotes permission to delete secret
	// versions, either individually or entirely.
	ActionDelete = Action("delete")
)

// Secret is a secret name pattern that can optionally contain '*' wildcard
// characters. The wildcard means "zero or more of any character here."
type Secret string

// Match reports whether the Secret name pattern matches val.
func (pat Secret) Match(val string) bool {
	s := string(pat)
	if !strings.Contains(s, "*") && s == val {
		return true
	}
	// We want the user to use glob-ish syntax, where '*' is the
	// equivalent of regexp's '.*'. We also don't want any other
	// character of the input misinterpreted as a regexp control
	// character.
	//
	// To achieve this, we:
	//  - split each input string on '*'
	//  - regexp-quote the resulting parts
	//  - reassemble the quoted parts around '.*' separators
	parts := strings.Split(s, "*")
	for i := range parts {
		parts[i] = regexp.QuoteMeta(parts[i])
	}
	re := regexp.MustCompile(fmt.Sprintf("^%s$", strings.Join(parts, ".*")))
	return re.MatchString(val)
}

// Rules is a set of ACLs for access to a secret.
type Rules []Rule

// Allow reports whether the ACLs allow action on secret.
func (rr Rules) Allow(action Action, secret string) bool {
	for _, r := range rr {
		if r.Allow(action, secret) {
			return true
		}
	}
	return false
}

// Rule is an access control rule that permits some actions on some
// secrets. Secrets can contain '*' wildcards, which match zero or
// more characters.
type Rule struct {
	Action []Action `json:"action"`
	Secret []Secret `json:"secret"`
}

// Allow reports whether the rule allows action on secret.
func (r *Rule) Allow(action Action, secret string) bool {
	actionMatches := func(acts []Action) bool {
		for _, a := range acts {
			if a == action {
				return true
			}
		}
		return false
	}
	secretMatches := func(secs []Secret) bool {
		for _, s := range secs {
			if s.Match(secret) {
				return true
			}
		}
		return false
	}
	return actionMatches(r.Action) && secretMatches(r.Secret)
}

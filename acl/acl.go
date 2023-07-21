// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// Package acl implements ACL evaluation for access to a secrets
// database.
//
// ACL policy files are a HuJSON object that looks like:
//
//	{
//	  "groups": {
//	    "admins": ["dave@tailscale.com", "kylie@tailscale.com", "fromberger@tailscale.com"],
//	    "log-sources": ["tag:server", "tag:lab"],
//	  },
//	  "rules": [
//	    {
//	      "principal": ["tag:control", "tag:control-us"],
//	      "action": ["get"],
//	      "secret": ["prod/control/rudderstack-api-key", "prod/control/stripe-api-key"],
//	    },
//	    {
//	      "principal": ["group:log-sources", "kylie@tailscale.com"],
//	      "action": ["get"],
//	      "secret": ["prod/elastic-agent-authkey"],
//	    },
//	    {
//	      "principal": ["group:admins"],
//	      "action": ["get", "list", "put", "set-active", "delete"],
//	      "secret": ["*"],
//	    },
//	    {
//	      "principal": ["dave@tailscale.com"],
//	      "action": ["get", "list", "put", "set-active", "delete"],
//	      "secret": ["dev/*", "prod/*/some-secret"],
//	    },
//	  ],
//	}
package acl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/tailscale/hujson"
	"tailscale.com/util/multierr"
)

// Action is an action on secrets that is subject to access control.
type Action string

const (
	ActionGet       = Action("get")
	ActionList      = Action("list")
	ActionPut       = Action("put")
	ActionSetActive = Action("set-active")
	ActionDelete    = Action("delete")
)

// Policy is an ACL policy that controls access to secrets.
type Policy struct {
	groups map[string][]string
	rules  []compiledRule
}

// Compile returns a Policy that enforces the ACLs in bs.
func Compile(bs []byte) (*Policy, error) {
	bs, err := hujson.Standardize(bs)
	if err != nil {
		return nil, fmt.Errorf("converting ACL policy to JSON: %w", err)
	}

	type rule struct {
		From   []string `json:"principal"`
		Secret []string `json:"secret"`
		Action []Action `json:"action"`
	}
	var pol struct {
		Groups map[string][]string `json:"groups"`
		Rules  []rule              `json:"rules"`
	}
	dec := json.NewDecoder(bytes.NewBuffer(bs))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&pol); err != nil {
		return nil, fmt.Errorf("parsing ACL policy JSON: %w", err)
	}

	ret := &Policy{
		groups: pol.Groups,
	}
	var errs []error

	for i, r := range pol.Rules {
		ruleNum := i + 1

		from, err := expandFrom(ruleNum, r.From, ret.groups)
		if err != nil {
			errs = append(errs, err)
		}
		secret, err := expandSecret(ruleNum, r.Secret)
		if err != nil {
			errs = append(errs, err)
		}
		action, err := expandAction(ruleNum, r.Action)
		if err != nil {
			errs = append(errs, err)
		}
		ret.rules = append(ret.rules, compiledRule{from, secret, action})
	}

	if len(errs) > 0 {
		return nil, multierr.New(errs...)
	}
	return ret, nil
}

// Allow reports whether anyone in 'from' can perform 'action' on 'secret'.
func (p *Policy) Allow(from []string, secret string, action Action) bool {
	for _, r := range p.rules {
		if r.allow(from, secret, action) {
			return true
		}
	}
	return false
}

type compiledRule struct {
	from   map[string]bool
	secret *regexp.Regexp
	action map[Action]bool
}

func (r *compiledRule) allow(from []string, secret string, action Action) bool {
	if !r.action[action] {
		return false
	}
	if !r.secret.MatchString(secret) {
		return false
	}
	for _, f := range from {
		if r.from[f] {
			return true
		}
	}
	return false
}

// expandFrom converts from into a map for fast lookups. Elements of
// the form "group:..." are expanded to the group's members.
func expandFrom(ruleNum int, from []string, groups map[string][]string) (map[string]bool, error) {
	ret := map[string]bool{}
	var errs []error
	for _, f := range from {
		g, ok := strings.CutPrefix(f, "group:")
		if !ok {
			ret[f] = true
			continue
		}

		grp, ok := groups[g]
		if !ok {
			errs = append(errs, fmt.Errorf("rule %d references unknown group %q", ruleNum, g))
			continue
		}
		for _, gf := range grp {
			ret[gf] = true
		}
	}
	if len(errs) > 0 {
		return nil, multierr.New(errs...)
	}
	return ret, nil
}

// expandSecret converts secret into a regular expression for fast
// matching.
func expandSecret(ruleNum int, secret []string) (*regexp.Regexp, error) {
	var ret []string
	// We want the user to use glob-ish syntax, where '*' is the
	// equivalent of regexp's '.*'. We also don't want any other
	// character of the input misinterpreted as a regexp control
	// character.
	//
	// To achieve this, we:
	//  - split each input string on '*'
	//  - regexp-quote the resulting parts
	//  - reassemble the quoted parts around '.*' separators
	//  - join all the converted inputs together with '|'
	//
	// The result is a single regex that expresses "any of the forms
	// in secret", with our desired glob-ish wildcard.
	for _, s := range secret {
		parts := strings.Split(s, "*")
		for i := range parts {
			parts[i] = regexp.QuoteMeta(parts[i])
		}
		ret = append(ret, strings.Join(parts, ".*"))
	}
	reStr := fmt.Sprintf("^(?:%s)$", strings.Join(ret, "|"))
	return regexp.Compile(reStr)
}

// expandAction converts action into a map for fast lookups.
func expandAction(ruleNum int, action []Action) (map[Action]bool, error) {
	ret := map[Action]bool{}
	var errs []error
	for _, a := range action {
		switch a {
		case ActionGet, ActionList, ActionPut, ActionSetActive, ActionDelete:
			ret[a] = true
		default:
			errs = append(errs, fmt.Errorf("rule %d has unknown action %q", ruleNum, a))
		}
	}
	if len(errs) > 0 {
		return nil, multierr.New(errs...)
	}
	return ret, nil
}

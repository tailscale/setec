// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// Package api defines types used to communicate between client and
// server.
package api

// SecretVersion is the version of a secret.
//
// Secrets can have multiple values over time, for example when API
// keys get rotated. The version is a positive integer that identifies
// a specific value for the secret. Successive updates to a secret's
// value return a higher version than before, so the SecretVersion can
// be interpreted as a chronological order of the secret's values.
type SecretVersion uint32

// Secret is a secret value and its associated version.
type SecretValue struct {
	Value   []byte
	Version SecretVersion
}

// SecretInfo is information about a named secret.
//
// A secret has one or more versions. One of the versions is always
// marked "active", and is the version served to clients that request
// a secret without providing an explicit version. Inactive secret
// versions can be deleted.
type SecretInfo struct {
	Name          string
	Versions      []SecretVersion
	ActiveVersion SecretVersion
}

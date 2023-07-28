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

// SecretVersionDefault is a version that means the client wants the
// server to pick an appropriate secret version. Currently, the server
// translates this to the version marked active.
const SecretVersionDefault SecretVersion = 0

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

// ListRequest is a request to list secrets.
type ListRequest struct{}

// GetRequest is a request to get a secret value.
type GetRequest struct {
	// Name is the name of the secret to fetch.
	Name string
	// Version is the version to fetch, or SecretVersionDefault to let
	// the server pick a version.
	Version SecretVersion
}

// InfoRequest is a request for secret metadata.
type InfoRequest struct {
	// Name is the name of the secret whose metadata to return.
	Name string
}

// PutRequest is a request to write a secret value.
type PutRequest struct {
	// Name is the name of the secret to write.
	Name string
	// Value is the secret value.
	Value []byte
}

// SetActiveRequest is a request to change the active version of a
// secret.
type SetActiveRequest struct {
	// Name is the name of the secret to update.
	Name string
	// Version is the version to make active.
	Version SecretVersion
}

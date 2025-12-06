// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// Package api defines types used to communicate between client and
// server.
package api

import (
	"errors"
	"strconv"
)

var (
	// ErrValueNotChanged is a sentinel error reported by Get requests when the
	// secret value has not changed from the specified value.
	ErrValueNotChanged = errors.New("value not changed")

	// ErrNotFound is a sentinel error reported by requests when the specified
	// secret version is not found.
	ErrNotFound = errors.New("not found")

	// ErrAccessDenied is a sentinel error reported by requests when access to
	// perform the requested operation is denied.
	ErrAccessDenied = errors.New("access denied")
)

// SecretVersion is the version of a secret.
//
// Secrets can have multiple values over time, for example when API
// keys get rotated. The version is a positive integer that identifies
// a specific value for the secret. Successive updates to a secret's
// value return a higher version than before, so the SecretVersion can
// be interpreted as a chronological order of the secret's values.
type SecretVersion uint32

func (v SecretVersion) String() string {
	return strconv.FormatUint(uint64(v), 10)
}

// SecretVersionDefault is a version that means the client wants the
// server to pick an appropriate secret version. Currently, the server
// translates this to the version marked active.
const SecretVersionDefault SecretVersion = 0

// SecretValue is a secret value and its associated version.
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

	// Version is the version to fetch, or SecretVersionDefault to specify that
	// the server should return the latest active version.
	Version SecretVersion

	// UpdateIfChanged, if true, instructs the server to return the latest
	// active version of the secret if (and only if) the latest active version
	// is different from Version. If the latest active version is equal to
	// Version, the server reports 304 Not Modified and returns no value.
	//
	// If Version == SecretVersionDefault, this flag is ignored and the latest
	// active version is returned unconditionally.
	UpdateIfChanged bool
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

// CreateVersionRequest is a request to create a specific version of a secret
// with a given value.
type CreateVersionRequest struct {
	// Name is the name of the secret to write.
	Name string
	// Version is the version to create and make active.
	Version SecretVersion
	// Value is the secret value.
	Value []byte
}

// ActivateRequest is a request to change the active version of a secret.
type ActivateRequest struct {
	// Name is the name of the secret to update.
	Name string
	// Version is the version to make active.
	Version SecretVersion
}

// DeleteRequest is a request to delete all versions of a secret.
type DeleteRequest struct {
	// Name is the name of the secret to delete.
	Name string
}

// DeleteVersionRequest is a request to delete a single version of a secret.
type DeleteVersionRequest struct {
	// Name is the name of the secret to delete a version from.
	Name string

	// Version is the version to delete; 0 is invalid for this request as the
	// active version cannot be deleted.
	Version SecretVersion
}

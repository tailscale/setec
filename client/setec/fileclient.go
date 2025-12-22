// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package setec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/tailscale/setec/types/api"
)

// FileClient is an implementation of the StoreClient interface that vends
// secrets from a static collection of data stored locally on disk.
//
// This is intended for use in bootstrapping and deployments without access to
// a separate secrets server.
type FileClient struct {
	path string                      // local filesystem path, for diagnostics
	db   map[string]*api.SecretValue // :: secret name → static version
}

// NewFileClient constructs a new FileClient using the contents of the
// specified local file path. The file must contain a JSON object having the
// following structure:
//
//	{
//	   "secret-name-1": {
//	      "secret": {"Value": "b3BlbiBzZXNhbWU=", "Version": 1}
//	   },
//	   "secret-name-2": {
//	      "secret": {"TextValue": "xyzzy", "Version": 5}
//	   },
//	   ...
//	}
//
// The secret values are encoded either as base64 strings ("Value") or as plain
// text ("TextValue"). A cache file written out by a FileCache can also be used
// as input. Unlike a cache, however, a FileClient only reads the file once,
// and subsequent modifications of the file are not observed.
func NewFileClient(path string) (*FileClient, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var input map[string]struct {
		Secret *struct {
			Value     []byte
			TextValue string
			Version   api.SecretVersion
		} `json:"secret"`
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, fmt.Errorf("decode secrets file: %w", err)
	}
	db := make(map[string]*api.SecretValue)
	for name, val := range input {
		if name == "" || val.Secret == nil {
			continue // no secret value; skip
		}
		sec := val.Secret
		if sec.Version <= 0 || (sec.TextValue == "" && len(sec.Value) == 0) {
			continue // invalid version or no value
		}
		if sec.TextValue != "" {
			db[name] = &api.SecretValue{Value: []byte(sec.TextValue), Version: sec.Version}
		} else {
			db[name] = &api.SecretValue{Value: sec.Value, Version: sec.Version}
		}
	}
	return &FileClient{path: path, db: db}, nil
}

// Get implements the corresponding method of StoreClient.
func (fc *FileClient) Get(_ context.Context, name string) (*api.SecretValue, error) {
	if s, ok := fc.db[name]; ok {
		return s, nil
	}
	return nil, api.ErrNotFound
}

// GetIfChanged implements the corresponding method of StoreClient.
func (fc *FileClient) GetIfChanged(_ context.Context, name string, oldVersion api.SecretVersion) (*api.SecretValue, error) {
	s, ok := fc.db[name]
	if !ok {
		return nil, api.ErrNotFound
	} else if s.Version == oldVersion {
		return nil, api.ErrValueNotChanged
	}
	return s, nil
}

// StoreClient is the interface to the setec API used by the Store.
type StoreClient interface {
	// Get fetches the current active secret value for name. See [Client.Get].
	Get(ctx context.Context, name string) (*api.SecretValue, error)

	// GetIfChanged fetches a secret value by name, if the active version on the
	// server is different from oldVersion. See [Client.GetIfChanged].
	GetIfChanged(ctx context.Context, name string, oldVersion api.SecretVersion) (*api.SecretValue, error)
}

// VersioningStoreClient is an extension of [StoreClient] that supports operations on versions of secrets.
type VersioningStoreClient interface {
	StoreClient

	// Info fetches metadata for a given secret name.
	Info(ctx context.Context, name string) (*api.SecretInfo, error)

	// GetVersion fetches a secret value by name and version. If version == 0,
	// GetVersion retrieves the current active version.
	GetVersion(ctx context.Context, name string, version api.SecretVersion) (*api.SecretValue, error)

	// CreateVersion Creates a specific version of a secret, sets its value and immediately activates that version.
	// It fails if this version of the secret ever had a value.
	CreateVersion(ctx context.Context, name string, version api.SecretVersion, value []byte) error
}

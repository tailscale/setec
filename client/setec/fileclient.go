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
//	      "secret": {"Value": "eHl6enk=", "Version": 5}
//	   },
//	   ...
//	}
//
// The secret values are encoded as base64 strings. A cache file written out by
// a FileCache can also be used as input. Unlike a cache, however, a FileClient
// only reads the file once, and subsequent modifications of the file are not
// observed.
func NewFileClient(path string) (*FileClient, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var input map[string]struct {
		Secret *api.SecretValue `json:"secret"`
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, fmt.Errorf("decode secrets file: %w", err)
	}
	db := make(map[string]*api.SecretValue)
	for name, val := range input {
		if name == "" || val.Secret == nil {
			continue // no secret value; skip
		}
		db[name] = val.Secret
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

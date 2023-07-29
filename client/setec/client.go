// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// Package setec is a client library to access and manage secrets
// stored remotely in a secret management service.
package setec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/tailscale/setec/types/api"
)

// Store is a store that provides named secrets.
type Store struct {
}

// Cache is an interface that lets a Store persist one piece of data
// locally. The persistence need not be perfect (i.e. it's okay to
// lose previously written data).
type Cache interface {
	// Write persists the given bytes for future retrieval.
	Write([]byte) error
	// Read returns previously persisted bytes, if any are available.
	Read() ([]byte, error)
}

// StoreConfig is the configuration for Store.
type StoreConfig struct {
	// Client is the client to use to fetch secrets.
	Client Client
	// Cache, if non-nil, is a cache that can save secrets locally.
	//
	// Depending on the implementation, local caching may degrade
	// security slightly by making secrets easier to get at, but in
	// return allows the Store to initialize and run during outages of
	// the secrets management service.
	//
	// If no cache is provided, the Store uses an ephemeral in-memory
	// cache that lasts the lifetime of the process only.
	Cache Cache
	// Secrets are the names of the secrets this Store should
	// retrieve. Only secrets named here can be read out of the store.
	Secrets []string
}

// NewStore creates a secret store with the given configuration.
// NewStore blocks until the secrets named in cfg.Secrets are
// available for retrieval through Secret().
func NewStore(ctx context.Context, cfg *StoreConfig) (*Store, error) {
	return nil, errors.New("not implemented")
}

// Secret returns the value of the named secret.
func (s *Store) Secret(name string) []byte {
	return nil
}

// Client is a raw client to the secret management server.
// If you're just consuming secrets, you probably want to use a Store
// instead.
type Client struct {
	// Server is the URL of the secrets server to talk to.
	Server string
	// DoHTTP is the function to use to make HTTP requests. If nil,
	// http.DefaultClient.Do is used.
	DoHTTP func(*http.Request) (*http.Response, error)
}

func do[RESP, REQ any](ctx context.Context, c *Client, path string, req REQ) (RESP, error) {
	var resp RESP

	bs, err := json.Marshal(req)
	if err != nil {
		return resp, fmt.Errorf("marshaling request: %w", err)
	}

	url := fmt.Sprintf("%s/%s", strings.TrimSuffix(c.Server, "/"), strings.TrimPrefix(path, "/"))

	r, err := http.NewRequestWithContext(ctx, "GET", url, bytes.NewReader(bs))
	if err != nil {
		return resp, fmt.Errorf("constructing HTTP request: %w", err)
	}
	r.Header.Set("Content-Type", "application/json")

	do := c.DoHTTP
	if do == nil {
		do = http.DefaultClient.Do
	}
	httpResp, err := do(r)
	if err != nil {
		return resp, fmt.Errorf("making HTTP request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		errBs, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return resp, fmt.Errorf("reading error response body (HTTP status %d): %w", httpResp.StatusCode, err)
		}
		return resp, fmt.Errorf("request returned status %d: %q", httpResp.StatusCode, string(bytes.TrimSpace(errBs)))
	}

	bs, err = io.ReadAll(httpResp.Body)
	if err != nil {
		return resp, err
	}

	if err := json.Unmarshal(bs, &resp); err != nil {
		return resp, fmt.Errorf("unmarshaling response: %w", err)
	}

	return resp, nil
}

// List fetches a list of secret names and associated metadata (but
// not the secret values themselves).
func (c *Client) List(ctx context.Context) ([]*api.SecretInfo, error) {
	return do[[]*api.SecretInfo](ctx, c, "/api/list", api.ListRequest{})
}

// Get fetches a secret value by name.
func (c *Client) Get(ctx context.Context, name string) (*api.SecretValue, error) {
	return do[*api.SecretValue](ctx, c, "/api/get", api.GetRequest{
		Name:    name,
		Version: api.SecretVersionDefault,
	})
}

// Get fetches a secret value by name and version.
func (c *Client) GetVersion(ctx context.Context, name string, version api.SecretVersion) (*api.SecretValue, error) {
	return do[*api.SecretValue](ctx, c, "/api/get", api.GetRequest{
		Name:    name,
		Version: version,
	})
}

// Info fetches metadata for a given secret name.
func (c *Client) Info(ctx context.Context, name string) (*api.SecretInfo, error) {
	return do[*api.SecretInfo](ctx, c, "/api/info", api.InfoRequest{
		Name: name,
	})
}

// Put creates a secret called name, with the given value. If a secret
// called name already exist, the value is saved as a new inactive
// version.
func (c *Client) Put(ctx context.Context, name string, value []byte) (version api.SecretVersion, err error) {
	return do[api.SecretVersion](ctx, c, "/api/put", api.PutRequest{
		Name:  name,
		Value: value,
	})
}

// SetActiveVersion changes the active version of the secret called
// name to version.
func (c *Client) SetActiveVersion(ctx context.Context, name string, version api.SecretVersion) error {
	_, err := do[struct{}](ctx, c, "/api/set-active", api.SetActiveRequest{
		Name:    name,
		Version: version,
	})
	return err
}

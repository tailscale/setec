// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// Package setec is a client library to access and manage secrets stored
// remotely in a secret management service.
package setec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/tailscale/setec/types/api"
)

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

	r, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bs))
	if err != nil {
		return resp, fmt.Errorf("constructing HTTP request: %w", err)
	}
	r.Header.Set("Content-Type", "application/json")
	// See the comment in server/server.go for what this does.
	r.Header.Set("Sec-X-Tailscale-No-Browsers", "setec")

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

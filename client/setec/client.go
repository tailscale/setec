// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// Package setec is a client library to access and manage secrets stored
// remotely in a secret management service.
//
// # Basic Usage
//
// Callers that consume secrets for production use should use a [Store] to read
// initial values for all desired secrets at startup, and cache them in memory
// while running.  This ensures that once the program is started, it will
// always have a valid value for each of its secrets, even if the setec server
// is temporarily unavailable.
//
// To initialize a store, call [NewStore], providing a [Client] for the API and
// the names of the desired secrets:
//
//	st, err := setec.NewStore(ctx, setec.StoreConfig{
//	   Client:  setec.Client{Server: "https://secrets.example.com"},
//	   Secrets: []string{"account-name", "access-key"},
//	})
//	if err != nil {
//	   log.Fatalf("Initializing secrets: %v", err)
//	}
//
// This will block until initial values are available for each declared secret,
// or ctx ends. Set a timeout or deadline on the provided context if you want
// to fail program startup after some reasonable grace period. Once [NewStore]
// has returned successfully, a value for each declared secret can be accessed
// immediately without blocking at any time:
//
//	accessKey := st.Secret("access-key").GetString()
//
// The Store periodically polls for new secret values in the background, so the
// caller does not need to worry about whether the server is available at the
// time when it needs a secret.
//
// Where possible, it is best to declare all desired secrets in the config, and
// by default only declared secrets can be accessed.  If a caller does not know
// the names of all the secrets in advance, you may use the AllowLookup option
// to permit undeclared secrets to be fetched later.
//
// See also [Bootstrapping and Availability].
//
// # Other Operations
//
// Programs that need to create, update, or delete secrets and secret versions
// may use a [Client] to directly call the full [setec HTTP API]. In this case,
// the caller is responsible for handling retries in case the secrets service
// is temporarily unavailable.
//
// [Bootstrapping and Availability]: https://github.com/tailscale/setec?tab=readme-ov-file#bootstrapping-and-availability
// [setec HTTP API]: https://github.com/tailscale/setec/blob/main/docs/api.md
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

func do[RESP, REQ any](ctx context.Context, c Client, path string, req REQ) (RESP, error) {
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

	if code := httpResp.StatusCode; code != http.StatusOK {
		errBs, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return resp, fmt.Errorf("reading error response body (HTTP status %d): %w", code, err)
		}
		switch code {
		case http.StatusNotFound:
			return resp, api.ErrNotFound
		case http.StatusForbidden:
			return resp, api.ErrAccessDenied
		case http.StatusNotModified:
			return resp, api.ErrValueNotChanged
		}
		return resp, fmt.Errorf("request returned status %d: %q", code, string(bytes.TrimSpace(errBs)))
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

// List fetches a list of secret names and associated metadata for all those
// secrets on which the caller has "info" access. List does not report the
// secret values themselves. If the caller does not have "info" access to any
// secrets, List reports zero values without error.
func (c Client) List(ctx context.Context) ([]*api.SecretInfo, error) {
	return do[[]*api.SecretInfo](ctx, c, "/api/list", api.ListRequest{})
}

// Get fetches the current active secret value for name.
//
// Access requirement: "get"
func (c Client) Get(ctx context.Context, name string) (*api.SecretValue, error) {
	return do[*api.SecretValue](ctx, c, "/api/get", api.GetRequest{
		Name:    name,
		Version: api.SecretVersionDefault,
	})
}

// GetIfChanged fetches a secret value by name, if the active version on the
// server is different from oldVersion. If the active version on the server is
// the same as oldVersion, it reports api.ErrValueNotChanged without returning
// a secret. As a special case, if oldVersion == 0 then GetIfVersion behaves as
// Get and retrieves the current active version.
//
// Access requirement: "get"
func (c Client) GetIfChanged(ctx context.Context, name string, oldVersion api.SecretVersion) (*api.SecretValue, error) {
	if oldVersion == api.SecretVersionDefault {
		return c.Get(ctx, name)
	}
	return do[*api.SecretValue](ctx, c, "/api/get", api.GetRequest{
		Name:            name,
		Version:         oldVersion,
		UpdateIfChanged: true,
	})
}

// GetVersion fetches a secret value by name and version. If version == 0,
// GetVersion retrieves the current active version.
//
// Access requirement: "get"
func (c Client) GetVersion(ctx context.Context, name string, version api.SecretVersion) (*api.SecretValue, error) {
	return do[*api.SecretValue](ctx, c, "/api/get", api.GetRequest{
		Name:    name,
		Version: version,
	})
}

// Info fetches metadata for a given secret name.
//
// Access requirement: "info"
func (c Client) Info(ctx context.Context, name string) (*api.SecretInfo, error) {
	return do[*api.SecretInfo](ctx, c, "/api/info", api.InfoRequest{
		Name: name,
	})
}

// Put creates a secret called name, with the given value. If a secret called
// name already exist, the value is saved as a new inactive version.
//
// Access requirement: "put"
func (c Client) Put(ctx context.Context, name string, value []byte) (version api.SecretVersion, err error) {
	return do[api.SecretVersion](ctx, c, "/api/put", api.PutRequest{
		Name:  name,
		Value: value,
	})
}

// Activate changes the active version of the secret called name to version.
//
// Access requirement: "activate"
func (c Client) Activate(ctx context.Context, name string, version api.SecretVersion) error {
	_, err := do[struct{}](ctx, c, "/api/activate", api.ActivateRequest{
		Name:    name,
		Version: version,
	})
	return err
}

// DeleteVersion deletes the specified version of the named secret.
//
// Note: DeleteVersion will report an error if the caller attempts to delete
// the active version, even if they have permission to do so.
//
// Access requirement: "delete"
func (c Client) DeleteVersion(ctx context.Context, name string, version api.SecretVersion) error {
	_, err := do[struct{}](ctx, c, "/api/delete-version", api.DeleteVersionRequest{
		Name:    name,
		Version: version,
	})
	return err
}

// Delete deletes all versions of the named secret.
//
// Note: Delete will delete all versions of the secret, including the active
// one, if the caller has permission to do so.
//
// Access requirement: "delete"
func (c Client) Delete(ctx context.Context, name string) error {
	_, err := do[struct{}](ctx, c, "/api/delete", api.DeleteRequest{
		Name: name,
	})
	return err
}

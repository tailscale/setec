# setec

[![GoDoc](https://img.shields.io/static/v1?label=godoc&message=reference&color=silver)](https://pkg.go.dev/github.com/tailscale/setec)
[![CI](https://github.com/tailscale/setec/actions/workflows/go-presubmit.yml/badge.svg?event=push&branch=main)](https://github.com/tailscale/setec/actions/workflows/go-presubmit.yml)

Setec is a lightweight secrets management service that uses Tailscale for access control.

> [!IMPORTANT]
> File issues in our [primary open source repository](https://github.com/tailscale/tailscale/issues).

## Table of Contents

- [Background](#background)
- [Get started](#get-started)
- [API Overview](#api-overview)
   - [Basic Operations](#basic-operations)
   - [Current Active Versions](#current-active-versions)
   - [Deleting Values](#deleting-values)
- [Basic Usage](#basic-usage)
- [Migrating to Setec](#migrating-to-setec)
- [Operations and Maintenance](#operations-and-maintenance)
   - [Secret Rotation](#secret-rotation)
   - [Automatic Updates](#automatic-updates)
   - [Bootstrapping and Availability](#bootstrapping-and-availability)
- [Testing](#testing)

### Additional documentation

- [API documentation](api.md)
- [Running a setec server](server.md)

## Background

Programs in production often need access to passwords, API keys, parameterized
connection URLs, and other sensitive information (hereafter, **secrets**) at
runtime.  Securely deploying secrets in production complicates operations, and
typically involves a combination of tedious manual intervention (e.g., copying
secrets out of secure storage at startup), or integrating with complex secrets
management infrastructure (usually specific to the deployment environment).

Setec comprises a lightweight HTTP-based [API](api.md) and a corresponding
server, that allows programs running on a tailnet to securely fetch secrets at
runtime. Access to secrets is governed by the tailnet's policy document, and
the server maintains secrets in encrypted storage, keeps an audit log of
accesses, and manages periodic backups.

The setec server integrates with existing key-management infrastructure to
bootstrap its own deployment (as of 24-Sep-2023, AWS KMS is supported).
Once the server is running on a tailnet, other programs can use it to access
their production secrets with a basic WireGuard-encrypted HTTP request, rather
than having to distribute secrets via files, environment variables, or manual
operator intervention.

In addition to reducing deployment toil, this also helps reduce the attack
surface for managing secrets in third-party deployment environments: All the
secrets are stored in one place, with access controls and audit logs to allow
forensics in the event of a compromise.

## Get started

To set up a setec server, follow the instructions in [Running a setec server](server.md).

## API Overview

See also the [full API documentation](api.md).

A **secret** in `setec` is a named collection of values. Each value is an
arbitrary byte string identified by an integer **version number**.

### Basic Operations

The [setec API](api.md) defines the following basic operations:

- The `get` method retrieves the value of a single version of a secret.
  Clients use this method to obtain the secrets they need in production.

- The `info` and `list` methods report the names and available versions (but
  not the values) of secrets accessible to the caller.

- The `put` method creates or adds a new value to a secret. The server assigns
  and reports a version number for the value.

- The `create-version` method creates a specific version of a secret, sets its
  value and immediately activates that version. It fails if a value has ever
  been set for that version.

`put` and `create-version` are safe to use in conjunction with each other on the same
secret.

### Current Active Versions

- At any time, one version of the secret is designated as its **current active
  version**. The active version is reported by default from the `get` method if
  the caller does not specify a specific version.

- When a secret is first created, its initial value (version 1) becomes its
  current active version.

- Thereafter, the `activate` method must be used to update the current active
  version. This ensures the operator of the service has precise control over
  which version of a secret should be used at a time.
   - The special `create-version` method automatically activates the set
     version and does not require a call to `activate`.

### Deleting Values

- The `delete-version` method deletes a single version of a secret.  This
  method will not delete the current active version.

- The `delete` method deletes all the versions of a secret, removing it
  entirely from the service.


## Basic usage

All of the methods of the setec API are HTTPS POST requests. Calls to the API
must include a `Sec-X-Tailscale-No-Browsers: setec` header.  This prevents
browser scripts from initiating calls to the service. The examples below assume
you have a setec server running at `secrets.example.ts.net` on your tailnet.

Go programs can use the [setec client library][setecclient] provided in this
repository. The API supports any language, however, so the examples below are
written using the curl command-line tool.

A program that wishes to fetch a secret at runtime does so by issuing an HTTPS
POST request to the `/api/get` method of the secrets service.

For example:

```shell
curl -H sec-x-tailscale-no-browsers:setec -H content-type:application/json -X POST \
  https://secrets.example.ts.net/api/get -d '{"Name": "prod/myprogram/secret-name"}'
```

The `"Name"` field specifies which secret to fetch. The name must be non-empty,
but is otherwise not interpreted by the service, and you should choose names
that make sense for your environment. Here we're using a basic path layout,
grouping secrets by deployment environment (`dev` vs. `prod`) and program.

If the caller has access to the secret named `prod/myprogram/secret-name`, the
server will return the **current active version** of this secret, e.g.,

```js
{"Value":"aGVsbG8sIHdvcmxk","Version":1}
```

The secret `Value` is base64-encoded (in this case, the string "hello, world")
and the `Version` is an integer indicating the sequential version number of
this secret value. New versions of a secret are added by calling `/api/put` and
any existing version can be set as the "active" version of the secret.

This repository also defines a [`setec` command-line tool][seteccli], written
in Go, that can be used to call the API from scripts. To install it, run:

```bash
go install github.com/tailscale/setec/cmd/setec@latest
```

If for some reason you do not want to use the CLI directly, the following shell
function is roughly equivalent for the purposes of these examples:

```bash
# Usage: setec_get <secret-name>
# This assumes setec resides at "secrets.example.ts.net".
# Roughly equivalent to "setec get <secret-name>" using the CLI.
setec_get() {
  local name="$1"
  curl -s https://secrets.example.ts.net/api/get -X POST --fail \
    -H sec-x-tailscale-no-browsers:setec -H content-type:application/json \
    -d '{"name":"'"$name"'"}' \
  | jq -r '.Value|@base64d'
}
```


## Migrating to Setec

When migrating existing programs to use setec, there are two main patterns of
use you are likely to encounter: Environment variables, and key files.

The examples below are written as bash scripts, and make use of the [`setec`
command-line tool][seteccli].

In practice most interesting programs will be written in a more structured
language, the goal of using bash here is to illustrate the plumbing in a
generic way.

### Migrating Environment Variables

Environment variables are often used to plumb secrets to programs running in
containers. Typically the host allows you to add secrets to a store they
manage, and to associate them with environment variables that they set up when
starting up a container on your behalf.  From the perspective of your program,
the environment variable is ambient.

For example, the following script expects `EXAMPLE_API_KEY` to be defined.

```bash
#!/usr/bin/env bash
set -euo pipefail

call_api() {
   curl -s https://api.example.com/v1/method --url-query "q=$@" \
      -H "Authorization: Bearer ${EXAMPLE_API_KEY}"
}

# ...
```

To replace this usage, load `EXAMPLE_API_KEY` by calling setec:

```bash
#!/usr/bin/env bash
set -euo pipefail

# Here we assume the secret name is "prod/script/example-api-key".
EXAMPLE_API_KEY="$(setec -s https://secrets.example.ts.net get prod/script/example-api-key)"

call_api() {
   curl -s https://api.example.com/v1/method --url-query "q=$@" \
      -H "Authorization: Bearer ${EXAMPLE_API_KEY}"
}

# ...
```

### Migrating Key Files

Key files are sometimes used to plumb secrets to programs running in VMs or on
colocated physical machines. Typically key files will be stored in a central
secret manager and deployed to the VM or hardware using tools like Ansible or
Chef. From the perspective of your program, the secret is a plain file at a
known path on the local filesystem.

For example, the following script expects `/secrets/example-api-key` to contain
the API key for a service:

```bash
#!/usr/bin/env bash
set -euo pipefail

readonly EXAMPLE_API_KEY="$(cat /secrets/example-api-key)"

call_api() {
   curl -s https://api.example.com/v1/method --url-query "q=$@" \
      -H "Authorization: Bearer ${EXAMPLE_API_KEY}"
}

# ...
```

To replace this usage, load `EXAMPLE_API_KEY` by calling setec:

```bash
#!/usr/bin/env bash
set -euo pipefail

# Here we assume the secret name is "prod/script/example-api-key".
readonly EXAMPLE_API_KEY="$(setec -s https://secrets.example.ts.net prod/script/example-api-key)"

call_api() {
   curl -s https://api.example.com/v1/method --url-query "q=$@" \
      -H "Authorization: Bearer ${EXAMPLE_API_KEY}"
}

# ...
```


## Operations and Maintenance

This section discusses some common service operation and maintenance tasks that
interact with secrets management, and how they interact with setec.

### Secret Rotation

Sometimes secrets need to be rotated. For example, if an API credential expires
or is compromised, the programs that depend on that credential will need to be
updated to use a new value.

The simplest way to rotate secrets managed by setec is for the operator to
install a new version of the secret, mark that version as active, and restart
or redeploy the programs that use that secret so they will pick up the latest
version. For example, using the `setec` command-line tool, suppose we have:

```bash
# HTTP: POST /api/info
setec info dev/hello-world
```

This will print the name of the secret, along with the version numbers that
exist and which one is active:

```
Name:           dev/hello-world
Active version: 7
Versions:       1, 2, 3, 4, 5, 6, 7
```

In this example we see that the `dev/hello-world` secret has 7 versions and
version 7 is currently active. To add a new version, use `setec put`:

```bash
# HTTP: POST /api/put
setec put dev/hello-world
```

This will prompt you for the new secret version:

```
Enter secret: ****
Confirm secret: ****
Secret saved as "dev/hello-world", version 8
```

At this point, version 8 exists, but version 7 is still active. To activate
version 8, write:

```bash
# HTTP: POST /api/activate
setec activate dev/hello-world 8
```

Now, any client that fetches the active version of `dev/hello-world` will get
this new value instead.

### Automatic Updates

The example above shows how to simply rotate a secret, but that still requires
all the programs which depend on that secret to be restarted or redeployed to
pick up the new value. That is fine for batch processing or low-volume services
that do not receive a lot of traffic, but may be disruptive for servers with a
larger active query load.

A program that does not wish to restart to pick up new secret values can poll
the secrets API periodically, to see whether a new version is available. To do
this, the request to the `/api/get` method may include the current version the
client is using, and set the `"UpdateIfChanged"` flag to `true`:

```json
{"Name":"dev/hello-world", "Version":7, "UpdateIfChanged":true}
```

If the current active version of the secret still matches what the client
requested, the server will report 304 Not Modified, indicating to the client
that it still (already) has the active version. This allows the client to check
for an update without sending secret values over the wire except when needed,
and does not trigger an audit-log record unless the server reveals the value of
a secret to the client, or the permissions have changed (causing the client to
be denied access).

The Go client library provides a [`setec.Store`][setecstore] type that handles
automatic updates using this polling mechanism, but the same logic can be
implemented in any language. In Go, this looks like:

```go
import "github.com/tailscale/setec/client/setec"

func main() {
   // Construct a setec.Store that tracks a set of secrets this program needs.
   // This will block until all the requested secrets are available.
   // Thereafter, in the background, it will poll for new versions at
   // approximately the given interval.
   st, err := setec.NewStore(context.Background(), setec.StoreConfig{
      Client: setec.Client{Server: "https://secrets.example.ts.net"},
      Secrets: []string{
         "prod/myprogram/secret-1",
         "prod/myprogram/secret-2",
      },
      PollInterval: 24*time.Hour,
   })
   if err != nil {
      log.Fatalf("NewStore: %v", err)
   }

   // Fetch a secret from the store. A setec.Secret is a handle that always
   // delivers the current value, automatically updated as new versions become
   // available from the server.
   apiKey := st.Secret("prod/myprogram/secret-1")

   cli := someservice.NewClient("username", apiKey.Get())
   // ...
}
```

Alternatively, you can obtain a [`setec.Updater`][setecupdater], which uses a
user-provided callback to update a local value whenever a new version of a
secret becomes available. An updater is safe for concurrent use by multiple
goroutines.

For example:

```go
// Construct an updater, given a callback that takes a secret value and returns
// a new someservice client using that secret.
client, err := setec.NewUpdater(ctx, store, "secret/name", func(secret []byte) (*svc.Client, error) {
   return svc.NewClient("username", secret)
})
if err != nil {
   return fmt.Errorf("initialize client: %w", err)
}

// Whenever you need a client, call the Get method:
rsp, err := client.Get().Method(ctx, args)
// ...
```

The updater constructs the initial client by invoking the callback with the
current secret value when `NewUpdater` is called. Thereafter, calls to
`u.Get()` will return the same client until the secret changes. When that
happens, `Get` invokes the callback again with the new secret value, to make a
fresh client.  If an error occurs while updating the client, the updater keeps
returning the previous value.

#### Explicit Refresh

Ordinarily a `Store` will automatically update secret values in the background.
If a program needs to explicitly refresh the values of secrets at a specific
time (for example, in response to an operator signal or other event) it may
explicitly call the `Store` value's [`Refresh`][strefresh] method, which
effects a poll of all known secrets synchronously. It is safe for the client to
do this concurrently with a background poll; the store will coalesce the
operations.

### Bootstrapping and Availability

A reasonable concern when fetching secrets from a network service is what
happens if the secrets service is not reachable when a program needs to fetch a
secret. A good answer depends on the nature of the program: Batch processing
tools can usually afford to wait and retry until the service becomes
available. Interactive services, by contrast, may not be able to tolerate
waiting.

To minimize the impact of a secrets server being temporarily unreachable, a
program should fetch all desired secrets at startup and cache them (typically
in memory) while running. If the secrets service is unreachable when the
program first starts, it should wait and retry as necessary, or fail the
startup process. Once the program has initial values for all its desired
secrets, it can poll for new values in the background.

This ensures that even if the secrets server is occasionally unreachable, the
program always has a good value for each secret, even if one that is
(temporarily) slightly stale.  The Go client library's [`setec.Store`][setecstore]
type implements this logic automatically (see the example above).

A program that needs be able to start immediately, even when the secrets server
is unavailable, can trade a bit of security for availability by caching the
active versions of the secrets it needs in persistent storage (e.g., a local
file). When the program start or restarts, it can fall back to the cached
values if the secrets service is not immediately available. The Go client
library's [`setec.Store`][setecstore] type supports this kind of caching as an
optional feature, and the same logic can be implemented in any language.

In Go, you can enable a file cache using `setec.NewFileCache`:

```go
// Create or open a cache associated with the specified file path.
fc, err := setec.NewFileCache("/data/secrets.cache")
if err != nil {
    return fmt.Errorf("creating cache: %w", err)
}
st, err := setec.NewStore(ctx, setec.StoreConfig{
    Client:       setec.Client{Server: "https://secrets.example.ts.net"},
    Secrets:      []string{"secret1", "secret2", "secret3"},
    PollInterval: 12 * time.Hour,
    Cache:        fc,
})
// ...
```

With the cache enabled, the store will automatically persist new secrets
fetched from the server into the cache as they become available. Morever, when
the store is created, the store will not block waiting for the server if all
the requested secrets already have a version stored in the cache.

**Enabling a file cache represents a security tradeoff:** The cache records all
the program's secret values to local storage, which means they can be read by
(other) programs and users with access to that storage. In return, however, the
program can start up immediately using cached data, even if the secrets server
is not reachable when it launches.

> [!WARNING]
> When you enable a secrets cache for a program, new secret values may not
> immediately become available even if the program is restarted. By design, if
> a cached value is available at startup, the store does not wait for the
> secrets service to respond before delivering the initial (cached) value.
>
> The store will see the new value (and update the cache) the next time it
> successfully polls.  If the program only looks at the initial value of the
> secret, however, it will not see the new value until it is restarted _after_
> the next update.
>
> As a general rule, we recommend you _not_ enable a cache unless the program
> cannot tolerate even a temporary outage of the secrets service or your
> tailnet at program start (for example, if it is part of your infrastructure
> bootstrap).  If you _must_ use a cache, we advise you structure your program
> to automatically handle new secret values, and not to "lock in" the initial
> value of a secret when the program starts up. You may also wish to decrease
> the polling interval from the default.

## Self-Contained Operation

In some cases, you may need to run a program entirely without access to a
secrets server. For example, in standalone testing and bootstrapping it may be
impractical to set up a secrets service, or you may want to deploy the same
program across different environments where a secrets service may or may not be
present.

To support these cases, the Go [`setec`][setecpkg] package provides a
[`FileClient`][fileclient] type that can be plugged into a
[`setec.Store`][setecstore].  Unlike the normal [`setec.Client`][setecclient],
a `FileClient` does not use the network at all, but vends secrets read from a
plaintext JSON file on the local filesystem.

To use this, construct a `Store` using a `FileClient` instead:

```go
fc, err := setec.NewFileClient("/data/secrets.json")
if err != nil {
   return fmt.Errorf("creating file client: %w", err)
}
st, err := setec.NewStore(ctx, setec.StoreConfig{
   Client:  fc,
   Secrets: []string{"svc/secret1", "svc/secret2"},
})
```

As input, the `FileClient` expects a file containing a JSON message like:

```json
{
   "svc/secret1": {
      "secret": {"Version": 1, "Value": "dGhlIGtub3dsZWRnZSBpcyBmb3JiaWRkZW4="}
   },
   "svc/secret2": {
      "secret": {"Version": 5, "TextValue": "eat your vegetables"}
   }
}
```

The object keys are the secret names, and the values have the structure shown.
Binary secret values are base64 encoded as `"Value"`, or if you are constructing
a secrets file by hand you may include plain text secrets as `"TextValue"`
instead.

A program that may be used in multiple environments can choose which client to
use at startup, and otherwise the store will work the same:

```go
var client setec.StoreClient
if *secretsAddr != "" {
   client = setec.Client{Server: *secretsAddr}
} else if fc, err := setec.NewFileClient(*localSecrets); err != nil {
   log.Fatalf("Open file client: %v", err)
} else {
   client = fc
}

st, err := setec.NewStore(ctx, setec.StoreConfig{
   Client: client,
   // ... other options as usual
})
```

### FileClient and Caching

Although the two are related, a `FileClient` differs from the cache mechanism
described in the previous section.  With a cache enabled, a store loads secrets
from the cache file at startup, but otherwise communicates with a secrets
service in the usual way.

With a `FileClient`, however, the store does not access the network at all: It
reads the specified file once at startup, and only serves those exact secret
values.

The two mechanisms are intended to be complementary. For example, you could
bootstrap a new deployment using the following steps:

- Create a secrets file seeded with the initial secrets your program needs.

- Start up a store with a `FileClient` that uses your seed file. This lets you
  get your program working even if your secrets server is not yet set up.

- When you are ready to switch to a secrets server, you can enable a cache, and
  the store will prime the cache with the secrets from your seed file.

- Then, when you switch from a `FileClient` to a regular `Client`, you will
  have an already-primed cache available (which can be helpful as you are
  working out the inevitable quirks of a new service configuration).

The opposite is also true: By design, the format of the cache files can also be
used directly as the input to a `FileClient` if you need to spin up a new
instance of an existing server somewhere else.


## Unit Testing

For programs written in Go, the [`setectest`][setectest] package provides
in-memory implementations of the setec server and its database for use in
writing tests. The servers created by this package use the same implementation
as the production server (`setec server` in the CLI), but use a stub
implementation of encryption (so they do not require an external KMS).

A [`setectest.Server`][stserver] can be used with the [`httptest`][httptest]
package to hook up a real, working setec client and/or store in tests:

```go
// Create a new database and add some secrets to it.
db := setectest.NewDB(t, nil)
db.MustPut(db.Superuser, "alpha", "ok")
db.MustPut(db.Superuser, "bravo", "yes")
db.MustPut(db.Superuser, "bravo", "no")

// Create a setec server using that database.
ts := setectest.NewServer(t, db, nil)

// Stand up an in-memory HTTP server exporting ts.
hs := httptest.NewServer(ts.Mux)
defer hs.Close()

// Start a setec.Store talking to this server.
st, err := setec.NewStore(context.Background(), setec.StoreConfig{
    // Note the client here uses the httptest URL and client.
    Client:  setec.Client{Server: hs.URL, DoHTTP: hs.Client().Do},
    Secrets: []string{"alpha", "bravo"},
})
if err != nil {
    t.Fatalf("NewStore: %v", err)
}
// ... the rest of the test
```


<!-- references -->
[httptest]: https://godoc.org/net/http/httptest
[seteccli]: https://github.com/tailscale/setec/tree/main/cmd/setec
[setecpkg]: https://godoc.org/github.com/tailscale/setec/client/setec
[setecclient]: https://godoc.org/github.com/tailscale/setec/client/setec#Client
[fileclient]: https://godoc.org/github.com/tailscale/setec/client/setec#FileClient
[setecstore]: https://godoc.org/github.com/tailscale/setec/client/setec#Store
[setectest]: https://godoc.org/github.com/tailscale/setec/setectest
[setecupdater]: https://godoc.org/github.com/tailscale/setec/client/setec#Updater
[stserver]: https://godoc.org/github.com/tailscale/setec/setectest#Server
[strefresh]: https://godoc.org/github.com/tailscale/setec/client/setec#Store.Refresh

# Documentation for Setec

## Table of Contents

- [Background](#background)
- [Basic usage](#basic-usage)
- [Migrating to Setec](#migrating-to-setec)
- [Operations and Maintenance](#operations-and-maintenance)
   - [Secret Rotation](#secret-rotation)
   - [Automatic Updates](#automatic-updates)
   - [Bootstrapping and Availability](#bootstrapping-and-availability)
- [Testing](#testing)
- [API documentation](api.md)

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

## Basic usage

All of the methods of the setec API are HTTPS POST requests. Calls to the API
must include a `Sec-X-Tailscale-No-Browsers: setec` header.  This prevents
browser scripts from initiating calls to the service. The examples below assume
you have a setec server running at `secrets.example.ts.net` on your tailnet.

Go programs can use the [setec client library][setecclient] provided in this
repository. The API supports any language, however, so the examples below are
written using the curl command-line tool.

A program that wishes to fetch a secret at runtime does so by issuing an HTTPS
POST request to the [`/api/get` method] of the secrets service.

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
client is using, and set the `"updateIfChanged"` flag to `true`:

```json
{"Name":"dev/hello-world", "Version":7, "UpdateIfChanged":true}
```

If the current active version of the secret still matches what the client
requested, the server will report 304 Not Modified, indicating to the client
that it still (already) has the active version. This allows the client to check
for an update without sending secret values over the wire except when needed,
and does not trigger an auditable access record unless the value or the
permissions have changed.

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

Alternatively, you can obtain a [`setec.Watcher`][setecwatcher], which combines
a secret with a channel that notifies when a new secret value is available:

```go
// Get the handle, as before.
apiKey := st.Watcher("prod/my-program/secret-1")

// Create a client with the current value.
cli := someservice.NewClient("username", apiKey.Get())

// Make a helper that will refresh the client when the secret updates.
// This example assumes no concurrency; you may need a lock if multiple
// goroutines will request a client at the same time.
getClient := func() *someservice.Client {
   select {
   case <-apiKey.Ready():
     cli = someservice.NewClient("username", apiKey.Get())
   default:
   }
   return cli
}

// Make API calls using the helper.
rsp, err := getClient().Method(ctx, args)
// ...
```

### Bootstrapping and Availability

A reasonable concern when fetching secrets from a network service is what
happens if the secrets service is not reachable when a program needs to fetch a
secret (e.g., when it is starting up). A good answer depends on the nature of
the program: Batch processing tools can usually afford to wait and retry until
the service becomes available. Interactive services, by contrast, may not be
able to tolerate waiting.

A program that needs be able to start even when the secrets server is
unavailable can trade a bit of security for availability by caching the active
versions of the secrets it needs in a local file. When the program start or
restarts, it can fall back to the cached values if the secrets service is not
immediately available. The Go client library's [`setec.Store`][setecstore] type
supports this kind of caching as an optional feature, and the same logic can be
implemented in any language.

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
   PollInterval: 12*time.Hour,
   Cache:        fc,
})
// ...
```

With the cache enabled, the store will automatically persist new secrets
fetched from the server into the cache as they become available. Morever, when
the store is created, the store will not block waiting for the server if all
the requested secrets already have a version stored in the cache. This permits
the program to start up even if the setec server is not immediately available.

## Testing

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
[setecclient]: https://godoc.org/github.com/tailscale/setec/client/setec#Client
[setecstore]: https://godoc.org/github.com/tailscale/setec/client/setec#Store
[setectest]: https://godoc.org/github.com/tailscale/setec/setectest
[setecwatcher]: https://godoc.org/github.com/tailscale/setec/client/setec#Watcher
[stserver]: https://godoc.org/github.com/tailscale/setec/setectest#Server

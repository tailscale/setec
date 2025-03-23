# Running a setec server

The `server` subcommand of the [`setec` command line tool][cli] implements an
HTTP server for the [setec API](./api.md). The server uses [tsnet][tsnet] to
join a tailnet of your choosing.

The CLI is written in [Go]. To install the command-line tool, either:

```shell
go install github.com/tailscale/setec/cmd/setec@latest
```

or clone this repository and build it from source:

```shell
git clone https://github.com/tailscale/setec
cd setec
go build ./cmd/setec
```

The rest of this document assumes you have the `setec` command-line tool
somewhere in your `$PATH`.

## Basic Setup

Run `setec help server` for a summary of command-line options. To run the
server you must provide at least a `--hostname` for the service to use, and a
`--state-dir` path where it will store its persistent state.

The first time you run the server you must also provide a [Tailscale auth
key][tsauth] via the `TS_AUTHKEY` environment variable, so that the server can
join your tailnet. You can omit this on subsequent invocations -- the server
will use the contents of the specified `--state-dir` to reconnect to the same
tailnet.

## Key Management

The server stores secrets in an encrypted file in the state directory. When the
server starts, it requires an **access key** to unlock the database.

In production, the server fetches an access key from an AWS KMS secret, whose
ARN is specified via the `--kms-key-name` flag. As of 05-May-2024, AWS KMS is
the only supported production access key store; we may add others in the
future.

This mode also requires access to the AWS APIs: If you are running the server
in AWS (e.g., an EC2 VM), you would typically grant access to the key via an
IAM role on the VM. Alternatively, you can plumb in credentials via environment
variables, for example using [`aws-vault`][awsvault] or similar.

For development and testing purposes, the server also supports a `--dev` flag,
which runs using a "dummy" static access key. **This mode is not secure for
production use**, but is useful for testing and debugging integrations locally.

## Usage Examples

> [!NOTE]
> In the examples below, you must provide a real value for `TS_AUTHKEY`
> obtained from your [tailnet's admin panel][admin-keys]. The value shown below
> is a fake key that will not work.

1. To run a setec server in development mode under the hostname `setec-dev`:

    ```shell
    TS_AUTHKEY=tskey-auth-kf4k3k3y4testCNTRL-ZmFrZSBrZXkgZm9yIHRlc3Q setec server \
      --hostname=setec-dev \
      --state-dir=$HOME/setec-dev \
      --dev
    ```

2. To run a setec server in production mode under the hostname `secrets`:

    ```shell
    TS_AUTHKEY=tskey-auth-kf4k3k3y4testCNTRL-ZmFrZSBrZXkgZm9yIHRlc3Q setec server \
      --hostname=secrets \
      --state-dir=$HOME/setec-state \
      --kms-key-name=arn:aws:kms:us-east-1:123456789012:key/b8074b63-13c0-4345-a9d8-e236267d2af1
    ```

    Note that the KMS key name shown here is a fake one, you must replace it
    with a real one from your own account. You may also need to set up other
    AWS environment variables (e.g. `AWS_ACCESS_KEY_ID` and
    `AWS_SECRET_ACCESS_KEY`).  For example, using `aws-vault` it might look
    like this:

    ```shell
    TS_AUTHKEY=tskey-auth-kf4k3k3y4testCNTRL-ZmFrZSBrZXkgZm9yIHRlc3Q aws-vault exec myaccount -- \
      setec server \
        --hostname=setec-dev \
        --state-dir=$HOME/setec-dev \
        --kms-key-name=arn:aws:kms:us-east-1:123456789012:key/b8074b63-13c0-4345-a9d8-e236267d2af1
    ```

Once you have run the server, you can grant access to it via your [tailnet
ACL][acl]. For example, if we assume your server's Tailscale address is
100.64.5.6, the following [ACL grants][grant] would give the administrators of
your tailnet full access to all secrets in your service via the API:


```hujson
    "grants": [
        {
            "ip":  ["*"],
            "src": ["autogroup:admin"],
            "dst": ["100.64.5.6"],

            // Alternatively, assign your node a tag, e.g., tag:secrets, and
            // use the tag as the dst instead.
        },
        {
            "src": ["autogroup:admin"],
            "dst": ["100.64.5.6"],
            "app": {
                "tailscale.com/cap/secrets": [
                    {
                        "action": ["get", "info", "put", "activate", "delete"],
                        "secret": ["*"],
                    },
                ],
            },
        },
    ],
```

In practice, you will want to scope these permissions more narrowly, e.g.,
granting `"get"` permission for individual secrets only to the servers that
need those values.

To test that this is working properly on a new server, try:

```shell
echo -n "hello, world" | setec -s https://setec-dev.example.ts.net put dev/hello-world
```

(replacing `example.ts.net` with your tailnet name). This should print:

```
Read 12 bytes from stdin
Secret saved as "dev/hello-world", version 1
```

Assuming that works, you should then be able to run:

```shell
setec -s https://setec-dev.example.ts.net list
```

which should give you output like:

```
NAME            ACTIVE VERSIONS
dev/hello-world 1      1
```

Note that the first time you call the server, it may take thirty seconds or
longer as the server will need to obtain a TLS certificate from LetsEncrypt.
Subsequent calls will run faster.

## Other Considerations

### Backups

When running setec in production, you will generally want to keep backups of
your secrets data. The `setec server` command has basic support for automatic
backups to S3 via the optional `--backup-bucket` and `--backup-bucket-region`
flags.  When these are set, the server automatically backs up the database to a
timestamped object in S3 up to once per minute, if its contents have changed
since the last backup.

The uploaded backups are fully encrypted.

### Audit Logs

While running, the server appends a basic audit log of all secret accesses to a
file called `audit.log` in its state directory. These logs can be used to check
which secrets were accessed when, by which users and/or services on the
tailnet.  For now (as of 05-May-2024), the audit logs are stored only in the
server's state directory.


[acl]: https://tailscale.com/kb/1018/acls
[admin-keys]: https://login.tailscale.com/admin/settings/keys
[awsvault]: https://github.com/99designs/aws-vault
[cli]: https://github.com/tailscale/setec/tree/main/cmd/setec
[go]: https://golang.org/dl
[grant]: https://tailscale.com/kb/1324/acl-grants
[tsauth]: https://tailscale.com/kb/1085/auth-keys
[tsnet]: https://godoc.org/tailscale.com/tsnet

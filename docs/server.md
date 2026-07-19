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
ARN is specified via the `--kms-key-name` flag.

This mode also requires access to the AWS APIs: If you are running the server
in AWS (e.g., an EC2 VM), you would typically grant access to the key via an
IAM role on the VM. Alternatively, you can plumb in credentials via environment
variables, for example using [`aws-vault`][awsvault] or similar.

For environments where no KMS is available (e.g., a homelab), the server can
instead use a key sealed to a local TPM 2.0 device, via the `--tpm-key-file`
flag. The flag names a file in which the server stores a TPM-sealed key blob;
if the file does not exist, the server generates a new key, seals it to the
TPM, and saves it there on first startup. The sealed blob can only be unsealed
by the TPM that created it, so a copy of the database and the key file together
cannot be decrypted elsewhere. By default the server uses the TPM at
`/dev/tpmrm0`; use `--tpm-device` to select a different device.

This also works with virtual TPMs, such as the vTPM QEMU (and thus Proxmox) can
attach to a VM. Two caveats to be aware of when using a vTPM:

- A vTPM's state is stored by the host (for Proxmox, in the VM's "TPM State"
  disk), so an attacker who obtains that state along with the database can
  still recover the key. Snapshots or backups of the whole VM including its
  TPM state likewise contain everything needed to decrypt the database.
- The key is bound to that vTPM instance: if the VM is rebuilt without
  preserving its TPM state, the database becomes unrecoverable. Keep a
  separate backup of the database key if you cannot afford that risk.

On systems without TPM support (such as Darwin), the server will report an
error at startup when `--tpm-key-file` is set.

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

3. To run a self-hosted setec server using a key sealed to the local TPM:

    ```shell
    TS_AUTHKEY=tskey-auth-kf4k3k3y4testCNTRL-ZmFrZSBrZXkgZm9yIHRlc3Q setec server \
      --hostname=secrets \
      --state-dir=$HOME/setec-state \
      --tpm-key-file=$HOME/setec-state/setec-tpm.key
    ```

    The sealed key file is created automatically the first time the server
    starts. The server must be able to read the TPM device (`/dev/tpmrm0` by
    default): either run it as root, or add its user to the group owning the
    device.

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
[awsvault]: https://github.com/ByteNess/aws-vault
[cli]: https://github.com/tailscale/setec/tree/main/cmd/setec
[go]: https://golang.org/dl
[grant]: https://tailscale.com/kb/1324/acl-grants
[tsauth]: https://tailscale.com/kb/1085/auth-keys
[tsnet]: https://godoc.org/tailscale.com/tsnet

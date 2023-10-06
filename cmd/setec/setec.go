// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// Program setec is a secret management server that vends secrets over
// Tailscale, and a client tool to communicate with that server.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"expvar"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/creachadair/command"
	"github.com/creachadair/flax"
	"github.com/tailscale/setec/audit"
	"github.com/tailscale/setec/client/setec"
	"github.com/tailscale/setec/server"
	"github.com/tailscale/setec/types/api"
	"github.com/tink-crypto/tink-go-awskms/integration/awskms"
	"github.com/tink-crypto/tink-go/v2/testutil"
	"github.com/tink-crypto/tink-go/v2/tink"
	"golang.org/x/term"
	"tailscale.com/tsnet"
	"tailscale.com/tsweb"
)

func main() {
	root := &command.C{
		Name:  filepath.Base(os.Args[0]),
		Usage: "server [options]\ncommand [flags] ...\nhelp [command]",
		Help: `A server and command-line tool for the setec API.

The "server" subcommand starts a server for the setec API.
The other subcommands call methods of a running setec server.

Client commands must provide a server URL with the -s flag, or via the
SETEC_SERVER environment variable.`,

		SetFlags: command.Flags(flax.MustBind, &clientArgs),

		Commands: []*command.C{
			{
				Name: "server",
				Help: `Run the setec server.

Start the server over Tailscale with the specified --hostname and --state-dir.
The first time you run the server, you must provide a TS_AUTHKEY to authorize
the node on the tailnet.

With the --dev flag, the server runs with a dummy KMS. This mode is intended
for debugging and is NOT SAFE for production use.

Otherwise you must provide a --kms-key-name to use to encrypt the database.`,

				SetFlags: command.Flags(flax.MustBind, &serverArgs),
				Run:      command.Adapt(runServer),
			},
			{
				Name: "list",
				Help: "List all secrets visible to the caller.",
				Run:  command.Adapt(runList),
			},
			{
				Name:  "info",
				Usage: "<secret-name>",
				Help:  "Get metadata for the specified secret.",
				Run:   command.Adapt(runInfo),
			},
			{
				Name:  "get",
				Usage: "<secret-name>",
				Help: `Get the active value of the specified secret.

With --version, fetch the specified version instead of the active one.
With --if-changed, return the active value only if it differs from --version.`,

				SetFlags: command.Flags(flax.MustBind, &getArgs),
				Run:      command.Adapt(runGet),
			},
			{
				Name:  "put",
				Usage: "<secret-name>",
				Help: `Put a new value for the specified secret.

With --from-file, the new value is read from the specified file; otherwise if
stdin is connected to a pipe, its contents are fully read to obtain the new
value. Otherwise, the user is prompted for a new value and confirmation.`,

				SetFlags: command.Flags(flax.MustBind, &putArgs),
				Run:      command.Adapt(runPut),
			},
			{
				Name:  "activate",
				Usage: "<secret-name> <secret-version>",
				Help:  "Set the active version of the specified secret.",
				Run:   command.Adapt(runActivate),
			},
			{
				Name:  "delete-version",
				Usage: "<secret-name> <secret-version> [<confirm-token>]",
				Help: `Delete the specified non-active version of a secret.

A confirmation token is required to delete a secret value.  Run the command to
generate the token, then re-run appending the provided value.`,

				Run: command.Adapt(runDeleteVersion),
			},
			{
				Name:  "delete",
				Usage: "<secret-name> [<confirm-token>]",
				Help: `Delete all versions of a secret (including active).

A confirmation token is required to delete a secret.  Run the command to
generate the token, then re-run appending the provided value.`,

				Run: command.Adapt(runDeleteSecret),
			},
			command.HelpCommand(nil),
			command.VersionCommand(),
		},
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	env := root.NewEnv(nil).SetContext(ctx).MergeFlags(true)
	command.RunOrFail(env, os.Args[1:])
}

var serverArgs struct {
	StateDir           string `flag:"state-dir,Server state directory"`
	Hostname           string `flag:"hostname,Tailscale hostname to use"`
	KMSKeyName         string `flag:"kms-key-name,Name of KMS key to use for database encryption"`
	BackupBucket       string `flag:"backup-bucket,Name of AWS S3 bucket to use for database backups"`
	BackupBucketRegion string `flag:"backup-bucket-region,AWS region of the backup S3 bucket"`
	BackupRole         string `flag:"backup-role,Name of AWS IAM role to assume to write backups"`
	Dev                bool   `flag:"dev,Run in developer mode"`
}

var clientArgs struct {
	Server string `flag:"s,default=$SETEC_SERVER,Server address"`
}

func runServer(env *command.Env) error {
	var kek tink.AEAD
	if serverArgs.Dev {
		if serverArgs.StateDir == "" {
			const devState = "setec-dev.state"
			if err := os.MkdirAll(devState, 0700); err != nil {
				return fmt.Errorf("creating dev state dir %q: %w", devState, err)
			}
			serverArgs.StateDir = devState
		}
		if serverArgs.Hostname == "" {
			serverArgs.Hostname = "setec-dev"
		}
		if serverArgs.KMSKeyName == "" {
			kek = &testutil.DummyAEAD{
				Name: "SetecDevOnlyDummyEncryption",
			}
		}
		log.Printf("dev mode: state dir is %q", serverArgs.StateDir)
		log.Printf("dev mode: hostname is %q", serverArgs.Hostname)
		log.Println("dev mode: using dummy KMS, NOT SAFE FOR PRODUCTION USE")
	}

	if serverArgs.StateDir == "" {
		return errors.New("--state-dir must be specified")
	}
	if serverArgs.Hostname == "" {
		return errors.New("--hostname must be specified")
	}
	if kek == nil {
		if serverArgs.KMSKeyName == "" {
			return errors.New("--kms-key-name must be specified")
		}
		// Tink requires prefixing the key identifier with a URI
		// scheme that identifies the correct backend to use.
		uri := "aws-kms://" + serverArgs.KMSKeyName
		kmsClient, err := awskms.NewClientWithOptions(uri)
		if err != nil {
			return fmt.Errorf("creating AWS KMS client: %v", err)
		}
		kek, err = kmsClient.GetAEAD(uri)
		if err != nil {
			return fmt.Errorf("getting KMS key handle: %v", err)
		}
	}

	s := &tsnet.Server{
		Dir:      filepath.Join(serverArgs.StateDir, "tsnet"),
		Hostname: serverArgs.Hostname,
	}

	lc, err := s.LocalClient()
	if err != nil {
		return fmt.Errorf("getting tailscale localapi client: %v", err)
	}

	// Wait until tailscale is fully up, so that CertDomains has data.
	if _, err := s.Up(context.Background()); err != nil {
		return fmt.Errorf("tailscale did not come up: %w", err)
	}

	doms := s.CertDomains()
	if len(doms) == 0 {
		return fmt.Errorf("tailscale did not provide TLS domains")
	}
	fqdn := doms[0]

	mux := http.NewServeMux()
	tsweb.Debugger(mux)

	audit, err := audit.NewFile(filepath.Join(serverArgs.StateDir, "audit.log"))
	if err != nil {
		return fmt.Errorf("opening audit log: %w", err)
	}

	srv, err := server.New(env.Context(), server.Config{
		DBPath:             filepath.Join(serverArgs.StateDir, "database"),
		Key:                kek,
		AuditLog:           audit,
		WhoIs:              lc.WhoIs,
		BackupBucket:       serverArgs.BackupBucket,
		BackupBucketRegion: serverArgs.BackupBucketRegion,
		BackupAssumeRole:   serverArgs.BackupRole,
		Mux:                mux,
	})
	if err != nil {
		return fmt.Errorf("initializing setec server: %v", err)
	}
	expvar.Publish("setec_server", srv.Metrics())

	l80, err := s.Listen("tcp", ":80")
	if err != nil {
		return fmt.Errorf("creating HTTP listener: %v", err)
	}
	go func() {
		port80 := tsweb.Port80Handler{
			Main: mux,
			FQDN: fqdn,
		}
		if err := http.Serve(l80, port80); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serving HTTP: %v", err)
		}
	}()

	l, err := s.ListenTLS("tcp", ":443")
	if err != nil {
		return fmt.Errorf("creating TLS listener: %v", err)
	}
	hs := &http.Server{Handler: tsweb.BrowserHeaderHandler(mux)}
	go func() {
		<-env.Context().Done()
		log.Print("Signal received, stopping...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hs.Shutdown(ctx)
	}()

	if err := hs.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serving HTTPS: %v", err)
	}

	return nil
}

func newClient() (*setec.Client, error) {
	if clientArgs.Server == "" {
		return nil, errors.New("no server address is set")
	}
	return &setec.Client{Server: clientArgs.Server}, nil
}

func runList(env *command.Env) error {
	c, err := newClient()
	if err != nil {
		return err
	}

	secrets, err := c.List(env.Context())
	if err != nil {
		return fmt.Errorf("failed to list secrets: %v", err)
	}

	tw := newTabWriter(os.Stdout)
	io.WriteString(tw, "NAME\tACTIVE\tVERSIONS\n")
	for _, s := range secrets {
		vers := make([]string, 0, len(s.Versions))
		for _, v := range s.Versions {
			vers = append(vers, v.String())
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Name, s.ActiveVersion, strings.Join(vers, ","))
	}
	return tw.Flush()
}

func runInfo(env *command.Env, name string) error {
	c, err := newClient()
	if err != nil {
		return err
	}

	info, err := c.Info(env.Context(), name)
	if err != nil {
		return fmt.Errorf("failed to get secret info: %v", err)
	}
	vers := make([]string, 0, len(info.Versions))
	for _, v := range info.Versions {
		vers = append(vers, v.String())
	}
	tw := newTabWriter(os.Stdout)
	fmt.Fprintf(tw, "Name:\t%s\n", info.Name)
	fmt.Fprintf(tw, "Active version:\t%s\n", info.ActiveVersion)
	fmt.Fprintf(tw, "Versions:\t%s\n", strings.Join(vers, ", "))
	return tw.Flush()
}

var getArgs struct {
	IfChanged bool   `flag:"if-changed,Get active version if changed from --version"`
	Version   uint64 `flag:"version,Secret version to retrieve (default: the active version)"`
}

func runGet(env *command.Env, name string) error {
	c, err := newClient()
	if err != nil {
		return err
	}

	var val *api.SecretValue
	if getArgs.Version == 0 {
		val, err = c.Get(env.Context(), name)
	} else if getArgs.IfChanged {
		val, err = c.GetIfChanged(env.Context(), name, api.SecretVersion(getArgs.Version))
	} else {
		val, err = c.GetVersion(env.Context(), name, api.SecretVersion(getArgs.Version))
	}
	if err != nil {
		return fmt.Errorf("failed to get secret: %v", err)
	}

	// Print with a newline if a human's going to look at it,
	// otherwise output just the secret bytes.
	if term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Println(string(val.Value))
	} else {
		os.Stdout.Write(val.Value)
	}
	return nil
}

var putArgs struct {
	File string `flag:"from-file,Read secret value from this file instead of stdin"`
}

func runPut(env *command.Env, name string) error {
	c, err := newClient()
	if err != nil {
		return err
	}

	var value []byte
	if putArgs.File != "" {
		// The user requested we use input from a file.
		var err error
		value, err = os.ReadFile(putArgs.File)
		if err != nil {
			return err
		}
		value = bytes.TrimSpace(value)
	} else if term.IsTerminal(int(os.Stdin.Fd())) {
		// Standard input is connected to a terminal; prompt the human to type or
		// paste the value and require confirmation.
		var err error
		io.WriteString(os.Stdout, "Enter secret: ")
		os.Stdout.Sync()
		value, err = term.ReadPassword(int(os.Stdin.Fd()))
		io.WriteString(os.Stdout, "\n")
		if err != nil {
			return err
		}
		if len(value) == 0 {
			return errors.New("no secret provided, aborting")
		}
		io.WriteString(os.Stdout, "Confirm secret: ")
		os.Stdout.Sync()
		s2, err := term.ReadPassword(int(os.Stdin.Fd()))
		io.WriteString(os.Stdout, "\n")
		if err != nil {
			return err
		}
		if !bytes.Equal(value, s2) {
			return errors.New("secrets do not match, aborting")
		}
	} else {
		var err error
		value, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read from stdin: %w", err)
		} else if len(value) == 0 {
			return errors.New("empty secret value")
		}
		fmt.Fprintf(env, "Read %d bytes from stdin\n", len(value))
	}

	ver, err := c.Put(env.Context(), name, value)
	if err != nil {
		return fmt.Errorf("failed to write secret: %w", err)
	}
	fmt.Printf("Secret saved as %q, version %d\n", name, ver)
	return nil
}

func runActivate(env *command.Env, name, versionString string) error {
	c, err := newClient()
	if err != nil {
		return err
	}

	version, err := strconv.ParseUint(versionString, 10, 32)
	if err != nil {
		return fmt.Errorf("invalid version %q: %w", versionString, err)
	}

	if err := c.Activate(env.Context(), name, api.SecretVersion(version)); err != nil {
		return fmt.Errorf("failed to set active version: %w", err)
	}

	return nil
}

func runDeleteVersion(env *command.Env, name, versionString string, rest ...string) error {
	c, err := newClient()
	if err != nil {
		return err
	}
	var token string
	if len(rest) != 0 {
		token = rest[0]
	}

	version, err := strconv.ParseUint(versionString, 10, 32)
	if err != nil {
		return fmt.Errorf("invalid version %q: %w", versionString, err)
	}
	req := fmt.Sprintf("delete-version:%s:%d", name, version)
	if err := checkConfirmation(req, token); err != nil {
		return err
	}
	if err := c.DeleteVersion(env.Context(), name, api.SecretVersion(version)); err != nil {
		return fmt.Errorf("failed to delete secret %q version %d: %w", name, version, err)
	}
	return nil
}

func runDeleteSecret(env *command.Env, name string, rest ...string) error {
	c, err := newClient()
	if err != nil {
		return err
	}
	var token string
	if len(rest) != 0 {
		token = rest[0]
	}

	req := fmt.Sprintf("delete-secret:%s", name)
	if err := checkConfirmation(req, token); err != nil {
		return err
	}
	if err := c.Delete(env.Context(), name); err != nil {
		return fmt.Errorf("failed to delete secret %q: %w", name, err)
	}
	return nil
}

// newConfirmationToken returns a nonce "token" that must be supplied to
// perform a dangerous operation like deleting a secret or secret value.
// The token is not a security feature, it is just a request digest with a
// timestamp to reduce the chances of things getting deleted by accident.
func newConfirmationToken(req string) string {
	// Code format: <time-window>.<req-digest>
	//
	// Confirmation codes last about 1 minute after construction, as a cheap
	// hedge against copy-pasta from old script output or command history.  The
	// digest is just to tie the token to the specific request.
	window := (int64(time.Now().Unix()) + 119) / 60 // round up
	sum := sha256.Sum256([]byte(req))
	return fmt.Sprintf("%x.%x", window, sum[:8])
}

func checkConfirmation(req, token string) error {
	if token == "" {
		return fmt.Errorf("confirmation required for %q, use token %q", req, newConfirmationToken(req))
	} else if want := newConfirmationToken(req); token != want {
		return fmt.Errorf("incorrect confirmation for %q, use token %q", req, want)
	}
	return nil
}

func newTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 4, 1, ' ', 0)
}

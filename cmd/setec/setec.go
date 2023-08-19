// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// Program setec is a secret management server that vends secrets over
// Tailscale, and a client tool to communicate with that server.
package main

import (
	"bytes"
	"context"
	"errors"
	"expvar"
	"flag"
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
The other subcommands call methods of a running setec server.`,

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
				Name:  "list",
				Usage: "<server>",
				Help:  "List all secrets visible to the caller.",
				Run:   command.Adapt(runList),
			},
			{
				Name:  "info",
				Usage: "<server> <secret-name>",
				Help:  "Get metadata for the specified secret.",
				Run:   command.Adapt(runInfo),
			},
			{
				Name:  "get",
				Usage: "<server> <secret-name>",
				Help: `Get the active value of the specified secret.

With --version, fetch the specified version instead of the active one.
With --if-changed, return the active value only if it differs from --version.`,

				SetFlags: command.Flags(flax.MustBind, &getArgs),
				Run:      command.Adapt(runGet),
			},
			{
				Name:  "put",
				Usage: "<server> <secret-name>",
				Help: `Put a new value for the specified secret.

With --from-file, the new value is read from the specified file; otherwise
the user is prompted for a new value and confirmation at the terminal.`,

				SetFlags: func(_ *command.Env, fs *flag.FlagSet) { flax.MustBind(fs, &putArgs) },
				Run:      command.Adapt(runPut),
			},
			{
				Name:  "set-active",
				Usage: "<server> <secret-name> <secret-version>",
				Help:  "Set the active version of the specified secret.",
				Run:   command.Adapt(runSetActive),
			},
			command.HelpCommand(nil),
		},
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	command.RunOrFail(root.NewEnv(nil).SetContext(ctx), os.Args[1:])
}

var serverArgs struct {
	StateDir   string `flag:"state-dir,Server state directory"`
	Hostname   string `flag:"hostname,Tailscale hostname to use"`
	KMSKeyName string `flag:"kms-key-name,Name of KMS key to use for database encryption"`
	Dev        bool   `flag:"dev,Run in developer mode"`
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

	srv, err := server.New(server.Config{
		DBPath:   filepath.Join(serverArgs.StateDir, "database"),
		Key:      kek,
		AuditLog: audit,
		WhoIs:    lc.WhoIs,
		Mux:      mux,
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
	if err := http.Serve(l, tsweb.BrowserHeaderHandler(mux)); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serving HTTPS: %v", err)
	}

	return nil
}

func newClient(url string) *setec.Client { return &setec.Client{Server: url} }

func runList(env *command.Env, server string) error {
	c := newClient(server)

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

func runInfo(env *command.Env, server, name string) error {
	c := newClient(server)

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

func runGet(env *command.Env, server, name string) error {
	c := newClient(server)

	var val *api.SecretValue
	var err error
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
	File string `flag:"from-file,Read secret value from this file instead of prompting"`
}

func runPut(env *command.Env, server, name string) error {
	c := newClient(server)

	var value []byte
	if putArgs.File != "" {
		var err error
		value, err = os.ReadFile(putArgs.File)
		if err != nil {
			return err
		}
		value = bytes.TrimSpace(value)
	} else {
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
	}

	ver, err := c.Put(env.Context(), name, value)
	if err != nil {
		return fmt.Errorf("failed to write secret: %w", err)
	}
	fmt.Printf("Secret saved as %q, version %d\n", name, ver)
	return nil
}

func runSetActive(env *command.Env, server, name, versionString string) error {
	c := newClient(server)

	version, err := strconv.ParseUint(versionString, 10, 32)
	if err != nil {
		return fmt.Errorf("invalid version %q: %w", version, err)
	}

	if err := c.SetActiveVersion(env.Context(), name, api.SecretVersion(version)); err != nil {
		return fmt.Errorf("failed to set active version: %w", err)
	}

	return nil
}

func newTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 4, 1, ' ', 0)
}

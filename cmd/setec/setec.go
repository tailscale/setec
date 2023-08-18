// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// setec is a simple secret management server that vends secrets over
// Tailscale.
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
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/peterbourgon/ff/v3/ffcli"
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
	serverCmd := &ffcli.Command{
		Name:       "server",
		ShortUsage: "setec server [flags]",
		ShortHelp:  "run the setec server",
		FlagSet: func() *flag.FlagSet {
			fs := flag.NewFlagSet("server", flag.ExitOnError)
			fs.StringVar(&serverArgs.StateDir, "state-dir", "", "setec state dir")
			fs.StringVar(&serverArgs.Hostname, "hostname", "", "Tailscale hostname to use")
			fs.StringVar(&serverArgs.KMSKeyName, "kms-key-name", "", "name of KMS key to use for database encryption")
			fs.BoolVar(&serverArgs.Dev, "dev", false, "dev mode")
			return fs
		}(),
		Exec: runServer,
	}
	listCmd := &ffcli.Command{
		Name:       "list",
		ShortUsage: "setec list [flags] <server>",
		ShortHelp:  "list secrets",
		Exec:       runList,
	}
	infoCmd := &ffcli.Command{
		Name:       "info",
		ShortUsage: "setec info [flags] <server> <secret-name>",
		ShortHelp:  "get secret info",
		Exec:       runInfo,
	}
	getCmd := &ffcli.Command{
		Name:       "get",
		ShortUsage: "setec get [flags] <server> <secret-name>",
		ShortHelp:  "get a secret",
		FlagSet: func() *flag.FlagSet {
			fs := flag.NewFlagSet("get", flag.ExitOnError)
			fs.BoolVar(&getArgs.IfChanged, "if-changed", false, "get active version if changed from --version")
			fs.Uint64Var(&getArgs.Version, "version", 0, "secret version to retrieve (default: the active version)")
			return fs
		}(),
		Exec: runGet,
	}
	putCmd := &ffcli.Command{
		Name:       "put",
		ShortUsage: "setec put [flags] <server> <secret-name>",
		ShortHelp:  "put a secret",
		FlagSet: func() *flag.FlagSet {
			fs := flag.NewFlagSet("put", flag.ExitOnError)
			fs.StringVar(&putArgs.File, "from-file", "", "read secret value from file instead of prompting interactively")
			return fs
		}(),
		Exec: runPut,
	}
	setActiveCmd := &ffcli.Command{
		Name:       "activate",
		ShortUsage: "setec activate [flags] <server> <secret-name> <secret-version>",
		ShortHelp:  "activate a secret version",
		Exec:       runSetActive,
	}

	root := &ffcli.Command{
		Name:       "setec",
		ShortUsage: "setec <subcmd>",
		Subcommands: []*ffcli.Command{
			serverCmd,
			listCmd,
			infoCmd,
			getCmd,
			putCmd,
			setActiveCmd,
		},
		Exec: func(context.Context, []string) error { return flag.ErrHelp },
	}

	if err := root.ParseAndRun(context.Background(), os.Args[1:]); errors.Is(err, flag.ErrHelp) {
		// A command tried to run but instructed us to print
		// help. Exit unsuccessfully to convey that the intended
		// command didn't run. Note, this branch isn't taken when the
		// user requests --help explicitly.
		os.Exit(2)
	} else if err != nil {
		log.Fatal(err)
	}
}

var serverArgs struct {
	StateDir   string
	Hostname   string
	KMSKeyName string
	Dev        bool
}

func runServer(ctx context.Context, args []string) error {
	if len(args) > 0 {
		return flag.ErrHelp
	}

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

func clientFromArgs(args []string) (client *setec.Client, remainingArgs []string, err error) {
	if len(args) < 1 {
		return nil, nil, flag.ErrHelp
	}
	server := args[0]
	// TODO(corp/13375): make a better UX here where single-label
	// hostnames get expanded into a full HTTPS URL.
	return &setec.Client{
		Server: server,
	}, args[1:], nil
}

func runList(ctx context.Context, args []string) error {
	c, args, err := clientFromArgs(args)
	if err != nil {
		return err
	}
	if len(args) > 0 {
		return flag.ErrHelp
	}

	secrets, err := c.List(ctx)
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

func runInfo(ctx context.Context, args []string) error {
	c, args, err := clientFromArgs(args)
	if err != nil {
		return err
	}
	if len(args) != 1 {
		return flag.ErrHelp
	}

	name := args[0]
	info, err := c.Info(ctx, name)
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
	IfChanged bool
	Version   uint64
}

func runGet(ctx context.Context, args []string) error {
	c, args, err := clientFromArgs(args)
	if err != nil {
		return err
	}
	if len(args) != 1 {
		return flag.ErrHelp
	}

	name := args[0]

	var val *api.SecretValue
	if getArgs.Version == 0 {
		val, err = c.Get(ctx, name)
	} else if getArgs.IfChanged {
		val, err = c.GetIfChanged(ctx, name, api.SecretVersion(getArgs.Version))
	} else {
		val, err = c.GetVersion(ctx, name, api.SecretVersion(getArgs.Version))
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
	File string
}

func runPut(ctx context.Context, args []string) error {
	c, args, err := clientFromArgs(args)
	if err != nil {
		return err
	}
	if len(args) != 1 {
		return flag.ErrHelp
	}

	name := args[0]

	var value []byte
	if putArgs.File != "" {
		value, err = os.ReadFile(putArgs.File)
		if err != nil {
			return err
		}
		value = bytes.TrimSpace(value)
	} else {
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

	ver, err := c.Put(ctx, name, value)
	if err != nil {
		return fmt.Errorf("failed to write secret: %w", err)
	}
	fmt.Printf("Secret saved as %q, version %d\n", name, ver)
	return nil
}

func runSetActive(ctx context.Context, args []string) error {
	c, args, err := clientFromArgs(args)
	if err != nil {
		return err
	}
	if len(args) != 2 {
		return flag.ErrHelp
	}

	name := args[0]
	version, err := strconv.ParseUint(args[1], 10, 32)
	if err != nil {
		return fmt.Errorf("invalid version %q: %w", version, err)
	}

	if err := c.SetActiveVersion(ctx, name, api.SecretVersion(version)); err != nil {
		return fmt.Errorf("failed to set active version: %w", err)
	}

	return nil
}

func newTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 4, 1, ' ', 0)
}

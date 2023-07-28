// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// setec is a simple secret management server that vends secrets over
// Tailscale.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/peterbourgon/ff/v3/ffcli"
	"github.com/tailscale/setec/server"
	"github.com/tink-crypto/tink-go/v2/testutil"
	"github.com/tink-crypto/tink-go/v2/tink"
	"tailscale.com/tsnet"
	"tailscale.com/tsweb"
)

func main() {
	serverCmd := &ffcli.Command{
		Name:      "server",
		ShortHelp: "run the setec server",
		FlagSet: func() *flag.FlagSet {
			fs := flag.NewFlagSet("server", flag.ExitOnError)
			fs.StringVar(&serverArgs.StateDir, "state-dir", "", "tsnet state dir")
			fs.StringVar(&serverArgs.Hostname, "hostname", "", "Tailscale hostname to use")
			fs.StringVar(&serverArgs.KMSKeyName, "kms-key-name", "", "name of KMS key to use for database encryption")
			fs.BoolVar(&serverArgs.Dev, "dev", false, "dev mode")
			return fs
		}(),
		Exec: runServer,
	}

	root := &ffcli.Command{
		Name:        "setec",
		Subcommands: []*ffcli.Command{serverCmd},
		Exec:        func(context.Context, []string) error { return flag.ErrHelp },
	}

	if err := root.ParseAndRun(context.Background(), os.Args[1:]); err != nil {
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
		return errors.New("unexpected extra positional arguments")
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
		// TODO(corp/13375): hook up to cloud KMS, and have a --dev mode.
		return errors.New("TODO: hookup to AWS KMS not implemented yet.")
	}

	s := &tsnet.Server{
		Dir:      filepath.Join(serverArgs.StateDir, "tsnet"),
		Hostname: serverArgs.Hostname,
	}

	lc, err := s.LocalClient()
	if err != nil {
		return fmt.Errorf("getting tailscale localapi client: %v", err)
	}

	mux := http.NewServeMux()
	tsweb.Debugger(mux)

	_, err = server.New(server.Config{
		DBPath: filepath.Join(serverArgs.StateDir, "database"),
		Key:    kek,
		WhoIs:  lc.WhoIs,
		Mux:    mux,
	})
	if err != nil {
		return fmt.Errorf("initializing setec server: %v", err)
	}

	l80, err := s.Listen("tcp", ":80")
	if err != nil {
		return fmt.Errorf("creating HTTP listener: %v", err)
	}
	go func() {
		if err := http.Serve(l80, tsweb.Port80Handler{Main: mux}); err != nil && !errors.Is(err, http.ErrServerClosed) {
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

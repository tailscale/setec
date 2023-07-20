// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// setec is a simple secret management server that vends secrets over
// Tailscale.
package main

import (
	"errors"
	"flag"
	"log"
	"net/http"
	"path/filepath"

	"tailscale.com/tsnet"
	"tailscale.com/tsweb"
)

var (
	stateDir = flag.String("state-dir", "", "tsnet state dir")
	hostname = flag.String("hostname", "setec-dev", "Tailscale hostname to use")
)

func main() {
	flag.Parse()
	if *stateDir == "" {
		log.Fatal("--state-dir must be specified")
	}

	s := &tsnet.Server{
		Dir:      filepath.Join(*stateDir, "tsnet"),
		Hostname: *hostname,
	}

	mux := http.NewServeMux()
	tsweb.Debugger(mux)

	l80, err := s.Listen("tcp", ":80")
	if err != nil {
		log.Fatalf("creating HTTP listener: %v", err)
	}
	go func() {
		if err := http.Serve(l80, tsweb.Port80Handler{Main: mux}); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serving HTTP: %v", err)
		}
	}()

	l, err := s.ListenTLS("tcp", ":443")
	if err != nil {
		log.Fatalf("creating TLS listener: %v", err)
	}
	if err := http.Serve(l, tsweb.BrowserHeaderHandler(mux)); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serving HTTPS: %v", err)
	}
}

// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package tpmkey

import (
	"fmt"
	"runtime"

	"github.com/google/go-tpm/tpm2/transport"
)

func openDevice(path string) (transport.TPMCloser, error) {
	return nil, fmt.Errorf("tpmkey: unsupported OS: %s", runtime.GOOS)
}

// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package tpmkey_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-tpm/tpm2/transport"
	"github.com/google/go-tpm/tpm2/transport/simulator"

	"github.com/tailscale/setec/tpmkey"
)

// newSimulatedTPM returns a connection to a simulated TPM.
//
// The simulator is a process-wide singleton, so a test that needs a second one
// must Close the first before calling this again.
func newSimulatedTPM(t *testing.T) transport.TPMCloser {
	t.Helper()
	tpm, err := simulator.OpenSimulator()
	if err != nil {
		t.Fatalf("opening TPM simulator: %v", err)
	}
	t.Cleanup(func() { tpm.Close() })
	return tpm
}

func TestOpenOrCreate(t *testing.T) {
	tpm := newSimulatedTPM(t)

	path := filepath.Join(t.TempDir(), "tpm-sealed.key")
	k1, err := tpmkey.OpenOrCreate(tpm, path)
	if err != nil {
		t.Fatalf("OpenOrCreate (create): %v", err)
	}

	plaintext := []byte("hello, world")
	context := []byte("test context")
	ciphertext, err := k1.Encrypt(plaintext, context)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Unsealing again from the same file on the same TPM must yield a
	// key that can decrypt data encrypted with the original.
	k2, err := tpmkey.OpenOrCreate(tpm, path)
	if err != nil {
		t.Fatalf("OpenOrCreate (reopen): %v", err)
	}
	got, err := k2.Decrypt(ciphertext, context)
	if err != nil {
		t.Fatalf("Decrypt with unsealed key: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("Decrypt = %q, want %q", got, plaintext)
	}
}

func TestOpenWrongTPM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tpm-sealed.key")

	// Seal a key on one TPM...
	tpm1 := newSimulatedTPM(t)
	if _, err := tpmkey.OpenOrCreate(tpm1, path); err != nil {
		t.Fatalf("OpenOrCreate (create): %v", err)
	}
	tpm1.Close()

	// ...then attempt to unseal it on a different TPM.
	tpm2 := newSimulatedTPM(t)
	if _, err := tpmkey.OpenOrCreate(tpm2, path); err == nil {
		t.Error("OpenOrCreate on a different TPM unexpectedly succeeded")
	}
}

func TestOpenCorruptFile(t *testing.T) {
	tpm := newSimulatedTPM(t)

	path := filepath.Join(t.TempDir(), "tpm-sealed.key")
	if err := os.WriteFile(path, []byte("not a sealed key"), 0600); err != nil {
		t.Fatalf("writing corrupt file: %v", err)
	}
	if _, err := tpmkey.OpenOrCreate(tpm, path); err == nil {
		t.Error("OpenOrCreate with corrupt file unexpectedly succeeded")
	}
}

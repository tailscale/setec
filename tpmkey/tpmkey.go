// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

// Package tpmkey provides a tink AEAD sealed to a local TPM 2.0 device, as an
// alternative to a cloud KMS.
//
// TPMs cannot perform AEAD operations directly, so instead the TPM is used to
// protect the key material: a random 32-byte key is generated and sealed to
// the TPM, and the resulting sealed blob is stored in a file on disk. At
// startup the blob is unsealed through the TPM and the decrypted key is used
// to construct the AEAD.
//
// The sealed blob can only be unsealed by the TPM that created it, so unlike a
// cleartext key file, a copy of the blob and the database together is not
// sufficient to decrypt the secrets.
package tpmkey

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/google/go-tpm/tpm2"
	"github.com/google/go-tpm/tpm2/transport"
	"github.com/tink-crypto/tink-go/v2/aead/subtle"
	"github.com/tink-crypto/tink-go/v2/tink"
	"tailscale.com/atomicfile"
)

// A quick digression on how and why we "flush" something from the TPM:
//
// Per the TPM2 spec ("TCG PC Client Platform TPM Profile Specification for TPM
// 2.0", found at [0]), the TPM is required to support at minimum 3 loaded objects at a time
// (TPM_PT_HR_TRANSIENT_MIN and TPM_PT_HR_LOADED_MIN). If the TPM runs out of
// slots for loaded objects, it will return a TPM_RC_OBJECT_MEMORY or
// TPM_RC_SESSION_MEMORY error.
//
// This package defaults to using the /dev/tpmrm0 device, which is a "resource
// manager" that will automatically flush objects from the TPM when the process
// closes the device. However, that doesn't prevent us from running out of
// slots during the course of a single process.
//
// To determine whether or not we need to flush an object, we can refer to a
// the TPM2 specification for commands[1]. We break down each command below with
// a citation:
//
//  1. tpm2.Create (section 12.1.1): "The object will need to be loaded
//     (TPM2_Load()) before it may be used" and "This command may require
//     temporary use of a transient resource, even though the object does not
//     remain loaded after the command"
//  2. tpm2.Load (section 12.2.1): "The returned handle is associated with
//     the object until the object is flushed (TPM2_FlushContext()) ..."
//  3. tpm2.Unseal (section 12.7.1): "This command returns the data in a
//     loaded Sealed Data Object", implying that it returns data about a
//     previously-loaded object, but does not itself load an object.
//  4. tpm2.CreatePrimary (section 24.1.1): "The command will create and
//     load a Primary Object."
//
// Thus, we should flush after calling Load or CreatePrimary, but not after
// calling Create or Unseal.
//
// [0]: https://trustedcomputinggroup.org/wp-content/uploads/PC-Client-Specific-Platform-TPM-Profile-for-TPM-2p0-v1p07_rc1_121225.pdf
// [1]: https://trustedcomputinggroup.org/wp-content/uploads/Trusted-Platform-Module-2.0-Library-Part-3-Commands_Version-185_pub.pdf

// DefaultDevice is the TPM device path used when none is specified.
const DefaultDevice = "/dev/tpmrm0"

// keySize is the size in bytes of the sealed AEAD key.
const keySize = 32

// sealedKeyVersion is the version of the on-disk sealed key file.
const sealedKeyVersion = 1

// sealedKey is the on-disk format of the sealed key file.
type sealedKey struct {
	// Version is the version of this sealed key file.
	Version int
	// Public holds the TPM2B_PUBLIC area of the sealed object.
	Public []byte
	// Private holds the TPM2B_PRIVATE area of the sealed object.
	Private []byte
}

// OpenDevicePath opens the TPM device at the given path.
func OpenDevicePath(path string) (transport.TPM, error) {
	return openDevice(path)
}

// OpenOrCreate returns a [tink.AEAD], constructed by unsealing the secret
// sealed to given TPM, with the sealed blob stored in the file at path.
//
// If no file exists at path, a new key is generated, sealed to the TPM, and
// saved there with mode 0600.
func OpenOrCreate(tpm transport.TPM, path string) (tink.AEAD, error) {
	bs, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return create(tpm, path)
	} else if err != nil {
		return nil, err
	}

	var sk sealedKey
	if err := json.Unmarshal(bs, &sk); err != nil {
		return nil, fmt.Errorf("loading sealed key file %q: %w", path, err)
	}
	if sk.Version != sealedKeyVersion {
		return nil, fmt.Errorf("unsupported sealed key file version %d", sk.Version)
	}
	pub, err := tpm2.Unmarshal[tpm2.TPM2BPublic](sk.Public)
	if err != nil {
		return nil, fmt.Errorf("parsing sealed key public area: %w", err)
	}
	priv, err := tpm2.Unmarshal[tpm2.TPM2BPrivate](sk.Private)
	if err != nil {
		return nil, fmt.Errorf("parsing sealed key private area: %w", err)
	}

	srk, flushSRK, err := createSRK(tpm)
	if err != nil {
		return nil, err
	}
	defer flushSRK()

	loadCmd := tpm2.Load{
		ParentHandle: tpm2.AuthHandle{
			Handle: srk.handle,
			Name:   srk.name,
			Auth:   tpm2.PasswordAuth(nil),
		},
		InPrivate: *priv,
		InPublic:  *pub,
	}
	loadRsp, err := loadCmd.Execute(tpm)
	if err != nil {
		return nil, fmt.Errorf("loading sealed key from %q into TPM: %w", path, err)
	}
	defer func() {
		flushCmd := tpm2.FlushContext{FlushHandle: loadRsp.ObjectHandle}
		flushCmd.Execute(tpm)
	}()

	unsealCmd := tpm2.Unseal{
		ItemHandle: tpm2.AuthHandle{
			Handle: loadRsp.ObjectHandle,
			Name:   loadRsp.Name,

			// See the comment in [create] for more information on
			// the Auth parameter here; note that we're using the
			// [tpm2.EncryptOut] instead of [tpm2.EncryptIn]
			// parameter here, to protect the unsealed data coming
			// "out" of the TPM.
			Auth: tpm2.HMAC(
				tpm2.TPMAlgSHA256, 16,
				tpm2.AESEncryption(128, tpm2.EncryptOut),
				tpm2.Salted(srk.handle, srk.pub),
			),
		},
	}
	unsealRsp, err := unsealCmd.Execute(tpm)
	if err != nil {
		return nil, fmt.Errorf("unsealing key: %w", err)
	}
	return newAEAD(unsealRsp.OutData.Buffer)
}

// create generates a new key, seals it to the TPM, saves the sealed blob to
// path with mode 0600, and returns a [tink.AEAD] keyed by it.
func create(tpm transport.TPM, path string) (tink.AEAD, error) {
	// TODO: when runtime/secret is stabilized, we should wrap this
	// function so that it erases the raw key material from memory

	secret := make([]byte, keySize)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generating key: %w", err)
	}

	srk, flushSRK, err := createSRK(tpm)
	if err != nil {
		return nil, err
	}
	defer flushSRK()

	createCmd := tpm2.Create{
		ParentHandle: tpm2.AuthHandle{
			Handle: srk.handle,
			Name:   srk.name,

			// Create an authenticated HMAC session between us and
			// the TPM.
			//
			// We also use AESEncryption to encrypt the data we
			// send "in" to the TPM here, so that the raw secret
			// is not visible in-transit on the wire. This is cheap
			// insurance against e.g. someone sniffing the bus
			// between the CPU and TPM.
			//
			// Since the object has no password (so that the
			// service can start unattended), inject a "salt"
			// derived from the TPM's SRK public key; since only
			// the TPM has the private half of the SRK, an
			// interposer cannot decrypt the salt, and thus cannot
			// derive the session keys that protect the data
			// in-transit.
			//
			// Note that technically an active interposer can MITM
			// here by replacing the SRK's public key with an
			// attacker-controlled one. While that's *possible*,
			// we've chosen to ignore that threat model for now. If
			// necessary, we can verify the TPM's endorsement key.
			Auth: tpm2.HMAC(
				tpm2.TPMAlgSHA256, 16,
				tpm2.AESEncryption(128, tpm2.EncryptIn),
				tpm2.Salted(srk.handle, srk.pub),
			),
		},
		InSensitive: tpm2.TPM2BSensitiveCreate{
			Sensitive: &tpm2.TPMSSensitiveCreate{
				Data: tpm2.NewTPMUSensitiveCreate(&tpm2.TPM2BSensitiveData{
					// This is what we're actually sealing
					Buffer: secret,
				}),
			},
		},
		InPublic: tpm2.New2B(tpm2.TPMTPublic{
			Type:    tpm2.TPMAlgKeyedHash,
			NameAlg: tpm2.TPMAlgSHA256,
			ObjectAttributes: tpm2.TPMAObject{
				FixedTPM:     true, // Bind to this specific TPM
				FixedParent:  true, // Bind to this specific key in the TPM
				UserWithAuth: true, // Require a user session (note: we don't set a password)
				NoDA:         true, // No dictionary attack protection required
			},
		}),
	}
	createRsp, err := createCmd.Execute(tpm)
	if err != nil {
		return nil, fmt.Errorf("sealing key to TPM: %w", err)
	}

	out, err := json.Marshal(sealedKey{
		Version: sealedKeyVersion,
		Public:  tpm2.Marshal(createRsp.OutPublic),
		Private: tpm2.Marshal(createRsp.OutPrivate),
	})
	if err != nil {
		return nil, fmt.Errorf("serializing sealed key: %w", err)
	}
	if err := atomicfile.WriteFile(path, out, 0600); err != nil {
		return nil, fmt.Errorf("writing sealed key: %w", err)
	}
	return newAEAD(secret)
}

// srk describes a loaded storage root key.
type srk struct {
	handle tpm2.TPMHandle
	name   tpm2.TPM2BName
	pub    tpm2.TPMTPublic
}

// createSRK creates the standard TCG ECC-P256 storage root key in the TPM's
// owner hierarchy (a.k.a. storage hierarchy), and returns it along with a
// function that flushes it from the TPM when the caller is done with it. The
// SRK template is always the same, so every call yields the same key on a
// given TPM.
//
// For more information on the key types here, see the following:
//
//	https://ericchiang.github.io/post/tpm-keys/
func createSRK(tpm transport.TPM) (*srk, func(), error) {
	createCmd := tpm2.CreatePrimary{
		PrimaryHandle: tpm2.TPMRHOwner,
		InPublic:      tpm2.New2B(tpm2.ECCSRKTemplate),
	}
	rsp, err := createCmd.Execute(tpm)
	if err != nil {
		return nil, nil, fmt.Errorf("creating storage root key: %w", err)
	}
	flush := func() {
		flushCmd := tpm2.FlushContext{FlushHandle: rsp.ObjectHandle}
		flushCmd.Execute(tpm)
	}
	pub, err := rsp.OutPublic.Contents()
	if err != nil {
		flush()
		return nil, nil, fmt.Errorf("parsing storage root key public area: %w", err)
	}
	return &srk{handle: rsp.ObjectHandle, name: rsp.Name, pub: *pub}, flush, nil
}

// newAEAD returns a Tink AEAD keyed by the given secret.
func newAEAD(secret []byte) (tink.AEAD, error) {
	if len(secret) != keySize {
		return nil, fmt.Errorf("unsealed key has size %d, want %d", len(secret), keySize)
	}
	return subtle.NewXChaCha20Poly1305(secret)
}

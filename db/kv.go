// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package db

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"

	"github.com/tailscale/setec/types/api"
	"github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/keyset"
	"github.com/tink-crypto/tink-go/v2/tink"
	"tailscale.com/atomicfile"
)

// aeadContextDEK returns the AEAD encryption context to use for
// cryptographic operations on the Data Encryption Keyset (DEK).
func aeadContextDEK(version uint32) []byte {
	return []byte(fmt.Sprintf("setec DEK v%d", version))
}

// aeadContextDB returns the AEAD encryption context to use for
// cryptographic operations on the database.
func aeadContextDB(version uint32) []byte {
	return []byte(fmt.Sprintf("setec database v%d", version))
}

// databaseSchemaVersion is the (currently) only valid schema version
// for the on-disk database.
const databaseSchemaVersion = 1

// kv is an encrypted, transactional key/value store.
//
// On disk, the store is encoded as a JSON object with an unencrypted wrapper
// inside which the secrets are packaged as an AEAD encrypted blob:
//
//	{
//	   "Version": 1,
//	   "DEK": "<data-encryption-key-base64>",
//	   "DB": "<encrypted-secrets-base64>"
//	}
//
// The contents of "DB" prior to encryption are a JSON-encoded persist object,
// in which the keys are the secret names and the values are secret blobs:
//
//	{
//	  "Secrets": {
//	    "secret1": {
//	      "Versions": {
//	        "1": "<secret-1-value-base64>",
//	        "2": "<secret-2-value-base64>"
//	      },
//	      "ActiveVersion": "1",
//	      "LatestVersion": "2"
//	    },
//
//	    "secret2": {
//	      ...
//	    },
//	    ...
//	  }
//	}
type kv struct {
	path string

	secrets map[string]*secret

	dek       *keyset.Handle
	dekCipher tink.AEAD
	dekRaw    []byte

	kekCipher tink.AEAD

	gen uint64
}

// secret is a named secret, which may have multiple versioned secret
// bytes.
type secret struct {
	// Versions maps all currently known versions to the corresponding
	// values.
	//
	// We rely on api.SecretVersion being a type encoding/json will translate to
	// a JSON string (currently an integer).
	Versions map[api.SecretVersion]byteString
	// ActiveVersion is the secret version that gets returned to
	// clients who don't ask for a specific version of the secret.
	ActiveVersion api.SecretVersion
	// LatestVersion is the latest version that has already been used
	// by a previous Put.
	LatestVersion api.SecretVersion
}

// byteString is an alias for a string, but encodes to JSON as the conventional
// base64 encoding used for []byte. We do this since we expect secrets to have
// random binary content, and are storing them as strings for immutability.
type byteString string

func (b *byteString) UnmarshalText(text []byte) error {
	dec, err := base64.StdEncoding.DecodeString(string(text))
	if err != nil {
		return err
	}
	*b = byteString(dec)
	return nil
}

func (b byteString) MarshalText() ([]byte, error) {
	return []byte(base64.StdEncoding.EncodeToString([]byte(b))), nil
}

// persist is the portion of DB that is persisted to disk, before
// encryption.
type persist struct {
	// Secrets maps a secret name to associated data and metadata.
	Secrets map[string]*secret
}

// wrapped is the database as it is stored on disk.
type wrapped struct {
	// Version is the version of the database schema.
	Version uint32
	// DEK is the Data Encryption Key that should be used to decrypt
	// DB. The DEK is encrypted using a separate Key Encryption Key
	// (KEK), which the package's user has to provide.
	DEK []byte
	// DB is the database. It is a serialized persist struct encrypted
	// with the DEK.
	DB []byte
}

func openOrCreateKV(path string, kek tink.AEAD) (*kv, error) {
	bs, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return newKV(path, kek)
	} else if err != nil {
		return nil, err
	}

	var wrapped wrapped
	if err := json.Unmarshal(bs, &wrapped); err != nil {
		return nil, fmt.Errorf("loading encrypted database: %w", err)
	}

	if wrapped.Version != 1 {
		return nil, fmt.Errorf("unsupported database version %d", err)
	}

	reader := keyset.NewBinaryReader(bytes.NewReader(wrapped.DEK))
	dek, err := keyset.ReadWithAssociatedData(reader, kek, aeadContextDEK(wrapped.Version))
	if err != nil {
		return nil, fmt.Errorf("decrypting DEK: %w", err)
	}
	dekCipher, err := aead.New(dek)
	if err != nil {
		return nil, fmt.Errorf("constructing cipher from DEK: %w", err)
	}
	clear, err := dekCipher.Decrypt(wrapped.DB, aeadContextDB(wrapped.Version))
	if err != nil {
		return nil, fmt.Errorf("decrypting database: %w", err)
	}

	var persist persist
	if err := json.Unmarshal(clear, &persist); err != nil {
		return nil, fmt.Errorf("unmarshaling decrypted database: %w", err)
	}

	ret := &kv{
		path:      path,
		secrets:   persist.Secrets,
		dek:       dek,
		dekCipher: dekCipher,
		dekRaw:    wrapped.DEK,
		kekCipher: kek,
		// Initialize gen to 1, so that 0 can be used as a sentinel
		// value by calling code.
		gen: 1,
	}
	return ret, nil
}

// newKV creates a new empty KV store, and saves it to path using key.
func newKV(path string, key tink.AEAD) (*kv, error) {
	dek, err := keyset.NewHandle(aead.XChaCha20Poly1305KeyTemplate())
	if err != nil {
		return nil, fmt.Errorf("generating database keyset: %w", err)
	}
	dekCipher, err := aead.New(dek)
	if err != nil {
		return nil, fmt.Errorf("constructing cipher from DEK: %w", err)
	}
	var encryptedDEK bytes.Buffer
	writer := keyset.NewBinaryWriter(&encryptedDEK)
	if err := dek.WriteWithAssociatedData(writer, key, aeadContextDEK(databaseSchemaVersion)); err != nil {
		return nil, fmt.Errorf("encrypting DEK: %w", err)
	}

	ret := &kv{
		path:      path,
		secrets:   map[string]*secret{},
		dek:       dek,
		dekCipher: dekCipher,
		dekRaw:    encryptedDEK.Bytes(),
		kekCipher: key,
	}
	if err := ret.save(); err != nil {
		return nil, fmt.Errorf("creating database: %w", err)
	}
	return ret, nil
}

// save encrypts and writes the kv to kv.path. If save return an
// error, the file at kv.path is unchanged.
func (kv *kv) save() (err error) {
	defer func() {
		if err == nil {
			kv.gen++
		}
	}()

	clearDB, err := json.Marshal(persist{
		Secrets: kv.secrets,
	})
	if err != nil {
		return err
	}
	encryptedDB, err := kv.dekCipher.Encrypt(clearDB, aeadContextDB(databaseSchemaVersion))
	if err != nil {
		return fmt.Errorf("encrypting database: %w", err)
	}
	out, err := json.Marshal(wrapped{
		Version: databaseSchemaVersion,
		DEK:     kv.dekRaw,
		DB:      encryptedDB,
	})
	if err != nil {
		return fmt.Errorf("serializing encrypted database: %w", err)
	}
	if err := atomicfile.WriteFile(kv.path, out, 0600); err != nil {
		return fmt.Errorf("writing database to %q: %w", kv.path, err)
	}
	return nil
}

// filePath returns the path to the database file on disk.
func (kv *kv) filePath() string {
	return kv.path
}

// writeGen returns a process-local "write generation" for the kv
// store. The write generation increments whenever a change is saved
// to disk, and can be used as a coarse change detection mechanism.
func (kv *kv) writeGen() uint64 {
	return kv.gen
}

// list returns a list of all secret names in kv.
func (kv *kv) list() []string {
	return slices.Sorted(maps.Keys(kv.secrets))
}

// info returns metadata about a secret.
func (kv *kv) info(name string) (*api.SecretInfo, error) {
	secret := kv.secrets[name]
	if secret == nil {
		return nil, ErrNotFound
	}
	info := &api.SecretInfo{
		Name:          name,
		ActiveVersion: secret.ActiveVersion,
	}
	for v := range secret.Versions {
		info.Versions = append(info.Versions, v)
	}
	slices.Sort(info.Versions)
	return info, nil
}

// get returns a secret's active value.
func (kv *kv) get(name string) (*api.SecretValue, error) {
	secret := kv.secrets[name]
	if secret == nil {
		return nil, ErrNotFound
	}
	bs, ok := secret.Versions[secret.ActiveVersion]
	if !ok {
		return nil, errors.New("[unexpected] active secret version missing from DB")
	}
	return &api.SecretValue{
		Value:   []byte(bs),
		Version: secret.ActiveVersion,
	}, nil
}

// getVersion returns a secret's value at a specific version.
func (kv *kv) getVersion(name string, version api.SecretVersion) (*api.SecretValue, error) {
	secret := kv.secrets[name]
	if secret == nil {
		return nil, ErrNotFound
	}
	bs, ok := secret.Versions[version]
	if !ok {
		return nil, ErrNotFound
	}
	return &api.SecretValue{
		Value:   []byte(bs),
		Version: version,
	}, nil
}

// put writes value to the secret called name. If the secret already
// exists, value is saved as a new inactive version. Otherwise, value
// is saved as the initial version of the secret and immediately set
// active. On success, returns the secret version for the new value.
func (kv *kv) put(name string, value []byte) (api.SecretVersion, error) {
	s := kv.secrets[name]
	if s == nil {
		kv.secrets[name] = &secret{
			LatestVersion: 1,
			ActiveVersion: 1,
			Versions: map[api.SecretVersion]byteString{
				1: byteString(value),
			},
		}
		if err := kv.save(); err != nil {
			delete(kv.secrets, name)
			return 0, err
		}
		return 1, nil
	}

	// If the new value is the same as the current latest version, don't store a
	// new copy.
	bsValue := byteString(value)
	if s.Versions[s.LatestVersion] == bsValue {
		return s.LatestVersion, nil
	}

	s.LatestVersion++
	s.Versions[s.LatestVersion] = bsValue
	if err := kv.save(); err != nil {
		delete(s.Versions, s.LatestVersion)
		s.LatestVersion--
		return 0, err
	}
	return s.LatestVersion, nil
}

// setActive changes the active version of the secret called name to
// version.
func (kv *kv) setActive(name string, version api.SecretVersion) error {
	if version == api.SecretVersionDefault {
		return errors.New("invalid version")
	}
	secret := kv.secrets[name]
	if secret == nil {
		return ErrNotFound
	}
	if _, ok := secret.Versions[version]; !ok {
		return ErrNotFound
	}
	if secret.ActiveVersion == version {
		return nil
	}
	old := secret.ActiveVersion
	secret.ActiveVersion = version
	if err := kv.save(); err != nil {
		secret.ActiveVersion = old
		return err
	}
	return nil
}

// deleteVersion deletes the specified version of a secret.
func (kv *kv) deleteVersion(name string, version api.SecretVersion) error {
	if version == api.SecretVersionDefault {
		return errors.New("invalid version")
	}
	secret := kv.secrets[name]
	if secret == nil {
		return fmt.Errorf("secret %q: %w", name, ErrNotFound)
	} else if version == secret.ActiveVersion {
		return errors.New("cannot delete active version")
	}
	old, ok := secret.Versions[version]
	if !ok {
		return fmt.Errorf("version %v: %w", version, ErrNotFound)
	}
	delete(secret.Versions, version)
	if err := kv.save(); err != nil {
		secret.Versions[version] = old
		return err
	}
	return nil
}

// deleteSecret deletes all versions of a secret.
func (kv *kv) deleteSecret(name string) error {
	secret := kv.secrets[name]
	if secret == nil {
		return nil // the secret (already) has no version
	}
	delete(kv.secrets, name)
	if err := kv.save(); err != nil {
		kv.secrets[name] = secret
		return err
	}
	return nil
}

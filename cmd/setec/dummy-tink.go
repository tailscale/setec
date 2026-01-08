// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
////////////////////////////////////////////////////////////////////////////////

// This file is a copy of tink-go's https://github.com/tink-crypto/tink-go/blob/v2.1.0/testutil/testutil.go#L90-L130
// with its type name unexported.
//
// We do this to avoid https://github.com/tink-crypto/tink-go/issues/31 happening
// in tink-go's testutil init function, which breaks our assumptions in CI
// that things don't hit the network.

package main

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
)

// dummyAEAD is a dummy implementation of AEAD interface. It "encrypts" data
// with a simple serialization capturing the dummy name, plaintext, and
// associated data, and "decrypts" it by reversing this and checking that the
// name and associated data match.
type dummyAEAD struct {
	Name string
}

type dummyAEADData struct {
	Name           string
	Plaintext      []byte
	AssociatedData []byte
}

// Encrypt encrypts the plaintext.
func (a *dummyAEAD) Encrypt(plaintext []byte, associatedData []byte) ([]byte, error) {
	buf := new(bytes.Buffer)
	encoder := gob.NewEncoder(buf)
	err := encoder.Encode(dummyAEADData{
		Name:           a.Name,
		Plaintext:      plaintext,
		AssociatedData: associatedData,
	})
	if err != nil {
		return nil, fmt.Errorf("dummy aead encrypt: %v", err)
	}
	return buf.Bytes(), nil
}

// Decrypt decrypts the ciphertext.
func (a *dummyAEAD) Decrypt(ciphertext []byte, associatedData []byte) ([]byte, error) {
	data := dummyAEADData{}
	decoder := gob.NewDecoder(bytes.NewBuffer(ciphertext))
	if err := decoder.Decode(&data); err != nil {
		return nil, fmt.Errorf("dummy aead decrypt: invalid data: %v", err)
	}
	if data.Name != a.Name || !bytes.Equal(data.AssociatedData, associatedData) {
		return nil, errors.New("dummy aead encrypt: name/associated data mismatch")
	}
	return data.Plaintext, nil
}

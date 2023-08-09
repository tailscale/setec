// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package setec

import (
	"os"

	"tailscale.com/atomicfile"
)

// Cache is an interface that lets a Store persist one piece of data
// locally. The persistence need not be perfect (i.e., it's okay to lose
// previously written data).
type Cache interface {
	// Write persists the given bytes for future retrieval.
	Write([]byte) error

	// Read returns previously persisted bytes, if any are available.  If the
	// cache is empty, Read must report an empty slice or nil without error.
	Read() ([]byte, error)
}

// MemCache is a trivial implementation of the Cache interface that stores a
// value in a byte slice. This is intended for use in testing.  The methods of
// a MemCache never report an error.
type MemCache struct{ data []byte }

func (m *MemCache) Write(data []byte) error { m.data = data; return nil }

func (m *MemCache) Read() ([]byte, error) { return m.data, nil }

func (m *MemCache) String() string { return string(m.data) }

// FileCache is an implementation of the Cache interface that stores a value in
// a file at the specified path.
type FileCache string

func (f FileCache) Write(data []byte) error {
	return atomicfile.WriteFile(string(f), data, 0600)
}

func (f FileCache) Read() ([]byte, error) { return os.ReadFile(string(f)) }

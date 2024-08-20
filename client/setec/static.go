// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package setec

import (
	"bytes"
	"fmt"
	"os"
)

// StaticSecret returns a Secret that vends a static string value.
// This is useful as a placeholder for development, migration, and testing.
// The value reported by a static secret never changes.
func StaticSecret(value string) Secret {
	return func() []byte { return []byte(value) }
}

// StaticWatcher returns a Watcher that vends a static string value.
// This is useful as a placeholder for development, migration, and testing.
// The value reported by a static watcher never changes, and the watcher
// channel is never ready.
func StaticWatcher(value string) Watcher {
	return Watcher{secret: StaticSecret(value)}
}

// StaticFile returns a Secret that vends the contents of path.  The contents
// of the file are returned exactly as stored.
//
// This is useful as a placeholder for development, migration, and testing.
// The value reported by this secret is the contents of path at the
// time this function is called, and never changes.
func StaticFile(path string) (Secret, error) {
	bs, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading static secret: %w", err)
	}
	return func() []byte { return bs }, nil
}

// StaticTextFile returns a secret that vends the contents of path, which are
// treated as text with leading and trailing whitespace trimmed.
//
// This is useful as a placeholder for development, migration, and testing.
// The value reported by a static secret never changes.
func StaticTextFile(path string) (Secret, error) {
	bs, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading static secret: %w", err)
	}
	text := bytes.TrimSpace(bs)
	return func() []byte { return text }, nil
}

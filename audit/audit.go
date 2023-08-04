// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// Package audit provides an audit log writer for access to secrets.
package audit

import (
	"encoding/json"
	"io"
	"math/rand"
	"net/netip"
	"os"
	"time"

	"github.com/tailscale/setec/acl"
	"github.com/tailscale/setec/types/api"
)

// Principal is the identity of a client taking action on the secrets
// service.
type Principal struct {
	// Hostname is the principal's Tailscale FQDN.
	Hostname string `json:"hostname"`
	// IP is one of the principal's Tailscale IPs that correspond to
	// Hostname. The specific IP here depends on the builder of an
	// instance of Principal, but is usually the IP from which a
	// request was received.
	IP netip.Addr `json:"ip"`
	// User is the human identity of the principal, or the empty
	// string if the principal is a tagged device.
	User string `json:"user,omitempty"`
	// Tags is the tags of the principal, or nil if the principal is
	// not a tagged device.
	Tags []string `json:"tags,omitempty"`
}

// Entry is an audit log entry.
type Entry struct {
	// ID is the entry's ID.
	ID uint64 `json:"id"`
	// Time is the entry's timestamp.
	Time time.Time `json:"time"`
	// Principal is the client who is doing something.
	Principal Principal `json:"principal"`
	// Action is the action being performed on a secret.
	Action acl.Action `json:"action"`

	// The fields above are set for all audit entries. The fields
	// below are only set for certain Actions.

	// Secret is the name of the secret being acted upon.
	Secret string `json:"secret"`
	// SecretVersion is the version of the secret being acted upon, if
	// applicable, or api.SecretVersionDefault if a version doesn't
	// make sense for the action (e.g. listing a secret).
	SecretVersion api.SecretVersion `json:"secretVersion,omitempty"`
}

// Writer is an audit log writer.
type Writer struct {
	w   io.Writer
	enc *json.Encoder
}

// New returns a Writer that outputs audit log entries to w as JSON
// objects. If w also implements io.Closer, Writer.Close closes w. If
// w also implements a Sync method with the same signature as os.File,
// Writer.Sync calls w.Sync.
func New(w io.Writer) *Writer {
	return &Writer{
		w:   w,
		enc: json.NewEncoder(w),
	}
}

// NewFile returns a Writer that outputs audit log entries to a file
// at path, creating it if necessary.
func NewFile(path string) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	return New(f), nil
}

// Sync commits the current contents of the file to stable storage if
// the Writer was created with a sink that itself implements Sync, or
// else does nothing successfully.
func (l *Writer) Sync() error {
	if s, ok := l.w.(syncer); ok {
		return s.Sync()
	}
	return nil
}

// Close closes the Writer if the writer was created with a sink that
// implements io.Closer, or else does nothing successfully.
func (l *Writer) Close() error {
	if err := l.Sync(); err != nil {
		return err
	}
	if c, ok := l.w.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

type syncer interface {
	Sync() error
}

// WriteEntries writes entries to the audit log. Each entry's ID and
// Time fields are set prior to writing, any existing value is
// overwritten.
func (l *Writer) WriteEntries(entries ...*Entry) error {
	for _, e := range entries {
		e.ID = rand.Uint64()
		e.Time = time.Now().UTC()

		if err := l.enc.Encode(e); err != nil {
			return err
		}
	}
	return l.Sync()
}

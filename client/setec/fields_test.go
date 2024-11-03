// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package setec_test

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/tailscale/setec/client/setec"
	"github.com/tailscale/setec/setectest"
	"tailscale.com/types/logger"
)

// testObj is a value that can be unmarshaled from JSON.
type testObj struct {
	X string `json:"x"`
	Y bool   `json:"y"`
}

// binValue is a value that can be unmarshaled from binary.
type binValue [2]string

func (t *binValue) UnmarshalBinary(text []byte) error {
	head, tail, ok := bytes.Cut(text, []byte(":"))
	if !ok {
		return errors.New("missing :")
	}
	t[0], t[1] = string(head), string(tail)
	return nil
}

func TestFields(t *testing.T) {
	// Populate a secret service with placeholder values for tagged secrets.
	secrets := map[string]string{
		"apple":  "1",
		"pear":   "2",
		"plum":   "3",
		"cherry": "4",

		// A text-encoded value compatible with binValue.
		"bin-value":     "kumquat:quince",
		"bin-value-ptr": "peach:durian",

		// A JSON-encoded value compatible with testObj.
		"object-value": `{"x":"hello","y":true}`,

		// A JSON-encoded value compatible with an int64.
		"int-value": `12345`,
	}
	db := setectest.NewDB(t, nil)
	for name, val := range secrets {
		db.MustPut(db.Superuser, "test/"+name, val)
	}

	ss := setectest.NewServer(t, db, nil)
	hs := httptest.NewServer(ss.Mux)
	defer hs.Close()

	// Verify that if we parse secrets with a store enabled, we correctly plumb
	// the values from the service into the tagged fields.
	type testTarget struct {
		A  string       `setec:"apple"`
		B  binValue     `setec:"bin-value"`
		BP *binValue    `setec:"bin-value-ptr"`
		P  []byte       `setec:"pear"`
		L  setec.Secret `setec:"plum"`
		X  string       // untagged, not affected
		J  testObj      `setec:"object-value,json"`
		Z  int          `setec:"int-value,json"`
	}
	var obj testTarget

	f, err := setec.ParseFields(&obj, "test")
	if err != nil {
		t.Fatalf("ParseFields: unexpected error: %v", err)
	}

	st, err := setec.NewStore(context.Background(), setec.StoreConfig{
		Client: setec.Client{Server: hs.URL, DoHTTP: hs.Client().Do},
		Structs: []setec.Struct{
			{Value: &obj, Prefix: "test"},
		},
		Logf: logger.Discard,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer st.Close()

	// Check that secret names respect prefixing.
	if diff := cmp.Diff(f.Secrets(), []string{
		"test/apple", "test/bin-value", "test/bin-value-ptr",
		"test/pear", "test/plum",
		"test/object-value", "test/int-value",
	}); diff != "" {
		t.Errorf("Prefixed secret names (-got, +want):\n%s", diff)
	}

	// Check that we can apply values.
	if err := f.Apply(context.Background(), st); err != nil {
		t.Errorf("Apply failed; %v", err)
	}

	// Don't try to compare complex plumbing; see below.
	opt := cmpopts.IgnoreFields(testTarget{}, "L")
	if diff := cmp.Diff(obj, testTarget{
		A:  secrets["apple"],
		B:  binValue{"kumquat", "quince"},
		BP: &binValue{"peach", "durian"},
		P:  []byte(secrets["pear"]),
		J:  testObj{X: "hello", Y: true},
		Z:  12345,
	}, opt); diff != "" {
		t.Errorf("Populated value (-got, +want):\n%s", diff)
	}

	// Check the handle-plumbed fields.
	if got, want := string(obj.L.Get()), secrets["plum"]; got != want {
		t.Errorf("Secret field: got %q, want %q", got, want)
	}
}

func TestParseErrors(t *testing.T) {
	checkFail := func(input any, wantErr string) func(t *testing.T) {
		return func(t *testing.T) {
			f, err := setec.ParseFields(input, "")
			if err == nil {
				t.Fatalf("Parse: got %v, want error", f)
			} else if !strings.Contains(err.Error(), wantErr) {
				t.Fatalf("Parse: got error %v, want %q", err, wantErr)
			}
		}
	}
	t.Run("NonPointer", checkFail(struct{ X string }{}, "not a pointer"))
	t.Run("NonStruct", checkFail(new(string), "not a pointer to a struct"))
	t.Run("InvalidType", checkFail(&struct {
		X float64 `setec:"x"` // N.B. not marked with JSON
	}{}, "unsupported type"))

	t.Run("NoTaggedFields", func(t *testing.T) {
		_, err := setec.ParseFields(&struct{ X string }{}, "")
		if !errors.Is(err, setec.ErrNoFields) {
			t.Fatalf("ParseFields: got %v, want %v", err, setec.ErrNoFields)
		}
	})
}

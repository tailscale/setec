// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package setec_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/tailscale/setec/client/setec"
	"github.com/tailscale/setec/setectest"
	"tailscale.com/types/logger"
)

type testObj struct {
	X string `json:"x"`
	Y bool   `json:"y"`
}

func TestFields(t *testing.T) {
	// Populate a secret service with placeholder values for tagged secrets.
	secrets := map[string]string{
		"apple":  "1",
		"pear":   "2",
		"plum":   "3",
		"cherry": "4",

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
		A string        `setec:"apple"`
		P []byte        `setec:"pear"`
		L setec.Secret  `setec:"plum"`
		C setec.Watcher `setec:"cherry"`
		X string        // untagged, not affected
		J testObj       `setec:"object-value,json"`
		Z int           `setec:"int-value,json"`
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
		"test/apple", "test/pear", "test/plum", "test/cherry",
		"test/object-value", "test/int-value",
	}); diff != "" {
		t.Errorf("Prefixed secret names (-got, +want):\n%s", diff)
	}

	// Check that we can apply values.
	if err := f.Apply(context.Background(), st); err != nil {
		t.Errorf("Apply failed; %v", err)
	}

	// Don't try to compare complex plumbing; see below.
	opt := cmpopts.IgnoreFields(testTarget{}, "L", "C")
	if diff := cmp.Diff(obj, testTarget{
		A: secrets["apple"],
		P: []byte(secrets["pear"]),
		J: testObj{X: "hello", Y: true},
		Z: 12345,
	}, opt); diff != "" {
		t.Errorf("Populated value (-got, +want):\n%s", diff)
	}

	// Check the handle-plumbed fields.
	if got, want := string(obj.L.Get()), secrets["plum"]; got != want {
		t.Errorf("Secret field: got %q, want %q", got, want)
	}
	if got, want := string(obj.C.Get()), secrets["cherry"]; got != want {
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
	t.Run("NoTaggedFields", checkFail(&struct{ X string }{}, "no setec-tagged fields"))
	t.Run("InvalidType", checkFail(&struct {
		X float64 `setec:"x"` // N.B. not marked with JSON
	}{}, "unsupported type"))
}

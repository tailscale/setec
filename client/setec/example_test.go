// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package setec_test

import (
	"context"
	"flag"
	"log"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tailscale/setec/client/setec"
	"github.com/tailscale/setec/setectest"
	"tailscale.com/types/logger"
)

var runEnv = flag.String("env", "dev", "Runtime environment (dev or prod)")

func TestExample(t *testing.T) {
	// Set up plumbing for the example. In real usage, the client will typically
	// communicate with a server running on another host.
	d := setectest.NewDB(t, nil)
	d.MustPut(d.Superuser, "dev/alpha", "dev-alpha")
	d.MustPut(d.Superuser, "prod/alpha", "prod-alpha")
	d.MustPut(d.Superuser, "dev/bravo", "dev-bravo")
	d.MustPut(d.Superuser, "prod/bravo", "prod-bravo")
	d.MustPut(d.Superuser, "dev/charlie", "dev-charlie")
	d.MustPut(d.Superuser, "prod/charlie", "prod-charlie")
	d.MustPut(d.Superuser, "dev/delta", `"2023-10-06T12:34:56Z"`)
	d.MustPut(d.Superuser, "prod/delta", `"2023-10-05T01:23:45Z"`)

	ts := setectest.NewServer(t, d, nil)
	hs := httptest.NewServer(ts.Mux)
	defer hs.Close()

	// Example begins here:
	setecClient := setec.Client{Server: hs.URL, DoHTTP: hs.Client().Do}

	// Set up a struct type with fields to carry the secrets you care about.
	var secrets struct {
		Alpha   []byte       `setec:"alpha"`
		Bravo   string       `setec:"bravo"`
		Charlie setec.Secret `setec:"charlie"`
		Delta   time.Time    `setec:"delta,json"`
		Other   int          // this field is not touched by setec
	}
	secrets.Other = 25

	// Create a setec.Store to track those secrets.  Use the --env flag to
	// select which set of secrets will be populated ("dev" or "prod").
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, err := setec.NewStore(ctx, setec.StoreConfig{
		Client:  setecClient,
		Structs: []setec.Struct{{Value: &secrets, Prefix: *runEnv}},
		Logf:    logger.Discard,
	})
	if err != nil {
		log.Fatalf("NewStore: %v", err)
	}
	defer st.Close()

	// At this point the field values have been populated.
	t.Logf("Example values:\nalpha: %q\nbravo: %q\ncharlie: %q\ndelta: %v\nother: %d\n",
		secrets.Alpha, secrets.Bravo, secrets.Charlie.Get(), secrets.Delta, secrets.Other)
}

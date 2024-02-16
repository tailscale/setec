//go:build go_mod_tidy_deps

// Package tools is a pseudo-package for tracking Go tool dependencies
// that are not needed for build or test.
package tools

import (
	// Used by CI.
	_ "honnef.co/go/tools/cmd/staticcheck"
)

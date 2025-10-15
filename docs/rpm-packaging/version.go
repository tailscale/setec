// Package version provides version information for leger
// Version info is embedded at build time via ldflags
// Based on Tailscale's version stamping approach
package version

import (
	"fmt"
	"runtime"
)

var (
	// Version is the semantic version (e.g., "v1.2.3" or "v1.2.3-dirty")
	// Set via ldflags: -X github.com/yourname/leger/version.Version=...
	Version = "development"

	// Commit is the git commit hash
	// Set via ldflags: -X github.com/yourname/leger/version.Commit=...
	Commit = "unknown"

	// BuildDate is the RFC3339 timestamp of when the binary was built
	// Set via ldflags: -X github.com/yourname/leger/version.BuildDate=...
	BuildDate = "unknown"
)

// Short returns just the version string
func Short() string {
	return Version
}

// Long returns a detailed version string including commit and build date
func Long() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, BuildDate)
}

// Full returns a complete version string with all available information
func Full() string {
	return fmt.Sprintf(
		"leger %s\nCommit:     %s\nBuild Date: %s\nGo Version: %s\nOS/Arch:    %s/%s",
		Version,
		Commit,
		BuildDate,
		runtime.Version(),
		runtime.GOOS,
		runtime.GOARCH,
	)
}

// Info returns structured version information
type Info struct {
	Version   string
	Commit    string
	BuildDate string
	GoVersion string
	OS        string
	Arch      string
}

// Get returns structured version information
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}

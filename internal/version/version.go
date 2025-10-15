package version

import "fmt"

var (
	// Set via ldflags during build
	Version   = "development"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func String() string {
	return Version
}

func Long() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, BuildDate)
}

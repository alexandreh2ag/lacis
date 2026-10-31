// Package version holds the build information injected with -ldflags.
package version

import "fmt"

var (
	// Version is the release version, set at build time.
	Version = "dev"
	// Commit is the git commit the binary was built from, set at build time.
	Commit = "none"
)

// GetFormattedVersion returns the version and commit on one line.
func GetFormattedVersion() string {
	return fmt.Sprintf("%s (commit: %s)", Version, Commit)
}

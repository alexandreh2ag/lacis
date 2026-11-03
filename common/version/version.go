// Package version holds the build information injected with -ldflags.
package version

import "fmt"

var (
	// Version is the release version, set at build time.
	Version = "dev"
	// Commit is the git commit the binary was built from, set at build time.
	Commit = "none"
	// Date is when the binary was built (RFC 3339), set at build time.
	Date = "unknown"
)

// GetFormattedVersion returns the version, commit and build date on one line.
func GetFormattedVersion() string {
	return fmt.Sprintf("%s (commit: %s, built: %s)", Version, Commit, Date)
}

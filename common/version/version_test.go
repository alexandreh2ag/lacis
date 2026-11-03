package version

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetFormattedVersion(t *testing.T) {
	Version, Commit, Date = "1.2.3", "abc123", "2026-11-01T10:00:00Z"
	t.Cleanup(func() { Version, Commit, Date = "dev", "none", "unknown" })

	assert.Equal(t, "1.2.3 (commit: abc123, built: 2026-11-01T10:00:00Z)", GetFormattedVersion())
}

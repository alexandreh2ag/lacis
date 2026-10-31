package version

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetFormattedVersion(t *testing.T) {
	Version, Commit = "1.2.3", "abc123"
	t.Cleanup(func() { Version, Commit = "dev", "none" })

	assert.Equal(t, "1.2.3 (commit: abc123)", GetFormattedVersion())
}

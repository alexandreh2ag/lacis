package system

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSameOwner(t *testing.T) {
	tests := []struct {
		name        string
		linkOwner   int // -1: unknown
		targetOwner int
		want        bool
	}{
		{name: "same owner", linkOwner: 1000, targetOwner: 1000, want: true},
		{name: "other owner", linkOwner: 1000, targetOwner: 0},
		{name: "link owner unknown", linkOwner: -1, targetOwner: 1000},
		{name: "target owner unknown", linkOwner: 1000, targetOwner: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sys, f, _ := newSystem(t, FamilyDebian, map[string]string{"/builds/app/composer.json": "{}"})
			f.Link("/var/www/app", "/builds/app")
			if tt.linkOwner >= 0 {
				f.Own("/var/www/app", uint32(tt.linkOwner))
			}
			if tt.targetOwner >= 0 {
				f.Own("/builds/app", uint32(tt.targetOwner))
			}
			assert.Equal(t, tt.want, sys.SameOwner("/var/www/app", "/builds/app"))
		})
	}

	sys, _, _ := newSystem(t, FamilyDebian, nil)
	assert.False(t, sys.SameOwner("/missing", "/builds/app"), "a missing link")

	// On the real file system, the test user owns both.
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "target"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(dir, "target"), filepath.Join(dir, "link")))
	sys.ctx.Fs = afero.NewOsFs()
	assert.True(t, sys.SameOwner(filepath.Join(dir, "link"), filepath.Join(dir, "target")))
	assert.False(t, sys.SameOwner(filepath.Join(dir, "link"), filepath.Join(dir, "missing")), "a missing target")

	// A file system that cannot stat a link itself tells no owner.
	sys.ctx.Fs = &afero.BasePathFs{}
	assert.False(t, sys.SameOwner("/a", "/b"))
}

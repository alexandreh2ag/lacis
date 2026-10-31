package fakefs

import (
	"io/fs"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFS(t *testing.T) {
	f := New()
	f.Files(t, map[string]string{"/proc/1/cmdline": "init", "/etc/hostname": "h"})
	f.Link("/proc/1/exe", "/usr/lib/systemd/systemd")

	b, err := afero.ReadFile(f, "/proc/1/cmdline")
	require.NoError(t, err)
	assert.Equal(t, "init", string(b))
	target, err := f.ReadlinkIfPossible("/proc/1/exe")
	require.NoError(t, err)
	assert.Equal(t, "/usr/lib/systemd/systemd", target)
	_, err = f.ReadlinkIfPossible("/proc/2/exe")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

package fakefs

import (
	"io/fs"
	"os"
	"syscall"
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

func TestFS_OpenFileNoFollow(t *testing.T) {
	f := New()
	f.Link("/app/.env", "/dev/zero")
	_, err := f.OpenFile("/app/.env", os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	assert.ErrorIs(t, err, syscall.ELOOP)
	_, err = f.OpenFile("/app/.env", os.O_RDONLY, 0)
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestFS_Owners(t *testing.T) {
	f := New()
	f.Files(t, map[string]string{"/builds/app/composer.json": "{}", "/etc/hostname": "h"})
	f.Link("/var/www/app", "/builds/app")
	f.Own("/var/www/app", 1000)
	f.Own("/builds/app", 33)

	fi, isLstat, err := f.LstatIfPossible("/var/www/app")
	require.NoError(t, err)
	assert.True(t, isLstat)
	assert.Equal(t, "app", fi.Name())
	assert.True(t, fi.Mode()&os.ModeSymlink != 0)
	assert.False(t, fi.IsDir())
	assert.Equal(t, uint32(1000), fi.Sys().(*syscall.Stat_t).Uid)

	fi, err = f.Stat("/builds/app")
	require.NoError(t, err)
	assert.True(t, fi.IsDir())
	assert.Equal(t, uint32(33), fi.Sys().(*syscall.Stat_t).Uid)
	fi, _, err = f.LstatIfPossible("/builds/app")
	require.NoError(t, err)
	assert.Equal(t, uint32(33), fi.Sys().(*syscall.Stat_t).Uid)

	// A path nobody owns tells no owner.
	fi, err = f.Stat("/etc/hostname")
	require.NoError(t, err)
	_, owned := fi.Sys().(*syscall.Stat_t)
	assert.False(t, owned)
	_, err = f.Stat("/missing")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

package fsutil

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/agent/fakefs"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadRegular_MemFs(t *testing.T) {
	f := fakefs.New()
	f.Write(t, "/app/.env", "A=1\n")
	f.Write(t, "/app/big", "0123456789")
	require.NoError(t, f.MkdirAll("/app/dir", 0o755))
	f.Link("/app/link", "/app/.env")

	tests := []struct {
		name    string
		path    string
		follow  bool
		want    string
		wantErr error
	}{
		{name: "regular", path: "/app/.env", want: "A=1\n"},
		{name: "follow regular", path: "/app/.env", follow: true, want: "A=1\n"},
		{name: "too large", path: "/app/big", wantErr: ErrTooLarge},
		{name: "directory", path: "/app/dir", wantErr: ErrNotRegular},
		{name: "link", path: "/app/link", wantErr: syscall.ELOOP},
		{name: "missing", path: "/app/none", wantErr: fs.ErrNotExist},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			read := ReadRegular
			if tt.follow {
				read = ReadRegularFollow
			}
			got, err := read(f, tt.path, 5)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
}

// TestReadRegular_Os checks the kernel side: a FIFO does not block, a link
// to /dev/zero is refused or, followed, is not a regular file.
func TestReadRegular_Os(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	link := filepath.Join(dir, "zero")
	require.NoError(t, os.Symlink("/dev/zero", link))
	file := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))
	fileLink := filepath.Join(dir, "file-link")
	require.NoError(t, os.Symlink(file, fileLink))
	osFs := afero.NewOsFs()

	done := make(chan error, 1)
	go func() { _, err := ReadRegular(osFs, fifo, 10); done <- err }()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, ErrNotRegular)
	case <-time.After(5 * time.Second):
		t.Fatal("reading a FIFO blocks")
	}

	_, err := ReadRegular(osFs, link, 10)
	assert.ErrorIs(t, err, syscall.ELOOP)
	_, err = ReadRegularFollow(osFs, link, 10)
	assert.ErrorIs(t, err, ErrNotRegular)
	_, err = ReadRegular(osFs, fileLink, 10)
	assert.ErrorIs(t, err, syscall.ELOOP)
	b, err := ReadRegularFollow(osFs, fileLink, 10)
	require.NoError(t, err)
	assert.Equal(t, "x", string(b))
}

func TestExitCode(t *testing.T) {
	for _, err := range []error{fs.ErrNotExist, fs.ErrPermission, ErrNotRegular, ErrTooLarge, errors.New("boom"), nil} {
		code := ExitCode(err)
		got := ErrorOf(code, "f")
		switch {
		case err == nil:
			assert.NoError(t, got)
		case code == ExitFailure:
			assert.EqualError(t, got, "f")
		default:
			assert.ErrorIs(t, got, err)
		}
	}
	assert.Equal(t, ExitPermission, ExitCode(&fs.PathError{Err: syscall.EACCES}))
}

func TestReadRegularLinks_MemFs(t *testing.T) {
	f := fakefs.New()
	f.Write(t, "/srv/app/shared/.env", "A=1\n")
	f.Write(t, "/srv/app/big", "0123456789")
	require.NoError(t, f.MkdirAll("/srv/app/dir", 0o755))
	// Capistrano: releases/X/.env -> ../../shared/.env
	f.Link("/srv/app/releases/1/.env", "../../shared/.env")
	f.Link("/srv/app/abs", "/srv/app/shared/.env")
	f.Link("/srv/app/loop-a", "loop-b")
	f.Link("/srv/app/loop-b", "./loop-a")
	f.Link("/srv/app/self", "self")
	for i := range 6 { // l0 -> l1 -> … -> l6 -> shared/.env: 7 hops
		f.Link(fmt.Sprintf("/srv/app/l%d", i), fmt.Sprintf("l%d", i+1))
	}
	f.Link("/srv/app/l6", "shared/.env")
	f.Link("/srv/app/dangling", "none")
	f.Link("/srv/app/to-dir", "dir")
	f.Link("/srv/app/to-big", "big")

	tests := []struct {
		name    string
		path    string
		hops    int
		want    string
		wantErr error
	}{
		{name: "regular", path: "/srv/app/shared/.env", hops: MaxLinkHops, want: "A=1\n"},
		{name: "capistrano relative link", path: "/srv/app/releases/1/.env", hops: MaxLinkHops, want: "A=1\n"},
		{name: "absolute link", path: "/srv/app/abs", hops: MaxLinkHops, want: "A=1\n"},
		{name: "loop", path: "/srv/app/loop-a", hops: MaxLinkHops, wantErr: ErrLinkLoop},
		{name: "self loop", path: "/srv/app/self", hops: MaxLinkHops, wantErr: ErrLinkLoop},
		{name: "chain over the max", path: "/srv/app/l0", hops: MaxLinkHops, wantErr: ErrTooManyLinks},
		{name: "chain within a larger max", path: "/srv/app/l0", hops: 7, want: "A=1\n"},
		{name: "no hop allowed", path: "/srv/app/abs", hops: 0, wantErr: ErrTooManyLinks},
		{name: "dangling", path: "/srv/app/dangling", hops: MaxLinkHops, wantErr: fs.ErrNotExist},
		{name: "to a directory", path: "/srv/app/to-dir", hops: MaxLinkHops, wantErr: ErrNotRegular},
		{name: "to a large file", path: "/srv/app/to-big", hops: MaxLinkHops, wantErr: ErrTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadRegularLinks(f, tt.path, 5, tt.hops)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
}

func TestResolveLinks_NoLinkReader(t *testing.T) {
	got, err := ResolveLinks(afero.NewMemMapFs(), "/a/b", MaxLinkHops)
	require.NoError(t, err)
	assert.Equal(t, "/a/b", got)
}

// TestReadRegularLinks_Os checks the kernel side: the Capistrano layout is
// read, a link to a FIFO does not block, a link to a device, a loop and a
// long chain are refused.
func TestReadRegularLinks_Os(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(dir, "shared")
	release := filepath.Join(dir, "releases", "1")
	require.NoError(t, os.MkdirAll(shared, 0o755))
	require.NoError(t, os.MkdirAll(release, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(shared, ".env"), []byte("A=1\n"), 0o600))
	require.NoError(t, os.Symlink("../../shared/.env", filepath.Join(release, ".env")))
	fifo := filepath.Join(dir, "fifo")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	require.NoError(t, os.Symlink(fifo, filepath.Join(dir, "to-fifo")))
	require.NoError(t, os.Symlink("/dev/zero", filepath.Join(dir, "to-zero")))
	require.NoError(t, os.Symlink("loop-b", filepath.Join(dir, "loop-a")))
	require.NoError(t, os.Symlink("loop-a", filepath.Join(dir, "loop-b")))
	for i := range MaxLinkHops + 1 {
		require.NoError(t, os.Symlink(fmt.Sprintf("c%d", i+1), filepath.Join(dir, fmt.Sprintf("c%d", i))))
	}
	require.NoError(t, os.Symlink("shared/.env", filepath.Join(dir, fmt.Sprintf("c%d", MaxLinkHops+1))))
	osFs := afero.NewOsFs()

	b, err := ReadRegularLinks(osFs, filepath.Join(release, ".env"), 10, MaxLinkHops)
	require.NoError(t, err)
	assert.Equal(t, "A=1\n", string(b))

	done := make(chan error, 1)
	go func() { _, err := ReadRegularLinks(osFs, filepath.Join(dir, "to-fifo"), 10, MaxLinkHops); done <- err }()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, ErrNotRegular)
	case <-time.After(5 * time.Second):
		t.Fatal("reading a link to a FIFO blocks")
	}
	_, err = ReadRegularLinks(osFs, filepath.Join(dir, "to-zero"), 10, MaxLinkHops)
	assert.ErrorIs(t, err, ErrNotRegular)
	_, err = ReadRegularLinks(osFs, filepath.Join(dir, "loop-a"), 10, MaxLinkHops)
	assert.ErrorIs(t, err, ErrLinkLoop)
	_, err = ReadRegularLinks(osFs, filepath.Join(dir, "c0"), 10, MaxLinkHops)
	assert.ErrorIs(t, err, ErrTooManyLinks)
	assert.ErrorContains(t, err, "more than 5")
	_, err = ReadRegularLinks(osFs, filepath.Join(dir, "missing"), 10, MaxLinkHops)
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

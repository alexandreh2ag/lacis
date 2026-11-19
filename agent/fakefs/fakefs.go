// Package fakefs is an in-memory file system with symbolic links, to
// reproduce a /proc tree in tests: afero.MemMapFs has no links.
package fakefs

import (
	"io/fs"
	"maps"
	"os"
	"path"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/afero"
)

// FS is an afero.MemMapFs whose links are only read, never followed.
type FS struct {
	afero.Fs
	links  map[string]string
	owners map[string]uint32
}

var (
	_ afero.LinkReader = (*FS)(nil)
	_ afero.Lstater    = (*FS)(nil)
)

func New() *FS {
	return &FS{Fs: afero.NewMemMapFs(), links: map[string]string{}, owners: map[string]uint32{}}
}

// Own makes uid the owner of path, a file, a directory or a link. A path
// nobody owns tells no owner.
func (f *FS) Own(path string, uid uint32) {
	f.owners[path] = uid
}

// Stat tells the owner given by Own.
func (f *FS) Stat(name string) (os.FileInfo, error) {
	fi, err := f.Fs.Stat(name)
	if err != nil {
		return nil, err
	}
	return f.owned(name, fi), nil
}

// LstatIfPossible describes a link itself, and any other path as Stat.
func (f *FS) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if target, ok := f.links[name]; ok {
		return f.owned(name, linkInfo{name: path.Base(name), target: target}), true, nil
	}
	fi, err := f.Stat(name)
	return fi, true, err
}

func (f *FS) owned(name string, fi os.FileInfo) os.FileInfo {
	if uid, ok := f.owners[name]; ok {
		return ownedInfo{FileInfo: fi, uid: uid}
	}
	return fi
}

// ownedInfo gives a file an owner, as the os package does.
type ownedInfo struct {
	os.FileInfo
	uid uint32
}

func (o ownedInfo) Sys() any { return &syscall.Stat_t{Uid: o.uid} }

// linkInfo describes a link.
type linkInfo struct {
	name, target string
}

func (l linkInfo) Name() string       { return l.name }
func (l linkInfo) Size() int64        { return int64(len(l.target)) }
func (l linkInfo) Mode() os.FileMode  { return os.ModeSymlink | 0o777 }
func (l linkInfo) ModTime() time.Time { return time.Time{} }
func (l linkInfo) IsDir() bool        { return false }
func (l linkInfo) Sys() any           { return nil }

// Link makes path a link to target, like os.Symlink(target, path).
func (f *FS) Link(path, target string) {
	f.links[path] = target
}

// Write creates path with content, failing the test on error.
func (f *FS) Write(t testing.TB, path, content string) {
	t.Helper()
	if err := afero.WriteFile(f.Fs, path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Files writes every path of files.
func (f *FS) Files(t testing.TB, files map[string]string) {
	t.Helper()
	for _, path := range sortedKeys(files) {
		f.Write(t, path, files[path])
	}
}

func sortedKeys(m map[string]string) []string {
	return slices.Sorted(maps.Keys(m))
}

func (f *FS) ReadlinkIfPossible(name string) (string, error) {
	if target, ok := f.links[name]; ok {
		return target, nil
	}
	return "", &fs.PathError{Op: "readlink", Path: name, Err: fs.ErrNotExist}
}

// OpenFile refuses a link opened with O_NOFOLLOW, like the kernel; a link
// is otherwise not followed: the path does not exist.
func (f *FS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if _, ok := f.links[name]; ok && flag&syscall.O_NOFOLLOW != 0 {
		return nil, &fs.PathError{Op: "open", Path: name, Err: syscall.ELOOP}
	}
	return f.Fs.OpenFile(name, flag, perm)
}

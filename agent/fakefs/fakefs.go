// Package fakefs is an in-memory file system with symbolic links, to
// reproduce a /proc tree in tests: afero.MemMapFs has no links.
package fakefs

import (
	"io/fs"
	"maps"
	"slices"
	"testing"

	"github.com/spf13/afero"
)

// FS is an afero.MemMapFs whose links are only read, never followed.
type FS struct {
	afero.Fs
	links map[string]string
}

var _ afero.LinkReader = (*FS)(nil)

func New() *FS {
	return &FS{Fs: afero.NewMemMapFs(), links: map[string]string{}}
}

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

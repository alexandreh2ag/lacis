package state

import (
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveLoad(t *testing.T) {
	fsys := afero.NewMemMapFs()
	_, err := Load(fsys, "/var/lib/lacis")
	assert.ErrorIs(t, err, ErrNotEnrolled)

	want := &Identity{AgentID: "019a2400-0000-7000-8000-0000000000aa", AgentToken: "lacis_at_secret"}
	require.NoError(t, Save(fsys, "/var/lib/lacis", want))

	got, err := Load(fsys, "/var/lib/lacis")
	require.NoError(t, err)
	assert.Equal(t, want, got)

	dir, err := fsys.Stat("/var/lib/lacis")
	require.NoError(t, err)
	assert.Equal(t, "drwx------", dir.Mode().String())
	file, err := fsys.Stat("/var/lib/lacis/" + IdentityFile)
	require.NoError(t, err)
	assert.Equal(t, "-rw-------", file.Mode().String())
	entries, err := afero.ReadDir(fsys, "/var/lib/lacis")
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file left")

	// Saved again over the previous identity.
	want.AgentToken = "lacis_at_other"
	require.NoError(t, Save(fsys, "/var/lib/lacis", want))
	got, err = Load(fsys, "/var/lib/lacis")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestSave_OsFs(t *testing.T) {
	dir := t.TempDir()
	want := &Identity{AgentID: "a", AgentToken: "t"}
	require.NoError(t, Save(afero.NewOsFs(), dir, want))
	got, err := Load(afero.NewOsFs(), dir)
	require.NoError(t, err)
	assert.Equal(t, want, got)
	info, err := os.Stat(dir + "/" + IdentityFile)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestLoad_Errors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		mode    os.FileMode
		want    string
	}{
		{name: "not json", content: "{", want: "decode identity"},
		{name: "incomplete", content: `{"agent_id":"a"}`, want: "incomplete"},
		{name: "readable by others", content: `{"agent_id":"a","agent_token":"t"}`, mode: 0o644, want: "accessible by other users"},
		{name: "readable by the group", content: `{"agent_id":"a","agent_token":"t"}`, mode: 0o640, want: "chmod 600"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := afero.NewMemMapFs()
			mode := tt.mode
			if mode == 0 {
				mode = 0o600
			}
			require.NoError(t, afero.WriteFile(fsys, "/state/"+IdentityFile, []byte(tt.content), mode))
			_, err := Load(fsys, "/state")
			assert.ErrorContains(t, err, tt.want)
		})
	}
	t.Run("unreadable", func(t *testing.T) {
		_, err := Load(afero.NewReadOnlyFs(failingFs{afero.NewMemMapFs()}), "/state")
		assert.ErrorContains(t, err, "read identity")
	})
}

func TestSave_Errors(t *testing.T) {
	id := &Identity{AgentID: "a", AgentToken: "t"}
	t.Run("read-only", func(t *testing.T) {
		err := Save(afero.NewReadOnlyFs(afero.NewMemMapFs()), "/state", id)
		assert.ErrorContains(t, err, "create state directory")
	})
	t.Run("write fails", func(t *testing.T) {
		assert.ErrorContains(t, Save(writeFailFs{afero.NewMemMapFs()}, "/state", id), "write identity")
	})
	t.Run("rename fails", func(t *testing.T) {
		fsys := renameFailFs{afero.NewMemMapFs()}
		assert.ErrorContains(t, Save(fsys, "/state", id), "write identity")
		entries, err := afero.ReadDir(fsys, "/state")
		require.NoError(t, err)
		assert.Empty(t, entries, "temporary file removed")
	})
	t.Run("chmod fails", func(t *testing.T) {
		assert.ErrorContains(t, Save(chmodFailFs{afero.NewMemMapFs()}, "/state", id), "write identity")
	})
}

// failingFs fails every open with an error other than "not exist".
type failingFs struct{ afero.Fs }

func (failingFs) Open(string) (afero.File, error) { return nil, assert.AnError }

type writeFailFs struct{ afero.Fs }

func (writeFailFs) OpenFile(string, int, os.FileMode) (afero.File, error) { return nil, assert.AnError }

type renameFailFs struct{ afero.Fs }

func (renameFailFs) Rename(string, string) error { return assert.AnError }

type chmodFailFs struct{ afero.Fs }

func (chmodFailFs) Chmod(string, os.FileMode) error { return assert.AnError }

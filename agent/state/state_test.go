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
	exists, err := afero.Exists(fsys, "/var/lib/lacis/"+IdentityFile+".tmp")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestLoad_Errors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "not json", content: "{", want: "decode identity"},
		{name: "incomplete", content: `{"agent_id":"a"}`, want: "incomplete"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := afero.NewMemMapFs()
			require.NoError(t, afero.WriteFile(fsys, "/state/"+IdentityFile, []byte(tt.content), 0o600))
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
		assert.ErrorContains(t, Save(renameFailFs{afero.NewMemMapFs()}, "/state", id), "write identity")
	})
}

// failingFs fails every open with an error other than "not exist".
type failingFs struct{ afero.Fs }

func (failingFs) Open(string) (afero.File, error) { return nil, assert.AnError }

type writeFailFs struct{ afero.Fs }

func (writeFailFs) OpenFile(string, int, os.FileMode) (afero.File, error) { return nil, assert.AnError }

type renameFailFs struct{ afero.Fs }

func (renameFailFs) Rename(string, string) error { return assert.AnError }

// Package state keeps the identity of the agent in its state directory,
// readable by root only.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"

	"github.com/spf13/afero"
)

// IdentityFile is the name of the identity file in the state directory.
const IdentityFile = "identity.json"

// ErrNotEnrolled is returned when the state directory holds no identity.
var ErrNotEnrolled = errors.New("agent not enrolled")

// Identity is what the server gave the agent at enrollment.
type Identity struct {
	AgentID    string `json:"agent_id"`
	AgentToken string `json:"agent_token"`
}

// Load reads the identity from dir, and returns ErrNotEnrolled when there
// is none. An identity readable by the group or the others is refused: its
// token may have leaked.
func Load(fsys afero.Fs, dir string) (*Identity, error) {
	path := filepath.Join(dir, IdentityFile)
	f, err := fsys.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotEnrolled
	}
	if err != nil {
		return nil, fmt.Errorf("read identity: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("read identity: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("identity %s is accessible by other users (mode %s): its token may have leaked, revoke the agent and enroll it again, or chmod 600 the file", path, info.Mode().Perm())
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read identity: %w", err)
	}
	id := &Identity{}
	if err := json.Unmarshal(b, id); err != nil {
		return nil, fmt.Errorf("decode identity %s: %w", filepath.Join(dir, IdentityFile), err)
	}
	if id.AgentID == "" || id.AgentToken == "" {
		return nil, fmt.Errorf("identity %s is incomplete", filepath.Join(dir, IdentityFile))
	}
	return id, nil
}

// Save writes the identity in dir, created when missing. Both are readable
// by their owner only.
func Save(fsys afero.Fs, dir string, id *Identity) error {
	if err := fsys.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	b, err := json.Marshal(id)
	if err != nil {
		return err
	}
	// Written aside, synced then renamed, so that a crash never leaves a
	// partial or empty identity.
	if err := writeTemp(fsys, dir, b); err != nil {
		return fmt.Errorf("write identity: %w", err)
	}
	return nil
}

// writeTemp writes b in a new file of dir, readable by its owner only
// before anything is written, then renames it to IdentityFile.
func writeTemp(fsys afero.Fs, dir string, b []byte) (err error) {
	f, err := afero.TempFile(fsys, dir, IdentityFile+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = fsys.Remove(tmp)
		}
	}()
	if err = fsys.Chmod(tmp, 0o600); err != nil {
		return err
	}
	if _, err = f.Write(b); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = fsys.Rename(tmp, filepath.Join(dir, IdentityFile)); err != nil {
		return err
	}
	// The rename is durable once the directory is synced too.
	if d, derr := fsys.Open(dir); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

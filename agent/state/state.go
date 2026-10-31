// Package state keeps the identity of the agent in its state directory,
// readable by root only.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
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
// is none.
func Load(fsys afero.Fs, dir string) (*Identity, error) {
	b, err := afero.ReadFile(fsys, filepath.Join(dir, IdentityFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotEnrolled
	}
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
	// Written aside then renamed, so that a crash never leaves a partial
	// identity.
	path := filepath.Join(dir, IdentityFile)
	if err := afero.WriteFile(fsys, path+".tmp", b, 0o600); err != nil {
		return fmt.Errorf("write identity: %w", err)
	}
	if err := fsys.Rename(path+".tmp", path); err != nil {
		return fmt.Errorf("write identity: %w", err)
	}
	return nil
}

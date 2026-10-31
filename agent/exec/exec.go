// Package exec runs the local commands of the agent. Collectors never call
// os/exec directly: they go through an Executor, mocked in tests.
package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	osexec "os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var (
	ErrRelativePath = errors.New("command path must be absolute")
	ErrNotAllowed   = errors.New("command not allowed")
)

// Allowed are the binaries the agent may run, matched on their base name:
// the path of a runtime, such as an nvm node, is only known on the host.
var Allowed = []*regexp.Regexp{
	regexp.MustCompile(`^systemd-detect-virt$`),
	regexp.MustCompile(`^dpkg-query$`),
	regexp.MustCompile(`^rpm$`),
	regexp.MustCompile(`^php(\d+(\.\d+)?)?$`),
	regexp.MustCompile(`^node$`),
	regexp.MustCompile(`^nginx$`),
	// --version of the services installed without package.
	regexp.MustCompile(`^(redis-server|mysqld|mariadbd|postgres)$`),
}

// Executor runs a command and returns its standard output. The caller bounds
// the run with the context deadline.
type Executor interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// OsExecutor runs commands with os/exec. It only accepts absolute paths, so
// that the PATH of the daemon never decides which binary runs as root, and
// only the binaries of its allow list.
type OsExecutor struct {
	allowed []*regexp.Regexp
}

var _ Executor = OsExecutor{}

// NewOsExecutor returns an executor allowing the Allowed binaries.
func NewOsExecutor() Executor {
	return OsExecutor{allowed: Allowed}
}

func (e OsExecutor) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if !filepath.IsAbs(name) {
		return nil, fmt.Errorf("%s: %w", name, ErrRelativePath)
	}
	if !slices.ContainsFunc(e.allowed, func(re *regexp.Regexp) bool { return re.MatchString(filepath.Base(name)) }) {
		return nil, fmt.Errorf("%s: %w", name, ErrNotAllowed)
	}
	cmd := osexec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"LC_ALL=C", "PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return out, fmt.Errorf("%s: %w: %s", name, err, msg)
		}
		return out, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

// Package exec runs the local commands of the agent. Collectors never call
// os/exec directly: they go through an Executor, mocked in tests. It is
// the only place switching to another user, so that tests never need
// root.
package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alexandreh2ag/lacis/agent/fsutil"
)

var (
	ErrRelativePath   = errors.New("command path must be absolute")
	ErrNotAllowed     = errors.New("command not allowed")
	ErrOutputTooLarge = errors.New("command output too large")
)

// Allowed are the binaries the agent may run, matched on their base name:
// the path of a runtime, such as an nvm node, is only known on the host.
// nginx is not: its configuration is read, never dumped by the binary.
var Allowed = []*regexp.Regexp{
	regexp.MustCompile(`^systemd-detect-virt$`),
	regexp.MustCompile(`^dpkg-query$`),
	regexp.MustCompile(`^rpm$`),
	regexp.MustCompile(`^php(\d+(\.\d+)?)?$`),
	regexp.MustCompile(`^node$`),
	// --version of the services installed without package.
	regexp.MustCompile(`^(redis-server|mysqld|mariadbd|postgres|varnishd|vault|minio|silo)$`),
}

const (
	// RunTimeout bounds a command of the agent, such as the package list.
	RunTimeout = time.Minute
	// ProbeTimeout bounds a command run as another user.
	ProbeTimeout = 10 * time.Second
	// WaitDelay is how long a command killed on timeout may keep its
	// output open.
	WaitDelay = 2 * time.Second
	// MaxOutput caps the output of a command of the agent; MaxProbeOutput
	// the one of a command run as another user, such as --version.
	MaxOutput      = 32 << 20
	MaxProbeOutput = 64 << 10
	maxStderr      = 4 << 10
)

// Credential is a user, its group and its supplementary groups.
type Credential struct {
	UID    uint32
	GID    uint32
	Groups []uint32
}

// Nobody runs the version probes: no supplementary group, so none of root.
var Nobody = Credential{UID: 65534, GID: 65534, Groups: []uint32{}}

// Executor runs a command and returns its standard output. The caller bounds
// the run with the context deadline.
type Executor interface {
	// Run runs a command as the agent: the package managers.
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
	// RunAs runs a command as cred, such as a --version probe.
	RunAs(ctx context.Context, cred Credential, name string, args ...string) ([]byte, error)
	// ReadFileAs reads a regular file of at most max bytes as cred, with
	// "lacis-agent internal read": a configuration file named by the
	// command line of a process is read with the rights of that process.
	// The error wraps fs.ErrNotExist, fs.ErrPermission,
	// fsutil.ErrNotRegular or fsutil.ErrTooLarge when it is one of them.
	ReadFileAs(ctx context.Context, cred Credential, path string, max int64) ([]byte, error)
}

// OsExecutor runs commands with os/exec. It only accepts absolute paths, so
// that the PATH of the daemon never decides which binary runs as root, and
// only the binaries of its allow list.
type OsExecutor struct {
	allowed []*regexp.Regexp
	// self is the binary of the agent, for ReadFileAs.
	self string
}

var _ Executor = OsExecutor{}

// NewOsExecutor returns an executor allowing the Allowed binaries.
func NewOsExecutor() Executor {
	self, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	return OsExecutor{allowed: Allowed, self: self}
}

func (e OsExecutor) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if err := e.check(name); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, RunTimeout)
	defer cancel()
	return run(ctx, nil, MaxOutput, name, args...)
}

func (e OsExecutor) RunAs(ctx context.Context, cred Credential, name string, args ...string) ([]byte, error) {
	if err := e.check(name); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	out, stderr, err := runWithStderr(ctx, &cred, MaxProbeOutput, name, args...)
	if err == nil && len(bytes.TrimSpace(out)) == 0 {
		// Some binaries print their version on stderr: varnishd -V.
		return stderr, nil
	}
	return out, err
}

func (e OsExecutor) ReadFileAs(ctx context.Context, cred Credential, path string, max int64) ([]byte, error) {
	if e.self == "" {
		return nil, errors.New("agent binary unknown")
	}
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	out, err := run(ctx, &cred, max+1, e.self, "internal", "read", "--max", strconv.FormatInt(max, 10), "--", path)
	var exitErr *osexec.ExitError
	if errors.As(err, &exitErr) {
		return nil, fsutil.ErrorOf(exitErr.ExitCode(), err.Error())
	}
	if errors.Is(err, ErrOutputTooLarge) {
		return nil, fmt.Errorf("%s: %w", path, fsutil.ErrTooLarge)
	}
	return out, err
}

func (e OsExecutor) check(name string) error {
	if !filepath.IsAbs(name) {
		return fmt.Errorf("%s: %w", name, ErrRelativePath)
	}
	if !slices.ContainsFunc(e.allowed, func(re *regexp.Regexp) bool { return re.MatchString(filepath.Base(name)) }) {
		return fmt.Errorf("%s: %w", name, ErrNotAllowed)
	}
	return nil
}

// run runs a command in its own process group, killed with its children
// when ctx is done, as cred when given and the agent is root: an agent run
// by a user already runs commands without privilege.
func run(ctx context.Context, cred *Credential, maxOut int64, name string, args ...string) ([]byte, error) {
	out, _, err := runWithStderr(ctx, cred, maxOut, name, args...)
	return out, err
}

// runWithStderr is run also returning the standard error, capped.
func runWithStderr(ctx context.Context, cred *Credential, maxOut int64, name string, args ...string) ([]byte, []byte, error) {
	cmd := osexec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"LC_ALL=C", "PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	cmd.Dir = "/"
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if cred != nil && os.Geteuid() == 0 {
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: cred.UID, Gid: cred.GID, Groups: slices.Clone(cred.Groups)}
		if cmd.SysProcAttr.Credential.Groups == nil {
			cmd.SysProcAttr.Credential.Groups = []uint32{} // never those of root
		}
	}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = WaitDelay
	stdout := &capped{max: maxOut}
	stderr := &capped{max: maxStderr}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	if err == nil && stdout.truncated {
		err = ErrOutputTooLarge
	}
	if err != nil {
		if msg := strings.TrimSpace(stderr.buf.String()); msg != "" {
			return stdout.buf.Bytes(), stderr.buf.Bytes(), fmt.Errorf("%s: %w: %s", name, err, msg)
		}
		return stdout.buf.Bytes(), stderr.buf.Bytes(), fmt.Errorf("%s: %w", name, err)
	}
	return stdout.buf.Bytes(), stderr.buf.Bytes(), nil
}

// capped keeps the first max bytes written, and drops the rest.
type capped struct {
	buf       bytes.Buffer
	max       int64
	truncated bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - int64(c.buf.Len()); room < int64(len(p)) {
		c.truncated = true
		c.buf.Write(p[:max(room, 0)])
		return len(p), nil
	}
	return c.buf.Write(p)
}

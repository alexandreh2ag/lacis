package system

import (
	stdContext "context"
	"errors"
	"fmt"
	"io/fs"
	"syscall"

	"github.com/alexandreh2ag/lacis/agent/exec"
	"github.com/alexandreh2ag/lacis/agent/fsutil"
)

var (
	ErrUnknownUser = errors.New("unknown user of the process")
	ErrUnsafeProbe = errors.New("binary of a root process writable by another user, not run")
)

// Credential returns the user, group and supplementary groups of p, false
// when they are unknown.
func Credential(p Process) (exec.Credential, bool) {
	if p.UID < 0 || p.GID < 0 {
		return exec.Credential{}, false
	}
	groups := make([]uint32, 0, len(p.Groups))
	for _, g := range p.Groups {
		groups = append(groups, uint32(g))
	}
	return exec.Credential{UID: uint32(p.UID), GID: uint32(p.GID), Groups: groups}, true
}

// ReadAs reads a configuration file named by the command line of p (its
// -c, -f…) with the rights of p: a process of bob can only make the agent
// read what bob reads. A file of a root process is read directly; the
// others through "lacis-agent internal read" run as the user of p.
func (s *System) ReadAs(stdCtx stdContext.Context, p Process, file string) ([]byte, error) {
	if p.UID == 0 {
		return fsutil.ReadRegularFollow(s.ctx.Fs, file, fsutil.MaxConfigSize)
	}
	cred, ok := Credential(p)
	if !ok {
		return nil, fmt.Errorf("read %s: %w", file, ErrUnknownUser)
	}
	return s.ctx.Executor.ReadFileAs(stdCtx, cred, file, fsutil.MaxConfigSize)
}

// Probe runs a binary found on the host, such as "php -v", as nobody
// without supplementary group. When nobody cannot run it (a binary in a
// 0700 home), it runs again as p, the process running it, when known: that
// user already runs it, no privilege is gained. A binary of a root
// process that another user can modify is never run as root.
func (s *System) Probe(stdCtx stdContext.Context, p *Process, exe string, args ...string) ([]byte, error) {
	out, err := s.ctx.Executor.RunAs(stdCtx, exec.Nobody, exe, args...)
	if err == nil || !errors.Is(err, fs.ErrPermission) || p == nil {
		return out, err
	}
	cred, ok := Credential(*p)
	if !ok {
		return nil, err
	}
	if p.UID == 0 && s.writableByOthers(exe) {
		return nil, fmt.Errorf("%s: %w", exe, ErrUnsafeProbe)
	}
	return s.ctx.Executor.RunAs(stdCtx, cred, exe, args...)
}

// fileOwner returns the owner and group of a file, false when the file
// system does not tell them.
var fileOwner = func(fi fs.FileInfo) (uid, gid uint32, ok bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return st.Uid, st.Gid, true
}

// writableByOthers reports whether a user other than root may modify the
// binary path; true when unknown.
func (s *System) writableByOthers(path string) bool {
	fi, err := s.ctx.Fs.Stat(path)
	if err != nil {
		return true
	}
	uid, gid, ok := fileOwner(fi)
	if !ok {
		return true
	}
	mode := fi.Mode().Perm()
	return uid != 0 || mode&0o002 != 0 || (mode&0o020 != 0 && gid != 0)
}

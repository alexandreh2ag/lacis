package system

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/afero"
)

const (
	ProcDir    = "/proc"
	PasswdFile = "/etc/passwd"
)

// ErrNoLinks is returned when the file system cannot read symbolic links.
var ErrNoLinks = errors.New("the file system cannot read symbolic links")

// Process is a running user-space process; kernel threads are left out.
type Process struct {
	PID  int
	PPID int
	// Name is the short name of /proc/<pid>/status, at most 15 characters.
	Name string
	// Exe is the real binary, symbolic links resolved by the kernel.
	Exe     string
	Cwd     string
	Cmdline []string
	UID     int
	// User is the name of UID, empty when /etc/passwd does not know it.
	User string
	// InContainer is true for a process of another PID namespace than the
	// agent, such as a Docker container: its paths are not those of the
	// host. The mount namespace would not do: systemd gives one to the
	// services with PrivateTmp or ProtectSystem.
	InContainer bool
}

// Processes returns the running processes, sorted by PID. A process ending
// while it is read is left out.
func (s *System) Processes() ([]Process, error) {
	return s.processes.get(func() ([]Process, error) {
		pids, err := s.pids()
		if err != nil {
			return nil, err
		}
		users := s.users()
		selfNS, _ := s.Readlink(ProcDir + "/self/ns/pid")
		var out []Process
		for _, pid := range pids {
			p, ok := s.process(pid)
			if !ok {
				continue
			}
			p.User = users[p.UID]
			if ns, err := s.Readlink(fmt.Sprintf("%s/%d/ns/pid", ProcDir, pid)); err == nil && selfNS != "" {
				p.InContainer = ns != selfNS
			}
			out = append(out, p)
		}
		return out, nil
	})
}

// pids lists the numeric entries of /proc, sorted.
func (s *System) pids() ([]int, error) {
	entries, err := afero.ReadDir(s.ctx.Fs, ProcDir)
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	var pids []int
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil && e.IsDir() {
			pids = append(pids, pid)
		}
	}
	slices.Sort(pids)
	return pids, nil
}

// process reads /proc/<pid>; false for a kernel thread or a process gone.
func (s *System) process(pid int) (Process, bool) {
	dir := fmt.Sprintf("%s/%d/", ProcDir, pid)
	status, err := afero.ReadFile(s.ctx.Fs, dir+"status")
	if err != nil {
		return Process{}, false
	}
	p := Process{PID: pid, UID: -1}
	scanner := bufio.NewScanner(bytes.NewReader(status))
	for scanner.Scan() {
		key, value, _ := strings.Cut(scanner.Text(), ":")
		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}
		switch key {
		case "Name":
			p.Name = fields[0]
		case "PPid":
			p.PPID, _ = strconv.Atoi(fields[0])
		case "Uid":
			p.UID, _ = strconv.Atoi(fields[0]) // real UID
		}
	}
	cmdline, _ := afero.ReadFile(s.ctx.Fs, dir+"cmdline")
	if len(cmdline) == 0 {
		return Process{}, false // kernel thread or zombie
	}
	p.Cmdline = strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
	p.Exe, _ = s.Readlink(dir + "exe")
	p.Exe = strings.TrimSuffix(p.Exe, " (deleted)") // binary replaced by an upgrade
	p.Cwd, _ = s.Readlink(dir + "cwd")
	return p, true
}

// Readlink returns the target of the link path, or ErrNoLinks when the file
// system cannot read links.
func (s *System) Readlink(path string) (string, error) {
	lr, ok := s.ctx.Fs.(afero.LinkReader)
	if !ok {
		return "", ErrNoLinks
	}
	return lr.ReadlinkIfPossible(path)
}

// users maps the UIDs of /etc/passwd to their names.
func (s *System) users() map[int]string {
	users := map[int]string{}
	f, err := s.ctx.Fs.Open(PasswdFile)
	if err != nil {
		return users
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) < 3 {
			continue
		}
		if uid, err := strconv.Atoi(fields[2]); err == nil {
			if _, seen := users[uid]; !seen {
				users[uid] = fields[0]
			}
		}
	}
	return users
}

// maxLinks bounds Resolve, against link loops.
const maxLinks = 32

// Resolve returns path with every link resolved, those of its directories
// included, like filepath.EvalSymlinks but on the context file system:
// /usr/lib/jvm/default-java/bin/java → /usr/lib/jvm/java-21-openjdk-amd64/bin/java.
func (s *System) Resolve(path string) (string, error) {
	if _, ok := s.ctx.Fs.(afero.LinkReader); !ok {
		return "", ErrNoLinks
	}
	resolved := "/"
	rest := strings.Split(strings.TrimPrefix(filepath.Clean(path), "/"), "/")
	for links := 0; len(rest) > 0; {
		name := rest[0]
		rest = rest[1:]
		if name == "" || name == "." {
			continue
		}
		next := filepath.Join(resolved, name)
		target, err := s.Readlink(next)
		if err != nil {
			resolved = next // not a link
			continue
		}
		if links++; links > maxLinks {
			return "", fmt.Errorf("%s: too many links", path)
		}
		if filepath.IsAbs(target) {
			resolved = "/"
		}
		rest = append(strings.Split(target, "/"), rest...)
	}
	return resolved, nil
}

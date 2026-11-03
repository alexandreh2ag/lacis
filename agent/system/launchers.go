package system

import (
	stdContext "context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/fsutil"
	"github.com/alexandreh2ag/lacis/agent/ini"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/spf13/afero"
)

const LauncherSupervisor = "supervisor"

// SupervisorConfigs are the main supervisor files, Debian then RHEL, read
// when the running supervisord names none with -c.
var SupervisorConfigs = []string{"/etc/supervisor/supervisord.conf", "/etc/supervisord.conf"}

// SupervisorGlobs are the program files read when there is no main file:
// Debian, then RHEL.
var SupervisorGlobs = []string{"/etc/supervisor/conf.d/*.conf", "/etc/supervisord.d/*.ini"}

// Launcher is a program started by a process manager, with the environment
// it gives the program: the app configuration may read it.
type Launcher struct {
	snapshot.Launcher
	Environment map[string]string
	// File is the configuration file declaring the program.
	File string
	// PIDs are the running processes of the program, children of
	// supervisord with its command; several with numprocs.
	PIDs []int
}

// Launchers returns the supervisor programs, sorted by name.
func (s *System) Launchers() ([]Launcher, error) {
	return s.launchers.get(func() ([]Launcher, error) {
		var out []Launcher
		main, proc := s.supervisorMain()
		read := s.supervisorReader(proc)
		files, err := s.supervisorFiles(main, read)
		if err != nil {
			return nil, err
		}
		// A file that cannot be read is reported, the others still read.
		var errs []error
		for _, file := range files {
			l, err := supervisorPrograms(read, file)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			out = append(out, l...)
		}
		s.launcherPIDs(out)
		slices.SortFunc(out, func(a, b Launcher) int { return strings.Compare(a.Name, b.Name) })
		return out, errors.Join(errs...)
	})
}

// launcherPIDs finds the processes of the programs among the children of
// supervisord: by the SUPERVISOR_PROCESS_NAME supervisord sets in their
// environment, which survives an exec such as the one of nvm-exec, else by
// their command line.
func (s *System) launcherPIDs(launchers []Launcher) {
	processes, _ := s.Processes() // without them, no PIDs
	supervisors := map[int]bool{}
	for _, p := range processes {
		if !p.InContainer && isSupervisord(p) {
			supervisors[p.PID] = true
		}
	}
	for _, p := range processes {
		if !supervisors[p.PPID] {
			continue
		}
		if i := programOf(launchers, s.environ(p.PID)["SUPERVISOR_PROCESS_NAME"], strings.Join(p.Cmdline, " ")); i >= 0 {
			launchers[i].PIDs = append(launchers[i].PIDs, p.PID)
		}
	}
}

var processNum = regexp.MustCompile(`^_\d+$`)

// programOf returns the index of the program of a process named name by
// supervisord, -1 for none: the program of that name, else the one it is an
// instance of with numprocs (<program>_<num>), else, without name, the one
// whose command is cmdline.
func programOf(launchers []Launcher, name, cmdline string) int {
	if name != "" {
		if i := slices.IndexFunc(launchers, func(l Launcher) bool { return l.Name == name }); i >= 0 {
			return i
		}
		return slices.IndexFunc(launchers, func(l Launcher) bool {
			rest, ok := strings.CutPrefix(name, l.Name)
			return ok && processNum.MatchString(rest)
		})
	}
	cmdline = normalizeCommand(cmdline)
	return slices.IndexFunc(launchers, func(l Launcher) bool { return normalizeCommand(l.Command) == cmdline })
}

// environ returns the environment of a process, empty when unreadable.
func (s *System) environ(pid int) map[string]string {
	env := map[string]string{}
	b, err := afero.ReadFile(s.ctx.Fs, fmt.Sprintf("%s/%d/environ", ProcDir, pid))
	if err != nil {
		return env
	}
	for _, kv := range strings.Split(string(b), "\x00") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return env
}

// normalizeCommand compares a command of a configuration with a command
// line: quotes dropped, spaces collapsed.
func normalizeCommand(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer(`"`, "", "'", "").Replace(s)), " ")
}

var python = regexp.MustCompile(`^python[\d.]*$`)

// isSupervisord reports whether p is a supervisord: its binary or argv[0],
// or the script run by python, never any argument.
func isSupervisord(p Process) bool {
	args := p.Cmdline
	if len(args) == 1 {
		args = strings.Fields(args[0]) // title rewritten
	}
	if len(args) == 0 {
		return false
	}
	argv0 := filepath.Base(args[0])
	if filepath.Base(p.Exe) == "supervisord" || argv0 == "supervisord" {
		return true
	}
	return (python.MatchString(filepath.Base(p.Exe)) || python.MatchString(argv0)) &&
		len(args) > 1 && filepath.Base(args[1]) == "supervisord"
}

// supervisorReader reads the files of the running supervisord with its
// rights (they come from its command line), the default ones, without
// supervisord, as the agent.
func (s *System) supervisorReader(proc *Process) func(file string) ([]byte, error) {
	if proc == nil {
		return func(file string) ([]byte, error) {
			return fsutil.ReadRegularFollow(s.ctx.Fs, file, fsutil.MaxConfigSize)
		}
	}
	return func(file string) ([]byte, error) {
		return s.ReadAs(stdContext.Background(), *proc, file)
	}
}

// supervisorFiles returns the main supervisor file and the files of its
// [include] section, as supervisord reads them; without main file, the
// files of SupervisorGlobs.
func (s *System) supervisorFiles(main string, read func(string) ([]byte, error)) ([]string, error) {
	if main == "" {
		var files []string
		for _, glob := range SupervisorGlobs {
			matches, _ := afero.Glob(s.ctx.Fs, glob) // only ErrBadPattern
			files = append(files, matches...)
		}
		return files, nil
	}
	includes, err := supervisorIncludes(read, main)
	if err != nil {
		return nil, err
	}
	files := []string{main}
	for _, pattern := range includes {
		matches, _ := afero.Glob(s.ctx.Fs, pattern) // only ErrBadPattern
		slices.Sort(matches)
		files = append(files, matches...)
	}
	return files, nil
}

// supervisorMain returns the -c of the running supervisord and its
// process, or the first of SupervisorConfigs found.
func (s *System) supervisorMain() (string, *Process) {
	processes, _ := s.Processes() // without them, the default files
	for _, p := range processes {
		if p.InContainer || !isSupervisord(p) {
			continue
		}
		fields := strings.Fields(strings.Join(p.Cmdline, " "))
		for i, f := range fields {
			if (f == "-c" || f == "--configuration") && i+1 < len(fields) {
				return fields[i+1], &p
			}
			if v, ok := strings.CutPrefix(f, "--configuration="); ok {
				return v, &p
			}
		}
	}
	for _, file := range SupervisorConfigs {
		if _, err := s.ctx.Fs.Stat(file); err == nil {
			return file, nil
		}
	}
	return "", nil
}

// supervisorOptions are those of the supervisor INI dialect: indented
// lines continue the previous value, ";" starts a comment after a space.
var supervisorOptions = ini.Options{Continuation: true, Value: stripComment}

// supervisorIncludes reads the "files" of the [include] section of a main
// file: space separated globs, relative to its directory, %(here)s too.
func supervisorIncludes(read func(string) ([]byte, error), main string) ([]string, error) {
	b, err := read(main)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", main, err)
	}
	sections, err := ini.Parse(b, supervisorOptions)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", main, err)
	}
	dir := filepath.Dir(main)
	var out []string
	for _, section := range sections {
		if section.Name != "include" {
			continue
		}
		value, _ := section.Get("files")
		for _, pattern := range strings.Fields(strings.ReplaceAll(value, "%(here)s", dir)) {
			if !filepath.IsAbs(pattern) {
				pattern = filepath.Join(dir, pattern)
			}
			out = append(out, pattern)
		}
	}
	return out, nil
}

// supervisorPrograms reads the [program:x] sections of a supervisor file.
func supervisorPrograms(read func(string) ([]byte, error), file string) ([]Launcher, error) {
	b, err := read(file)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", file, err)
	}
	sections, err := ini.Parse(b, supervisorOptions)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", file, err)
	}
	var out []Launcher
	for _, section := range sections {
		name, ok := strings.CutPrefix(section.Name, "program:")
		if !ok {
			continue
		}
		l := Launcher{Launcher: snapshot.Launcher{Type: LauncherSupervisor, Name: strings.TrimSpace(name)}, File: file}
		l.Command, _ = section.Get("command")
		l.Directory, _ = section.Get("directory")
		l.User, _ = section.Get("user")
		env, _ := section.Get("environment")
		l.Environment = parseEnvironment(env)
		out = append(out, l)
	}
	return out, nil
}

// stripComment removes an inline comment, which supervisor only knows
// after a space: "a ;b" but not "a;b".
func stripComment(value string) string {
	if i := strings.Index(value, " ;"); i >= 0 {
		value = value[:i]
	}
	return strings.TrimSpace(value)
}

// parseEnvironment reads KEY="value",KEY2=value2; quoted values may hold
// commas.
func parseEnvironment(s string) map[string]string {
	env := map[string]string{}
	for len(s) > 0 {
		key, rest, ok := strings.Cut(s, "=")
		if !ok {
			break
		}
		key = strings.TrimSpace(key)
		rest = strings.TrimLeft(rest, " ")
		var value string
		if rest != "" && (rest[0] == '"' || rest[0] == '\'') {
			end := strings.IndexByte(rest[1:], rest[0])
			if end < 0 {
				value, rest = rest[1:], ""
			} else {
				value, rest = rest[1:end+1], rest[end+2:]
			}
			_, rest, _ = strings.Cut(rest, ",")
		} else {
			value, rest, _ = strings.Cut(rest, ",")
			value = strings.TrimSpace(value)
		}
		if key != "" {
			env[key] = value
		}
		s = rest
	}
	return env
}

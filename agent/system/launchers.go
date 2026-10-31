package system

import (
	"bufio"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

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
		files, err := s.supervisorFiles()
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			l, err := s.supervisorPrograms(file)
			if err != nil {
				return nil, err
			}
			out = append(out, l...)
		}
		s.launcherPIDs(out)
		slices.SortFunc(out, func(a, b Launcher) int { return strings.Compare(a.Name, b.Name) })
		return out, nil
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
		if !p.InContainer && slices.ContainsFunc(strings.Fields(strings.Join(p.Cmdline, " ")), func(f string) bool { return strings.HasSuffix(f, "supervisord") }) {
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

// supervisorFiles returns the main supervisor file and the files of its
// [include] section, as supervisord reads them; without main file, the
// files of SupervisorGlobs.
func (s *System) supervisorFiles() ([]string, error) {
	main := s.supervisorMain()
	if main == "" {
		var files []string
		for _, glob := range SupervisorGlobs {
			matches, _ := afero.Glob(s.ctx.Fs, glob) // only ErrBadPattern
			files = append(files, matches...)
		}
		return files, nil
	}
	includes, err := supervisorIncludes(s.ctx.Fs, main)
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

// supervisorMain returns the -c of the running supervisord, or the first
// of SupervisorConfigs found.
func (s *System) supervisorMain() string {
	processes, _ := s.Processes() // without them, the default files
	for _, p := range processes {
		fields := strings.Fields(strings.Join(p.Cmdline, " "))
		if p.InContainer || !slices.ContainsFunc(fields, func(f string) bool { return strings.HasSuffix(f, "supervisord") }) {
			continue
		}
		for i, f := range fields {
			if (f == "-c" || f == "--configuration") && i+1 < len(fields) {
				return fields[i+1]
			}
			if v, ok := strings.CutPrefix(f, "--configuration="); ok {
				return v
			}
		}
	}
	for _, file := range SupervisorConfigs {
		if _, err := s.ctx.Fs.Stat(file); err == nil {
			return file
		}
	}
	return ""
}

// supervisorIncludes reads the "files" of the [include] section of a main
// file: space separated globs, relative to its directory, %(here)s too.
func supervisorIncludes(fsys afero.Fs, main string) ([]string, error) {
	f, err := fsys.Open(main)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", main, err)
	}
	defer func() { _ = f.Close() }()
	dir := filepath.Dir(main)
	var out []string
	inInclude := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			inInclude = line == "[include]"
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !inInclude || strings.TrimSpace(key) != "files" {
			continue
		}
		value = strings.ReplaceAll(stripComment(strings.TrimSpace(value)), "%(here)s", dir)
		for _, pattern := range strings.Fields(value) {
			if !filepath.IsAbs(pattern) {
				pattern = filepath.Join(dir, pattern)
			}
			out = append(out, pattern)
		}
	}
	return out, scanner.Err()
}

// supervisorPrograms reads the [program:x] sections of a supervisor file,
// an INI file whose indented lines continue the previous value.
func (s *System) supervisorPrograms(file string) ([]Launcher, error) {
	f, err := s.ctx.Fs.Open(file)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", file, err)
	}
	defer func() { _ = f.Close() }()

	var out []Launcher
	var current *Launcher
	var lastKey string
	values := map[string]string{}
	flush := func() {
		if current == nil {
			return
		}
		current.Command = values["command"]
		current.Directory = values["directory"]
		current.User = values["user"]
		current.Environment = parseEnvironment(values["environment"])
		out = append(out, *current)
		current, values = nil, map[string]string{}
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		raw := scanner.Text()
		line := strings.TrimSpace(raw)
		switch {
		case line == "" || line[0] == ';' || line[0] == '#':
			continue
		case line[0] == '[':
			flush()
			section := strings.Trim(line, "[]")
			if name, ok := strings.CutPrefix(section, "program:"); ok {
				current = &Launcher{Launcher: snapshot.Launcher{Type: LauncherSupervisor, Name: strings.TrimSpace(name)}, File: file}
			}
			lastKey = ""
		case current == nil:
			continue
		case raw[0] == ' ' || raw[0] == '\t':
			if lastKey != "" {
				values[lastKey] += " " + stripComment(line)
			}
		default:
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			lastKey = strings.TrimSpace(key)
			values[lastKey] = stripComment(strings.TrimSpace(value))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", file, err)
	}
	flush()
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

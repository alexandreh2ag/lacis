package webservers

import (
	"bufio"
	"bytes"
	stdContext "context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/netaddr"
	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/spf13/afero"
)

const TypeApache = "apache"

// ApachePackages hold the Apache version, first installed wins.
var ApachePackages = []string{"apache2", "apache2-bin", "httpd"}

// apacheDefaults are the server root and main file by binary: Debian, RHEL.
var apacheDefaults = map[string][2]string{
	"apache2": {"/etc/apache2", "apache2.conf"},
	"httpd":   {"/etc/httpd", "conf/httpd.conf"},
}

func isApache(p system.Process) bool {
	_, ok := apacheDefaults[filepath.Base(p.Exe)]
	return ok
}

// readApache reads the configuration files of the Apache master p, from its
// main file and its includes.
func readApache(stdCtx stdContext.Context, ctx *context.Context, sys *system.System, p system.Process) (snapshot.Webserver, error) {
	ws := snapshot.Webserver{}
	packages, err := sys.Packages(stdCtx)
	if err != nil {
		return ws, err
	}
	ws.Version, _ = packages.Version(ApachePackages...)

	defaults := apacheDefaults[filepath.Base(p.Exe)]
	root, main := defaults[0], defaults[1]
	if d := flagValue(p.Cmdline, "-d"); d != "" {
		root = d
	}
	if f := flagValue(p.Cmdline, "-f"); f != "" {
		main = f
	}
	r := &apacheReader{
		fs:      ctx.Fs,
		read:    func(file string) ([]byte, error) { return sys.ReadAs(stdCtx, p, file) },
		root:    root,
		defines: apacheDefines(p.Cmdline),
	}
	sections, err := r.file(r.path(main), 0)
	if err != nil {
		return ws, err
	}
	for _, s := range flatten(sections) {
		if strings.EqualFold(s.Name, "VirtualHost") && s.Block != nil {
			ws.Vhosts = append(ws.Vhosts, apacheVhost(s))
		}
	}
	return ws, errors.Join(r.errs...)
}

// flagValue returns the value of "-x value" in a command line, whose title
// may hold it in one argument.
func flagValue(cmdline []string, flag string) string {
	fields := strings.Fields(strings.Join(cmdline, " "))
	if i := slices.Index(fields, flag); i >= 0 && i+1 < len(fields) {
		return fields[i+1]
	}
	return ""
}

// apacheDefines returns the parameters defined on the command line:
// -DFOREGROUND, -D SSL.
func apacheDefines(cmdline []string) map[string]string {
	defines := map[string]string{}
	fields := strings.Fields(strings.Join(cmdline, " "))
	for i, f := range fields {
		if name, ok := strings.CutPrefix(f, "-D"); ok {
			if name == "" && i+1 < len(fields) {
				name = fields[i+1]
			}
			if name != "" {
				defines[name] = ""
			}
		}
	}
	return defines
}

// flatten makes the conditional sections (IfModule, IfDefine, IfVersion)
// transparent: Apache is running, their content is assumed active. The
// negative ones (<IfModule !ssl_module>) are left out by the parser.
func flatten(directives []directive) []directive {
	var out []directive
	for _, d := range directives {
		if isConditional(d.Name) && d.Block != nil {
			out = append(out, flatten(d.Block)...)
			continue
		}
		out = append(out, d)
	}
	return out
}

func isConditional(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), "if")
}

type apacheReader struct {
	fs afero.Fs
	// read reads a file with the rights of the Apache process.
	read func(file string) ([]byte, error)
	root string
	// defines are the variables of Define and -D, substituted in ${NAME}.
	defines map[string]string
	// errs are the included files that cannot be read: reported, the rest
	// of the configuration still read.
	errs []error
}

func (r *apacheReader) path(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(r.root, p)
}

// file parses an Apache configuration file, its includes expanded.
func (r *apacheReader) file(path string, depth int) ([]directive, error) {
	b, err := r.read(path)
	if err != nil {
		return nil, err
	}
	lines, err := apacheLines(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	directives, rest, err := r.parse(path, lines, "", depth, true)
	if err != nil {
		return nil, err
	}
	if len(rest) > 0 {
		return nil, fmt.Errorf("%s: unexpected %q", path, rest[0])
	}
	return directives, nil
}

var apacheVariable = regexp.MustCompile(`\$\{([^}]+)\}`)

// substitute replaces the ${NAME} of the defined variables; the others,
// such as those of /etc/apache2/envvars, are kept.
func (r *apacheReader) substitute(line string) string {
	return apacheVariable.ReplaceAllStringFunc(line, func(m string) string {
		if v, ok := r.defines[m[2:len(m)-1]]; ok {
			return v
		}
		return m
	})
}

// parse reads directives until the closing tag of section, when given.
// The content of an inactive section, a negative condition such as
// <IfModule !ssl_module>, is read but left out: no include, no Define.
func (r *apacheReader) parse(file string, lines []string, section string, depth int, active bool) ([]directive, []string, error) {
	directives := []directive{}
	for len(lines) > 0 {
		line := lines[0]
		lines = lines[1:]
		if strings.HasPrefix(line, "</") {
			name := strings.TrimSuffix(strings.TrimPrefix(line, "</"), ">")
			if !strings.EqualFold(name, section) {
				return nil, nil, fmt.Errorf("%s: unexpected %q", file, line)
			}
			return directives, lines, nil
		}
		if active {
			line = r.substitute(line)
		}
		if strings.HasPrefix(line, "<") {
			fields := apacheFields(strings.TrimSuffix(strings.TrimPrefix(line, "<"), ">"))
			if len(fields) == 0 {
				continue
			}
			negative := isConditional(fields[0]) && len(fields) > 1 && strings.HasPrefix(fields[1], "!")
			block, rest, err := r.parse(file, lines, fields[0], depth, active && !negative)
			if err != nil {
				return nil, nil, err
			}
			lines = rest
			if active && !negative {
				directives = append(directives, directive{Name: fields[0], Args: fields[1:], Block: block, File: file})
			}
			continue
		}
		if !active {
			continue
		}
		fields := apacheFields(line)
		d := directive{Name: fields[0], Args: fields[1:], File: file}
		switch {
		case (strings.EqualFold(d.Name, "Include") || strings.EqualFold(d.Name, "IncludeOptional")) && len(d.Args) == 1:
			included, err := r.include(d, depth)
			if err != nil {
				return nil, nil, err
			}
			directives = append(directives, included...)
			continue
		case strings.EqualFold(d.Name, "ServerRoot") && len(d.Args) == 1:
			r.root = d.Args[0]
		case strings.EqualFold(d.Name, "Define") && len(d.Args) > 0:
			r.defines[d.Args[0]] = strings.Join(d.Args[1:], " ")
		case strings.EqualFold(d.Name, "UnDefine") && len(d.Args) > 0:
			delete(r.defines, d.Args[0])
		}
		directives = append(directives, d)
	}
	if section != "" {
		return nil, nil, fmt.Errorf("%s: missing </%s>", file, section)
	}
	return directives, nil, nil
}

// include reads the files of an Include: a file, a directory or a glob.
func (r *apacheReader) include(d directive, depth int) ([]directive, error) {
	if depth >= maxIncludeDepth {
		return nil, fmt.Errorf("%s: includes nested too deep", d.File)
	}
	pattern := r.path(d.Args[0])
	files, _ := afero.Glob(r.fs, pattern) // only ErrBadPattern
	if info, err := r.fs.Stat(pattern); err == nil && info.IsDir() {
		files, _ = afero.Glob(r.fs, filepath.Join(pattern, "*"))
	}
	optional := strings.EqualFold(d.Name, "IncludeOptional")
	if len(files) == 0 && !optional && !strings.ContainsAny(pattern, "*?[") {
		r.errs = append(r.errs, fmt.Errorf("%s: include %s not found", d.File, pattern))
	}
	slices.Sort(files)
	var out []directive
	for _, file := range files {
		if info, err := r.fs.Stat(file); err == nil && info.IsDir() {
			continue
		}
		included, err := r.file(file, depth+1)
		if err != nil {
			if strings.Contains(err.Error(), "nested too deep") {
				return nil, err
			}
			r.errs = append(r.errs, err)
			continue
		}
		out = append(out, included...)
	}
	return out, nil
}

// apacheLines returns the logical lines of a file: comments and blank lines
// dropped, lines ending with a backslash joined.
func apacheLines(f io.Reader) ([]string, error) {
	var lines []string
	var current strings.Builder
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if current.Len() == 0 && (line == "" || strings.HasPrefix(line, "#")) {
			continue
		}
		if cont, ok := strings.CutSuffix(line, `\`); ok {
			current.WriteString(cont + " ")
			continue
		}
		current.WriteString(line)
		lines = append(lines, strings.TrimSpace(current.String()))
		current.Reset()
	}
	if current.Len() > 0 {
		lines = append(lines, strings.TrimSpace(current.String()))
	}
	return lines, scanner.Err()
}

// apacheFields splits a line into words, double-quoted ones kept whole.
func apacheFields(line string) []string {
	var fields []string
	for {
		line = strings.TrimLeft(line, " \t")
		switch {
		case line == "":
			return fields
		case line[0] == '"':
			end := strings.IndexByte(line[1:], '"')
			if end < 0 {
				return append(fields, line[1:])
			}
			fields = append(fields, line[1:end+1])
			line = line[end+2:]
		default:
			end := strings.IndexAny(line, " \t")
			if end < 0 {
				return append(fields, line)
			}
			fields = append(fields, line[:end])
			line = line[end:]
		}
	}
}

func apacheVhost(section directive) snapshot.Vhost {
	v := snapshot.Vhost{ConfigFile: section.File}
	for _, addr := range section.Args {
		v.Listen = appendUnique(v.Listen, netaddr.Listen(addr, "80"))
	}
	var visit func(directives []directive)
	visit = func(directives []directive) {
		for _, d := range directives {
			if d.Block != nil {
				visit(d.Block)
				continue
			}
			switch strings.ToLower(d.Name) {
			case "servername":
				if len(d.Args) > 0 {
					name, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(d.Args[0], "https://"), "http://"), ":")
					v.ServerNames = appendUnique(v.ServerNames, name)
				}
			case "serveralias":
				for _, name := range d.Args {
					v.ServerNames = appendUnique(v.ServerNames, name)
				}
			case "documentroot":
				if len(d.Args) > 0 && v.Root == "" {
					v.Root = strings.TrimSuffix(d.Args[0], "/")
				}
			case "proxypass", "proxypassmatch":
				if target := proxyPassTarget(d.Args); target != "" {
					v.Upstreams = appendUnique(v.Upstreams, apacheTarget(target))
				}
			case "sethandler":
				if len(d.Args) > 0 && strings.HasPrefix(d.Args[0], "proxy:") {
					v.Upstreams = appendUnique(v.Upstreams, apacheTarget(strings.TrimPrefix(d.Args[0], "proxy:")))
				}
			}
		}
	}
	visit(section.Block)
	return v
}

// proxyPassTarget returns the target of "ProxyPass [path] url [key=value…]":
// the path is absent in a <Location>; "!" excludes a path.
func proxyPassTarget(args []string) string {
	for _, arg := range args {
		if arg == "!" {
			return ""
		}
		if strings.Contains(arg, "://") || strings.HasPrefix(arg, "unix:") {
			return arg
		}
	}
	return ""
}

// apacheTarget keeps what identifies a proxy target, as for nginx:
// "unix:/run/php/php8.2-fpm.sock|fcgi://localhost" is the socket,
// "fcgi://127.0.0.1:9000/var/www/$1" the FastCGI address.
func apacheTarget(target string) string {
	if socket, _, ok := strings.Cut(target, "|"); ok && strings.HasPrefix(socket, "unix:") {
		return socket
	}
	if rest, ok := strings.CutPrefix(target, "fcgi://"); ok {
		host, _, _ := strings.Cut(rest, "/")
		return host
	}
	return passTarget("proxy_pass", target)
}

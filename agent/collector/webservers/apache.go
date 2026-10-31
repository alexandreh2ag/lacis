package webservers

import (
	"bufio"
	stdContext "context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/context"
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
	r := &apacheReader{fs: ctx.Fs, root: root}
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

// flatten makes the conditional sections (IfModule, IfDefine, IfVersion)
// transparent: Apache is running, their content is assumed active.
func flatten(directives []directive) []directive {
	var out []directive
	for _, d := range directives {
		if strings.HasPrefix(strings.ToLower(d.Name), "if") && d.Block != nil {
			out = append(out, flatten(d.Block)...)
			continue
		}
		out = append(out, d)
	}
	return out
}

type apacheReader struct {
	fs   afero.Fs
	root string
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
	f, err := r.fs.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	lines, err := apacheLines(f)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	directives, rest, err := r.parse(path, lines, "", depth)
	if err != nil {
		return nil, err
	}
	if len(rest) > 0 {
		return nil, fmt.Errorf("%s: unexpected %q", path, rest[0])
	}
	return directives, nil
}

// parse reads directives until the closing tag of section, when given.
func (r *apacheReader) parse(file string, lines []string, section string, depth int) ([]directive, []string, error) {
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
		if strings.HasPrefix(line, "<") {
			fields := apacheFields(strings.TrimSuffix(strings.TrimPrefix(line, "<"), ">"))
			if len(fields) == 0 {
				continue
			}
			block, rest, err := r.parse(file, lines, fields[0], depth)
			if err != nil {
				return nil, nil, err
			}
			directives = append(directives, directive{Name: fields[0], Args: fields[1:], Block: block, File: file})
			lines = rest
			continue
		}
		fields := apacheFields(line)
		d := directive{Name: fields[0], Args: fields[1:], File: file}
		if (strings.EqualFold(d.Name, "Include") || strings.EqualFold(d.Name, "IncludeOptional")) && len(d.Args) == 1 {
			included, err := r.include(d, depth)
			if err != nil {
				return nil, nil, err
			}
			directives = append(directives, included...)
			continue
		}
		if strings.EqualFold(d.Name, "ServerRoot") && len(d.Args) == 1 {
			r.root = d.Args[0]
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
func apacheLines(f afero.File) ([]string, error) {
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
		v.Listen = appendUnique(v.Listen, apacheListen(addr))
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

// apacheListen normalizes a VirtualHost address: "*:80" and "_default_:443"
// listen on every IPv4 address.
func apacheListen(addr string) string {
	for _, wildcard := range []string{"*", "_default_"} {
		if port, ok := strings.CutPrefix(addr, wildcard+":"); ok {
			return "0.0.0.0:" + port
		}
		if addr == wildcard {
			return "0.0.0.0:80"
		}
	}
	if !strings.Contains(addr, ":") || strings.HasSuffix(addr, "]") {
		return addr + ":80"
	}
	return addr
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

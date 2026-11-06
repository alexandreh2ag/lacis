package webservers

import (
	stdContext "context"
	"errors"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/netaddr"
	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/spf13/afero"
)

// NginxPackages hold the nginx version, first installed wins.
var NginxPackages = []string{"nginx", "nginx-core", "nginx-full", "nginx-light", "nginx-extras", "nginx-common", "openresty"}

// passDirectives send a request to a backend.
var passDirectives = []string{"fastcgi_pass", "proxy_pass", "uwsgi_pass", "scgi_pass", "grpc_pass"}

// NginxConfig is the main file of nginx when its command line names none
// and no -p prefix holds one.
const NginxConfig = "/etc/nginx/nginx.conf"

// readNginx reads the configuration of the nginx master p. nginx is never
// run (nginx -T would run any binary named nginx as root): its files are
// parsed, read with the rights of p; the version comes from the package.
func readNginx(stdCtx stdContext.Context, ctx *context.Context, sys *system.System, p system.Process) (snapshot.Webserver, error) {
	ws := snapshot.Webserver{}
	packages, err := sys.Packages(stdCtx)
	if err != nil {
		return ws, err
	}
	ws.Version, _ = packages.Version(NginxPackages...)

	main := nginxMainConfig(ctx.Fs, p)
	r := &nginxReader{
		read:    func(file string) ([]byte, error) { return sys.ReadAs(stdCtx, p, file) },
		glob:    func(pattern string) ([]string, error) { return globFiles(ctx.Fs, pattern) },
		confDir: path.Dir(main),
	}
	directives, err := r.file(main, 0)
	if err != nil {
		return ws, err
	}
	for _, http := range blocks(directives, "http") {
		for _, d := range http.Block {
			switch d.Name {
			case "server":
				ws.Vhosts = append(ws.Vhosts, nginxVhost(d))
			case "upstream":
				if len(d.Args) == 1 {
					ws.Upstreams = append(ws.Upstreams, nginxUpstream(d))
				}
			}
		}
	}
	return ws, errors.Join(r.errs...)
}

// nginxMainConfig returns the main file of the master p: its -c, relative
// to its -p prefix; else the conf/nginx.conf of its prefix, given or
// guessed from the binary (/opt/nginx/sbin/nginx), when it exists; else
// NginxConfig. The master title holds its command line: "nginx: master
// process /usr/sbin/nginx -c /etc/nginx/nginx.conf".
func nginxMainConfig(fsys afero.Fs, p system.Process) string {
	prefix := flagValue(p.Cmdline, "-p")
	if conf := flagValue(p.Cmdline, "-c"); conf != "" {
		if path.IsAbs(conf) {
			return conf
		}
		if prefix == "" {
			prefix = path.Dir(path.Dir(p.Exe))
		}
		return path.Join(prefix, conf)
	}
	if prefix != "" {
		return path.Join(prefix, "conf", "nginx.conf")
	}
	if _, err := fsys.Stat(NginxConfig); err == nil || p.Exe == "" {
		return NginxConfig
	}
	if conf := path.Join(path.Dir(path.Dir(p.Exe)), "conf", "nginx.conf"); conf != "/conf/nginx.conf" {
		if _, err := fsys.Stat(conf); err == nil {
			return conf
		}
	}
	return NginxConfig
}

// globFiles returns the files matching pattern, directories left out.
func globFiles(fsys afero.Fs, pattern string) ([]string, error) {
	matches, err := afero.Glob(fsys, pattern)
	files := matches[:0]
	for _, m := range matches {
		if info, err := fsys.Stat(m); err != nil || !info.IsDir() {
			files = append(files, m)
		}
	}
	return files, err
}

// blocks returns the directives named name of a block.
func blocks(directives []directive, name string) []directive {
	var out []directive
	for _, d := range directives {
		if d.Name == name && d.Block != nil {
			out = append(out, d)
		}
	}
	return out
}

func nginxVhost(server directive) snapshot.Vhost {
	v := snapshot.Vhost{ConfigFile: server.File}
	for _, d := range server.Block {
		switch d.Name {
		case "listen":
			if len(d.Args) > 0 {
				v.Listen = appendUnique(v.Listen, netaddr.Listen(d.Args[0], "80"))
			}
		case "server_name":
			for _, name := range d.Args {
				if name != "_" && name != "" {
					v.ServerNames = appendUnique(v.ServerNames, name)
				}
			}
		case "root":
			if len(d.Args) == 1 {
				v.Root = d.Args[0]
			}
		}
	}
	if len(v.Listen) == 0 {
		v.Listen = []string{"0.0.0.0:80"}
	}
	vars := setVariables(server.Block)
	var visit func(directives []directive, path string, inLocation bool)
	visit = func(directives []directive, path string, inLocation bool) {
		for _, d := range directives {
			switch {
			case d.Name == "location" && d.Block != nil:
				visit(d.Block, locationPath(d.Args), true)
			case d.Name == "if" && d.Block != nil:
				visit(d.Block, path, inLocation)
			case slices.Contains(passDirectives, d.Name) && len(d.Args) > 0:
				target := passTarget(d.Name, expand(d.Args[0], vars))
				v.Upstreams = appendUnique(v.Upstreams, target)
				fpm := d.Name != "proxy_pass" && d.Name != "grpc_pass"
				v.AddLocation(passedPath(path, fpm), target)
			case d.Name == "root" && v.Root == "" && len(d.Args) == 1 && inLocation:
				v.Root = d.Args[0] // only in a location
			}
		}
	}
	visit(server.Block, "", false)
	return v
}

// passedPath is the path a backend is passed: a FastCGI backend under a
// regexp (~ \.php$) runs the app of the vhost whatever the path called,
// try_files rewriting it.
func passedPath(path string, fpm bool) string {
	if fpm && strings.HasPrefix(path, "~") {
		return ""
	}
	return path
}

// locationPath writes the arguments of an nginx location as a path
// matcher: "/api", "=/health", "~^/(a|b)", "~(?i)\.jpg$" for ~*; a named
// location (@app) and / serve any path.
func locationPath(args []string) string {
	modifier, uri := "", ""
	switch len(args) {
	case 1:
		uri = args[0]
		for _, m := range []string{"^~", "~*", "~", "="} {
			if rest, ok := strings.CutPrefix(uri, m); ok {
				modifier, uri = m, rest
				break
			}
		}
	case 2:
		modifier, uri = args[0], args[1]
	default:
		return ""
	}
	switch {
	case strings.HasPrefix(uri, "@"):
		return ""
	case modifier == "=":
		return "=" + uri
	case modifier == "~":
		return "~" + uri
	case modifier == "~*":
		return "~(?i)" + uri
	case uri == "/":
		return ""
	}
	return uri
}

// setVariables returns the variables set by "set $name value" in a server
// block and its locations. Without evaluating the requests, a variable set
// in several places keeps its first value.
func setVariables(directives []directive) map[string]string {
	vars := map[string]string{}
	add := func(d directive) {
		if d.Name == "set" && len(d.Args) == 2 && strings.HasPrefix(d.Args[0], "$") {
			name := strings.Trim(strings.TrimPrefix(d.Args[0], "$"), "{}")
			if _, seen := vars[name]; !seen && !strings.Contains(d.Args[1], "$") {
				vars[name] = d.Args[1]
			}
		}
	}
	for _, d := range directives {
		add(d)
	}
	walk(directives, add)
	return vars
}

var variable = regexp.MustCompile(`\$\{?(\w+)\}?`)

// expand replaces the variables of vars in s; the others are kept.
func expand(s string, vars map[string]string) string {
	return variable.ReplaceAllStringFunc(s, func(m string) string {
		if v, ok := vars[variable.FindStringSubmatch(m)[1]]; ok {
			return v
		}
		return m
	})
}

// walk calls fn on every directive of the locations of a block, nested ones
// included.
func walk(directives []directive, fn func(d directive)) {
	for _, d := range directives {
		if d.Name == "location" || d.Name == "if" {
			for _, inner := range d.Block {
				fn(inner)
			}
			walk(d.Block, fn)
		} else if d.Block == nil {
			fn(d)
		}
	}
}

func nginxUpstream(d directive) snapshot.Upstream {
	u := snapshot.Upstream{Name: d.Args[0], Servers: []string{}}
	for _, s := range d.Block {
		if s.Name == "server" && len(s.Args) > 0 {
			u.Servers = append(u.Servers, s.Args[0])
		}
	}
	return u
}

// passTarget keeps what identifies the backend of a pass directive: the
// scheme and host of a URL, without its path.
func passTarget(name, target string) string {
	if name == "fastcgi_pass" || name == "uwsgi_pass" || name == "scgi_pass" || !strings.Contains(target, "://") {
		return target
	}
	// http://unix:/run/app.sock:/path
	if rest, ok := strings.CutPrefix(target[strings.Index(target, "://")+3:], "unix:"); ok {
		socket, _, _ := strings.Cut(rest, ":")
		return "unix:" + socket
	}
	// A variable may follow the host: https://127.0.0.1$request_uri.
	scheme, rest, _ := strings.Cut(target, "://")
	host := rest
	if i := strings.IndexAny(rest, "/$?"); i >= 0 {
		host = rest[:i]
	}
	if strings.HasSuffix(host, ":") || strings.HasPrefix(rest, "$") {
		return target // the host or the port is a variable: http://$backend
	}
	u, err := url.Parse(scheme + "://" + host)
	if err != nil || u.Host == "" {
		return target
	}
	return u.Scheme + "://" + u.Host
}

func appendUnique(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}

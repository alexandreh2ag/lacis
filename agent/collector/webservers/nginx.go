package webservers

import (
	"bytes"
	stdContext "context"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/snapshot"
)

// NginxPackages hold the nginx version, first installed wins.
var NginxPackages = []string{"nginx", "nginx-core", "nginx-full", "nginx-light", "nginx-extras", "nginx-common", "openresty"}

// passDirectives send a request to a backend.
var passDirectives = []string{"fastcgi_pass", "proxy_pass", "uwsgi_pass", "scgi_pass", "grpc_pass"}

// readNginx dumps the configuration of the nginx master p with nginx -T.
func readNginx(stdCtx stdContext.Context, ctx *context.Context, sys *system.System, p system.Process) (snapshot.Webserver, error) {
	ws := snapshot.Webserver{}
	packages, err := sys.Packages(stdCtx)
	if err != nil {
		return ws, err
	}
	ws.Version, _ = packages.Version(NginxPackages...)

	args := []string{"-T"}
	// The master title holds its command line: "nginx: master process
	// /usr/sbin/nginx -c /etc/nginx/nginx.conf".
	if conf := flagValue(p.Cmdline, "-c"); conf != "" {
		args = append(args, "-c", conf)
	}
	out, err := ctx.Executor.Run(stdCtx, p.Exe, args...)
	if err != nil {
		return ws, err
	}
	directives, err := parseDump(bytes.NewReader(out))
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
	return ws, nil
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
				v.Listen = appendUnique(v.Listen, nginxListen(d.Args[0]))
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
	walk(server.Block, func(d directive) {
		switch {
		case slices.Contains(passDirectives, d.Name) && len(d.Args) > 0:
			v.Upstreams = appendUnique(v.Upstreams, passTarget(d.Name, expand(d.Args[0], vars)))
		case d.Name == "root" && v.Root == "" && len(d.Args) == 1:
			v.Root = d.Args[0] // only in a location
		}
	})
	return v
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

// nginxListen normalizes a listen address: "80" and "*:80" listen on every
// IPv4 address, a host without port on 80.
func nginxListen(s string) string {
	switch {
	case strings.HasPrefix(s, "unix:"), strings.HasPrefix(s, "["):
		return s
	case !strings.Contains(s, ":"):
		if isPort(s) {
			return "0.0.0.0:" + s
		}
		return s + ":80"
	case strings.HasPrefix(s, "*:"):
		return "0.0.0.0:" + strings.TrimPrefix(s, "*:")
	}
	return s
}

func isPort(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
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

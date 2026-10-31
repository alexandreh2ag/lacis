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

const (
	TypeHAProxy = "haproxy"
	// HAProxyConfig is read when the process names none with -f.
	HAProxyConfig = "/etc/haproxy/haproxy.cfg"
)

// readHAProxy reads the configuration files of the HAProxy process p: each
// -f, a file or a directory of .cfg files.
func readHAProxy(stdCtx stdContext.Context, ctx *context.Context, sys *system.System, p system.Process) (snapshot.Webserver, error) {
	ws := snapshot.Webserver{}
	packages, err := sys.Packages(stdCtx)
	if err != nil {
		return ws, err
	}
	ws.Version, _ = packages.Version("haproxy")

	var files []string
	fields := strings.Fields(strings.Join(p.Cmdline, " "))
	for i, f := range fields {
		if f == "-f" && i+1 < len(fields) {
			files = append(files, fields[i+1])
		}
	}
	if len(files) == 0 {
		files = []string{HAProxyConfig}
	}
	h := &haproxyConfig{}
	var errs []error
	for _, path := range files {
		if info, err := ctx.Fs.Stat(path); err == nil && info.IsDir() {
			cfgs, _ := afero.Glob(ctx.Fs, filepath.Join(path, "*.cfg")) // only ErrBadPattern
			slices.Sort(cfgs)
			for _, cfg := range cfgs {
				errs = append(errs, h.read(ctx.Fs, cfg))
			}
			continue
		}
		errs = append(errs, h.read(ctx.Fs, path))
	}
	ws.Frontends, ws.Backends = h.frontends, h.backends
	return ws, errors.Join(errs...)
}

type haproxyConfig struct {
	frontends []snapshot.Frontend
	backends  []snapshot.Backend
}

// read parses the sections of an haproxy.cfg file. A listen section gives a
// frontend and the backend of the same name.
func (h *haproxyConfig) read(fsys afero.Fs, path string) error {
	f, err := fsys.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	var defaultMode string
	var frontend *snapshot.Frontend
	var backend *snapshot.Backend
	var section string
	flush := func() {
		if frontend != nil {
			h.frontends = append(h.frontends, *frontend)
		}
		if backend != nil {
			h.backends = append(h.backends, *backend)
		}
		frontend, backend = nil, nil
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line, _, _ := strings.Cut(scanner.Text(), "#")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		keyword := fields[0]
		switch keyword {
		case "global", "defaults", "frontend", "backend", "listen", "peers", "resolvers", "userlist", "program", "cache", "mailers", "http-errors", "ring":
			flush()
			section = keyword
			name := ""
			if len(fields) > 1 {
				name = fields[1]
			}
			if keyword == "frontend" || keyword == "listen" {
				frontend = &snapshot.Frontend{Name: name, Mode: defaultMode}
			}
			if keyword == "backend" || keyword == "listen" {
				backend = &snapshot.Backend{Name: name, Mode: defaultMode, Servers: []snapshot.BackendServer{}}
			}
			if keyword == "listen" {
				frontend.DefaultBackend = name
			}
			continue
		}
		args := fields[1:]
		switch {
		case keyword == "mode" && len(args) > 0:
			switch section {
			case "defaults":
				defaultMode = args[0]
			default:
				if frontend != nil {
					frontend.Mode = args[0]
				}
				if backend != nil {
					backend.Mode = args[0]
				}
			}
		case keyword == "bind" && frontend != nil && len(args) > 0:
			for _, addr := range strings.Split(args[0], ",") {
				frontend.Bind = appendUnique(frontend.Bind, haproxyBind(addr))
			}
		case keyword == "default_backend" && frontend != nil && len(args) > 0:
			frontend.DefaultBackend = args[0]
		case keyword == "use_backend" && frontend != nil && len(args) > 0:
			frontend.Backends = appendUnique(frontend.Backends, args[0])
		case keyword == "server" && backend != nil && len(args) > 1:
			backend.Servers = append(backend.Servers, snapshot.BackendServer{Name: args[0], Address: args[1]})
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	flush()
	return nil
}

// haproxyBind normalizes a bind address: "*:80" and ":80" listen on every
// IPv4 address; "ipv4@", "ipv6@" prefixes are dropped.
func haproxyBind(addr string) string {
	if _, rest, ok := strings.Cut(addr, "@"); ok && !strings.HasPrefix(addr, "unix@") {
		addr = rest
	}
	switch {
	case strings.HasPrefix(addr, "unix@"):
		return "unix:" + strings.TrimPrefix(addr, "unix@")
	case strings.HasPrefix(addr, "/"):
		return "unix:" + addr
	case strings.HasPrefix(addr, "*:"):
		return "0.0.0.0:" + strings.TrimPrefix(addr, "*:")
	case strings.HasPrefix(addr, ":") && !strings.HasPrefix(addr, "::"):
		return "0.0.0.0" + addr
	}
	return addr
}

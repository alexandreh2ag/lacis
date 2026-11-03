package webservers

import (
	"bufio"
	"bytes"
	stdContext "context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/netaddr"
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
	read := func(file string) {
		b, err := sys.ReadAs(stdCtx, p, file)
		if err != nil {
			errs = append(errs, err)
			return
		}
		errs = append(errs, h.read(file, b))
	}
	for _, path := range files {
		if info, err := ctx.Fs.Stat(path); err == nil && info.IsDir() {
			cfgs, _ := afero.Glob(ctx.Fs, filepath.Join(path, "*.cfg")) // only ErrBadPattern
			slices.Sort(cfgs)
			for _, cfg := range cfgs {
				read(cfg)
			}
			continue
		}
		read(path)
	}
	ws.Frontends, ws.Backends = h.frontends, h.backends
	return ws, errors.Join(errs...)
}

type haproxyConfig struct {
	frontends []snapshot.Frontend
	backends  []snapshot.Backend
}

// haproxyFile is the state of the parsing of one file: the section being
// read and the mode of its defaults.
type haproxyFile struct {
	section     string
	defaultMode string
	frontend    *snapshot.Frontend
	backend     *snapshot.Backend
}

// haproxySections start a section; listen gives a frontend and the backend
// of the same name.
var haproxySections = []string{"global", "defaults", "frontend", "backend", "listen", "peers", "resolvers", "userlist", "program", "cache", "mailers", "http-errors", "ring"}

// haproxyKeywords handle the keywords read in a section, args being those
// following the keyword; at least minArgs of them.
var haproxyKeywords = map[string]struct {
	minArgs int
	handle  func(f *haproxyFile, args []string)
}{
	"mode": {1, func(f *haproxyFile, args []string) {
		if f.section == "defaults" {
			f.defaultMode = args[0]
			return
		}
		if f.frontend != nil {
			f.frontend.Mode = args[0]
		}
		if f.backend != nil {
			f.backend.Mode = args[0]
		}
	}},
	"bind": {1, func(f *haproxyFile, args []string) {
		if f.frontend != nil {
			for _, addr := range strings.Split(args[0], ",") {
				f.frontend.Bind = appendUnique(f.frontend.Bind, netaddr.Listen(addr, ""))
			}
		}
	}},
	"default_backend": {1, func(f *haproxyFile, args []string) {
		if f.frontend != nil {
			f.frontend.DefaultBackend = args[0]
		}
	}},
	"use_backend": {1, func(f *haproxyFile, args []string) {
		if f.frontend != nil {
			f.frontend.Backends = appendUnique(f.frontend.Backends, args[0])
		}
	}},
	"server": {2, func(f *haproxyFile, args []string) {
		if f.backend != nil {
			f.backend.Servers = append(f.backend.Servers, snapshot.BackendServer{Name: args[0], Address: args[1]})
		}
	}},
}

// start begins a section, the previous one flushed into h.
func (f *haproxyFile) start(h *haproxyConfig, keyword string, args []string) {
	f.flush(h)
	f.section = keyword
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	if keyword == "frontend" || keyword == "listen" {
		f.frontend = &snapshot.Frontend{Name: name, Mode: f.defaultMode}
	}
	if keyword == "backend" || keyword == "listen" {
		f.backend = &snapshot.Backend{Name: name, Mode: f.defaultMode, Servers: []snapshot.BackendServer{}}
	}
	if keyword == "listen" {
		f.frontend.DefaultBackend = name
	}
}

func (f *haproxyFile) flush(h *haproxyConfig) {
	if f.frontend != nil {
		h.frontends = append(h.frontends, *f.frontend)
	}
	if f.backend != nil {
		h.backends = append(h.backends, *f.backend)
	}
	f.frontend, f.backend = nil, nil
}

// read parses the sections of an haproxy.cfg file.
func (h *haproxyConfig) read(path string, content []byte) error {
	f := &haproxyFile{}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		line, _, _ := strings.Cut(scanner.Text(), "#")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		keyword, args := fields[0], fields[1:]
		if slices.Contains(haproxySections, keyword) {
			f.start(h, keyword, args)
			continue
		}
		if k, ok := haproxyKeywords[keyword]; ok && len(args) >= k.minArgs {
			k.handle(f, args)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	f.flush(h)
	return nil
}

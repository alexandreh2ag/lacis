package apps

import (
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/spf13/afero"
)

// Sources of an app runtime.
const (
	RuntimeSourceFPMPool  = "fpm_pool"
	RuntimeSourceProcess  = "process"
	RuntimeSourceLauncher = "launcher"
	RuntimeSourceNvmrc    = "nvmrc"
)

// runtime finds the runtime an app really runs on, by priority: the PHP-FPM
// pool its vhosts send to, the binary of its Node process, the PHP of its
// launcher command, then its .nvmrc resolved against the installed nvm
// versions.
func (c *collection) runtime(a *app) *snapshot.AppRuntime {
	for _, ref := range a.vhosts {
		if ref.proxied {
			continue
		}
		for _, target := range expandUpstreams(ref.ws, ref.vhost.Upstreams) {
			if r := c.fpmRuntime(target); r != nil {
				return r
			}
		}
	}
	for _, p := range a.processes {
		if path.Base(p.Exe) == "node" {
			return &snapshot.AppRuntime{Type: "node", Version: c.runtimeVersion("node", p.Exe), Path: p.Exe, Source: RuntimeSourceProcess}
		}
	}
	for _, l := range a.launchers {
		fields := strings.Fields(l.Command)
		if len(fields) == 0 || !strings.HasPrefix(path.Base(fields[0]), "php") {
			continue
		}
		bin := fields[0]
		if !path.IsAbs(bin) {
			bin = "/usr/bin/" + bin
		}
		if resolved, err := c.sys.Resolve(bin); err == nil {
			bin = resolved
		}
		return &snapshot.AppRuntime{Type: "php", Version: c.runtimeVersion("php", bin), Path: bin, Source: RuntimeSourceLauncher}
	}
	return c.nvmrcRuntime(a)
}

// fpmRuntime returns the PHP of the FPM pool listening on target.
func (c *collection) fpmRuntime(target string) *snapshot.AppRuntime {
	addr := normalizeAddr(target)
	for _, svc := range c.out.Services {
		if svc.Type != "php-fpm" {
			continue
		}
		for _, pool := range svc.Pools {
			if normalizeAddr(pool.Listen) == addr {
				return &snapshot.AppRuntime{Type: "php", Version: svc.Version, Source: RuntimeSourceFPMPool}
			}
		}
	}
	return nil
}

// runtimeVersion returns the version of the runtime collected at path.
func (c *collection) runtimeVersion(typ, binary string) string {
	for _, r := range c.out.Runtimes {
		if r.Type == typ && r.Path == binary {
			return r.Version
		}
	}
	return ""
}

// nvmrcRuntime resolves the .nvmrc of an app against the nvm versions
// installed, those of its launcher user first: "20" gives the highest 20.x.
func (c *collection) nvmrcRuntime(a *app) *snapshot.AppRuntime {
	b, err := afero.ReadFile(c.ctx.Fs, path.Join(a.resolved, ".nvmrc"))
	if err != nil {
		return nil
	}
	wanted := strings.TrimPrefix(strings.TrimSpace(string(b)), "v")
	if wanted == "" {
		return nil
	}
	user := ""
	if len(a.launchers) > 0 {
		user = a.launchers[0].User
	}
	var best *snapshot.Runtime
	for i, r := range c.out.Runtimes {
		if r.Type != "node" || r.Manager != "nvm" || (r.Version != wanted && !strings.HasPrefix(r.Version, wanted+".")) {
			continue
		}
		mine := user != "" && strings.Contains(r.Path, "/"+user+"/")
		bestMine := best != nil && user != "" && strings.Contains(best.Path, "/"+user+"/")
		if best == nil || (mine && !bestMine) || (mine == bestMine && newer(r.Version, best.Version)) {
			best = &c.out.Runtimes[i]
		}
	}
	if best == nil {
		// An alias such as lts/* or a version not installed is kept as is.
		return &snapshot.AppRuntime{Type: "node", Version: wanted, Source: RuntimeSourceNvmrc}
	}
	return &snapshot.AppRuntime{Type: "node", Version: best.Version, Path: best.Path, Source: RuntimeSourceNvmrc}
}

// newer compares dotted versions numerically.
func newer(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range min(len(pa), len(pb)) {
		na, _ := strconv.Atoi(pa[i])
		nb, _ := strconv.Atoi(pb[i])
		if na != nb {
			return na > nb
		}
	}
	return len(pa) > len(pb)
}

// expandUpstreams replaces the nginx upstream names of targets by their
// servers: fastcgi_pass espacefpm → unix:/run/php/espace.sock.
func expandUpstreams(ws *snapshot.Webserver, targets []string) []string {
	var out []string
	for _, t := range targets {
		name := t
		if _, rest, ok := strings.Cut(t, "://"); ok {
			name = rest
		}
		i := slices.IndexFunc(ws.Upstreams, func(u snapshot.Upstream) bool { return u.Name == name })
		if i < 0 {
			out = append(out, t)
			continue
		}
		out = append(out, ws.Upstreams[i].Servers...)
	}
	return out
}

// normalizeAddr makes two spellings of one local address equal: the scheme
// is dropped and every loopback name becomes 127.0.0.1, so that
// "localhost:9000", "fcgi://127.0.0.1:9000" and "[::1]:9000" match.
func normalizeAddr(s string) string {
	if strings.HasPrefix(s, "unix:") {
		return s
	}
	if _, rest, ok := strings.Cut(s, "://"); ok {
		s = rest
	}
	s, _, _ = strings.Cut(s, "/")
	host, port := s, ""
	if i := strings.LastIndex(s, ":"); i >= 0 && !strings.HasSuffix(s, "]") {
		host, port = s[:i], s[i+1:]
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" || host == "::1" || strings.HasPrefix(host, "127.") {
		host = "127.0.0.1"
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]" // IPv6
	}
	if port == "" {
		return host
	}
	return host + ":" + port
}

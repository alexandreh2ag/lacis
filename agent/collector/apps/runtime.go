package apps

import (
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/netaddr"
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
			r := c.runtimeAt("node", p.Exe)
			return &snapshot.AppRuntime{Type: "node", Version: r.Version, PackageVersion: r.PackageVersion, Path: p.Exe, Source: RuntimeSourceProcess}
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
		r := c.runtimeAt("php", bin)
		return &snapshot.AppRuntime{Type: "php", Version: r.Version, PackageVersion: r.PackageVersion, Path: bin, Source: RuntimeSourceLauncher}
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
				return &snapshot.AppRuntime{Type: "php", Version: svc.Version, PackageVersion: svc.PackageVersion, Source: RuntimeSourceFPMPool}
			}
		}
	}
	return nil
}

// runtimeAt returns the runtime collected at path; zero when none was.
func (c *collection) runtimeAt(typ, binary string) snapshot.Runtime {
	for _, r := range c.out.Runtimes {
		if r.Type == typ && r.Path == binary {
			return r
		}
	}
	return snapshot.Runtime{}
}

// nvmrcRuntime resolves the .nvmrc of an app against the nvm versions
// installed, those of its launcher user first: "20" gives the highest 20.x.
func (c *collection) nvmrcRuntime(a *app) *snapshot.AppRuntime {
	wanted := readNvmrc(c.ctx.Fs, a.resolved)
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
	return &snapshot.AppRuntime{Type: "node", Version: best.Version, PackageVersion: best.PackageVersion, Path: best.Path, Source: RuntimeSourceNvmrc}
}

// maxNvmrc bounds the .nvmrc read: it holds one version.
const maxNvmrc = 256

// nvmrcRe is a version or an alias nvm accepts: 20, v20.11.1, lts/iron,
// lts/*, node, stable.
var nvmrcRe = regexp.MustCompile(`^(v?\d+(\.\d+){0,2}|lts/[\w*-]+|node|stable)$`)

// readNvmrc returns the version of the .nvmrc of root without its "v",
// empty when there is none or it has not the form of a version. It is read
// confined to root, like the .git directory: a link to another file is
// refused.
func readNvmrc(fsys afero.Fs, root string) string {
	if _, err := fsys.Stat(path.Join(root, ".nvmrc")); err != nil {
		return ""
	}
	appFS, closeRoot, err := openRoot(fsys, root)
	if err != nil {
		return ""
	}
	defer closeRoot()
	b, err := readSmall(appFS, ".nvmrc", maxNvmrc)
	if err != nil {
		return ""
	}
	wanted := strings.TrimSpace(string(b))
	if !nvmrcRe.MatchString(wanted) {
		return ""
	}
	return strings.TrimPrefix(wanted, "v")
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
	return netaddr.Key(s)
}

// Package apps collects the apps deployed on the host: project roots found
// from the vhosts, the local targets of the proxies, the launchers, the
// processes and the scan paths, with their name, role, framework, runtime
// and code version.
package apps

import (
	stdContext "context"
	"errors"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/collector"
	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/spf13/afero"
)

const Name = "apps"

// Markers make a directory an app root.
var Markers = []string{"composer.json", "package.json", ".git", DeployInfoJSON, DeployInfoYAML, DeployInfoYML}

// EnvKeywords give the env of an app when they name a directory of its
// path. Any other env, such as "pp", is set by a version file.
var EnvKeywords = []string{"prod", "production", "preprod", "staging", "release", "preview"}

// maxWalkUp bounds the search of a root above a directory.
const maxWalkUp = 8

func init() {
	collector.Register(Name, func(*context.Context) (collector.Collector, error) { return &Collector{}, nil })
}

type Collector struct{}

func (*Collector) Name() string { return Name }

// DependsOn: the vhosts give the web apps, the runtimes and services
// (containers included) their runtime.
func (*Collector) DependsOn() []string {
	return []string{"webservers", "runtimes", "services", "docker"}
}

// Applicable is always true: the scan paths are always looked at.
func (*Collector) Applicable(*context.Context, *system.System) bool { return true }

// app gathers what points to one app root.
type app struct {
	path      string // as seen by the source, e.g. a "current" link
	resolved  string
	vhosts    []vhostRef
	launchers []system.Launcher
	processes []system.Process
}

type vhostRef struct {
	ws    *snapshot.Webserver
	vhost *snapshot.Vhost
	// proxied is true when the app is the process a proxy target of the
	// vhost reaches: the other targets of the vhost, such as the FastCGI of
	// its root, belong to other apps.
	proxied bool
}

type collection struct {
	ctx       *context.Context
	sys       *system.System
	out       *snapshot.Snapshot
	processes []system.Process
	listeners []system.Listener
	apps      map[string]*app
	order     []string
	errs      []error
}

func (*Collector) Collect(stdCtx stdContext.Context, ctx *context.Context, sys *system.System, out *snapshot.Snapshot) error {
	c := &collection{ctx: ctx, sys: sys, out: out, apps: map[string]*app{}}
	var err error
	if c.processes, err = sys.Processes(); err != nil {
		c.errs = append(c.errs, err)
	}
	if c.listeners, err = sys.Listeners(); err != nil {
		c.errs = append(c.errs, err)
	}
	launchers, err := sys.Launchers()
	if err != nil {
		c.errs = append(c.errs, err)
	}

	for i := range out.Webservers {
		ws := &out.Webservers[i]
		for j := range ws.Vhosts {
			c.fromVhost(ws, &ws.Vhosts[j])
		}
	}
	ctx.Logger.Debug("launchers read", "count", len(launchers))
	for _, l := range launchers {
		if a := c.launcherApp(l); a != nil {
			a.launchers = append(a.launchers, l)
			for _, pid := range l.PIDs {
				if p, ok := c.process(pid); ok {
					a.processes = appendProcess(a.processes, p)
				}
			}
			continue
		}
		ctx.Logger.Debug("launcher in no app", "launcher", l.Name, "file", l.File, "directory", l.Directory, "command", l.Command)
	}
	for _, p := range c.processes {
		if isAppProcess(p) {
			if a := c.add(p.Cwd); a != nil {
				a.processes = appendProcess(a.processes, p)
			}
		}
	}
	for _, root := range ctx.Config.Collectors.Apps.ScanPaths {
		if err := stdCtx.Err(); err != nil {
			return err
		}
		c.scan(root, 0)
	}

	detectors := detectors(ctx.Config.Collectors.Apps.Frameworks)
	for _, key := range c.order {
		out.Apps = append(out.Apps, c.build(c.apps[key], detectors))
	}
	slices.SortFunc(out.Apps, func(a, b snapshot.App) int { return strings.Compare(a.Path, b.Path) })
	return errors.Join(c.errs...)
}

// fromVhost adds the app of a vhost root, and the apps of the local
// processes it proxies to.
func (c *collection) fromVhost(ws *snapshot.Webserver, v *snapshot.Vhost) {
	ref := vhostRef{ws: ws, vhost: v}
	if v.Root != "" {
		if a := c.add(v.Root); a != nil {
			a.vhosts = append(a.vhosts, ref)
		}
	}
	for _, target := range expandUpstreams(ws, v.Upstreams) {
		p, ok := c.localProcess(target)
		if !ok {
			continue
		}
		if a := c.add(p.Cwd); a != nil {
			a.vhosts = append(a.vhosts, vhostRef{ws: ws, vhost: v, proxied: true})
			a.processes = appendProcess(a.processes, p)
		}
	}
}

// localProcess returns the process listening on a loopback target.
func (c *collection) localProcess(target string) (system.Process, bool) {
	addr := normalizeAddr(target)
	host, port, ok := strings.Cut(addr, ":")
	if !ok || host != "127.0.0.1" {
		return system.Process{}, false
	}
	for _, l := range c.listeners {
		if l.Proto != system.ProtoTCP || l.PID == 0 || !strings.HasSuffix(l.Address, ":"+port) {
			continue
		}
		if a := normalizeAddr(l.Address); a == addr || strings.HasPrefix(l.Address, "0.0.0.0:") || strings.HasPrefix(l.Address, "[::]:") {
			for _, p := range c.processes {
				if p.PID == l.PID && !p.InContainer {
					return p, true
				}
			}
		}
	}
	return system.Process{}, false
}

// launcherApp returns the app a program runs in: its directory, or else
// the first absolute path of its command inside an app, such as the script
// of "php /var/www/app/bin/console messenger:consume".
func (c *collection) launcherApp(l system.Launcher) *app {
	if l.Directory != "" {
		return c.add(l.Directory)
	}
	for _, field := range strings.Fields(l.Command) {
		if path.IsAbs(field) {
			if a := c.add(path.Dir(field)); a != nil {
				return a
			}
		}
	}
	return nil
}

// isAppProcess tells the processes whose directory may be an app: Node and
// PHP command line programs.
func isAppProcess(p system.Process) bool {
	base := path.Base(p.Exe)
	return !p.InContainer && p.Cwd != "" && p.Cwd != "/" &&
		(base == "node" || (strings.HasPrefix(base, "php") && !strings.HasPrefix(base, "php-fpm")))
}

func appendProcess(list []system.Process, p system.Process) []system.Process {
	if slices.ContainsFunc(list, func(q system.Process) bool { return q.PID == p.PID }) {
		return list
	}
	return append(list, p)
}

// add returns the app whose root contains dir, created on first sight; nil
// when dir is in no app.
func (c *collection) add(dir string) *app {
	if dir == "" {
		return nil
	}
	literal, resolved := c.findRoot(dir)
	if literal == "" {
		return nil
	}
	if a, ok := c.apps[resolved]; ok {
		return a
	}
	a := &app{path: literal, resolved: resolved}
	c.apps[resolved] = a
	c.order = append(c.order, resolved)
	return a
}

// findRoot walks up from dir to the first directory holding a marker. The
// root keeps the path of dir, links included; resolved is its target.
func (c *collection) findRoot(dir string) (literal, resolved string) {
	d := path.Clean(dir)
	if c.excluded(d) {
		return "", ""
	}
	for range maxWalkUp {
		if tooHigh(d) {
			return "", ""
		}
		res, err := c.sys.Resolve(d)
		if err != nil {
			res = d
		}
		if hasMarker(c.ctx.Fs, res) {
			return d, res
		}
		d = path.Dir(d)
	}
	return "", ""
}

// excluded rejects a directory under a hidden one (~/.cache, ~/.npm…) or
// under one of collectors.apps.exclude (node_modules, vendor…): a package
// there is not an app.
func (c *collection) excluded(d string) bool {
	for _, part := range strings.Split(strings.Trim(d, "/"), "/") {
		if strings.HasPrefix(part, ".") || slices.Contains(c.ctx.Config.Collectors.Apps.Exclude, part) {
			return true
		}
	}
	return false
}

// tooHigh rejects the system and home directories: a dotfiles repository
// is not an app.
func tooHigh(d string) bool {
	parts := strings.Split(strings.Trim(d, "/"), "/")
	switch {
	case d == "/" || len(parts) == 1:
		return true
	case len(parts) == 2 && parts[0] == "home":
		return true
	}
	return false
}

func hasMarker(fsys afero.Fs, dir string) bool {
	for _, m := range Markers {
		if _, err := fsys.Stat(path.Join(dir, m)); err == nil {
			return true
		}
	}
	return false
}

// scan looks for app roots under dir, down to collectors.apps.max_depth. A
// directory with a "current" link is a deployment: only current is an app,
// its releases are not.
func (c *collection) scan(dir string, depth int) {
	if depth > c.ctx.Config.Collectors.Apps.MaxDepth || slices.Contains(c.ctx.Config.Collectors.Apps.Exclude, path.Base(dir)) {
		return
	}
	if hasMarker(c.ctx.Fs, dir) {
		c.add(dir)
		return
	}
	if _, err := c.sys.Readlink(path.Join(dir, "current")); err == nil {
		c.add(path.Join(dir, "current"))
		return
	}
	entries, err := afero.ReadDir(c.ctx.Fs, dir)
	if err != nil {
		return // missing scan path, or not readable
	}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			c.scan(path.Join(dir, e.Name()), depth+1)
		}
	}
}

// build describes an app.
func (c *collection) build(a *app, detectors []detector) snapshot.App {
	fsys := c.ctx.Fs
	labels := c.ctx.Config.Labels
	out := snapshot.App{
		Path:         a.path,
		Name:         dirName(a.path),
		Role:         snapshot.AppRoleWorker,
		Project:      labels.Project,
		Env:          labels.Env,
		Framework:    framework(fsys, a.resolved, detectors),
		Dependencies: []snapshot.Dependency{},
	}
	if a.resolved != a.path {
		out.ResolvedPath = a.resolved
	}

	info, warnings := readDeployInfo(fsys, a.resolved)
	for _, w := range warnings {
		c.errs = append(c.errs, collector.Warn(w))
	}
	repo, err := readGit(fsys, a.resolved)
	if err != nil {
		c.errs = append(c.errs, collector.Warn(err))
	}
	switch {
	case info != nil:
		out.Code = info.code()
	case repo != nil:
		out.Code = repo.code()
	}
	if repo != nil && repo.remote != "" {
		out.Name = repoName(repo.remote)
	}
	out.Env = firstNonEmpty(pathEnv(a.path), pathEnv(a.resolved), out.Env)
	if info != nil {
		out.Name = firstNonEmpty(info.App, out.Name)
		out.Project = firstNonEmpty(info.Project, out.Project)
		out.Env = firstNonEmpty(info.Env, out.Env)
	}

	for _, ref := range a.vhosts {
		for _, name := range ref.vhost.ServerNames {
			if !slices.Contains(out.Vhosts, name) {
				out.Vhosts = append(out.Vhosts, name)
			}
		}
	}
	slices.Sort(out.Vhosts)
	slices.SortFunc(a.launchers, func(x, y system.Launcher) int { return strings.Compare(x.Name, y.Name) })
	out.Launchers = []snapshot.Launcher{}
	for _, l := range a.launchers {
		launcher := l.Launcher
		launcher.Role = snapshot.AppRoleWorker
		if c.listens(c.tree(l.PIDs)) {
			launcher.Role = snapshot.AppRoleWeb
		}
		out.Launchers = append(out.Launchers, launcher)
	}
	if len(a.vhosts) > 0 || c.listens(a.processes) {
		out.Role = snapshot.AppRoleWeb
	}
	out.Runtime = c.runtime(a)
	return out
}

// process returns the process pid.
func (c *collection) process(pid int) (system.Process, bool) {
	i := slices.IndexFunc(c.processes, func(p system.Process) bool { return p.PID == pid })
	if i < 0 {
		return system.Process{}, false
	}
	return c.processes[i], true
}

// tree returns the processes pids and their descendants: the server a
// "bash -c npm start" program runs is a grandchild.
func (c *collection) tree(pids []int) []system.Process {
	var out []system.Process
	queue := slices.Clone(pids)
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		if p, ok := c.process(pid); ok && !slices.ContainsFunc(out, func(q system.Process) bool { return q.PID == pid }) {
			out = append(out, p)
			for _, child := range c.processes {
				if child.PPID == pid {
					queue = append(queue, child.PID)
				}
			}
		}
	}
	return out
}

// listens reports whether one of processes listens on TCP: an app serving
// requests without a webserver in front.
func (c *collection) listens(processes []system.Process) bool {
	for _, l := range c.listeners {
		if l.Proto == system.ProtoTCP && slices.ContainsFunc(processes, func(p system.Process) bool { return p.PID == l.PID }) {
			return true
		}
	}
	return false
}

// pathEnv returns the deepest directory of p that is an env keyword:
// /home/svc/var/www/example.fr/api/production/www is production.
func pathEnv(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if slices.Contains(EnvKeywords, parts[i]) {
			return parts[i]
		}
	}
	return ""
}

// genericDirs and envDirs do not name an app: its name is the first
// directory above them.
var (
	genericDirs = []string{"www", "current", "releases", "public", "html", "htdocs", "web", "src", "dist", "build", "app"}
	// envDirs skip more names than EnvKeywords give envs: a "pp" directory
	// does not name an app, but its env needs a version file.
	envDirs  = append([]string{"pp", "stage", "recette", "qualif", "demo", "dev", "develop", "test", "testing", "uat", "int", "integration"}, EnvKeywords...)
	buildDir = regexp.MustCompile(`^v?\d+([.\-_]\d+)*$`)
)

// dirName names an app after its directory, skipping the generic ones,
// the environments, numbered ones too (dev1, staging2), and the build
// numbers: /var/www/client/current and
// /home/svc/var/www/example.fr/api/production/www give client and api.
func dirName(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	// Never a top level directory nor a user name: /srv, /home/deploy.
	lowest := 1
	if parts[0] == "home" {
		lowest = 2
	}
	for i := len(parts) - 1; i >= lowest; i-- {
		part := parts[i]
		if !slices.Contains(genericDirs, part) && !slices.Contains(envDirs, strings.TrimRight(part, "0123456789")) && !buildDir.MatchString(part) {
			return part
		}
	}
	return path.Base(p)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

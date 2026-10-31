// Package envfiles reads the configuration of each app, in the order its
// framework loads it, and turns the connection values into dependencies,
// credentials removed. Only the allowed keys are read, the denied ones
// never; a value that cannot be parsed is sent as its key alone.
package envfiles

import (
	"bufio"
	stdContext "context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alexandreh2ag/lacis/agent/collector"
	"github.com/alexandreh2ag/lacis/agent/config"
	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/dsn"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/spf13/afero"
)

const Name = "envfiles"

// HostsFile is read before the DNS, as the resolver of the host does.
const HostsFile = "/etc/hosts"

// LookupTimeout bounds the DNS resolution of one name.
const LookupTimeout = 2 * time.Second

// InfoKeys are sent as they are, for information: they never give the env.
var InfoKeys = []string{"APP_ENV", "NODE_ENV"}

func init() {
	collector.Register(Name, func(*context.Context) (collector.Collector, error) { return &Collector{}, nil })
}

type Collector struct{}

func (*Collector) Name() string { return Name }

// DependsOn: the apps give the roots, frameworks and launchers.
func (*Collector) DependsOn() []string { return []string{"apps"} }

func (*Collector) Applicable(*context.Context, *system.System) bool { return true }

// sourceSet is how a framework loads its configuration.
type sourceSet func(r *reader, e env, root string) error

// sources by framework name; other apps read the .env of their root.
var sources = map[string]sourceSet{
	"symfony": symfonySources,
	"next":    nextSources,
}

func (*Collector) Collect(stdCtx stdContext.Context, ctx *context.Context, sys *system.System, out *snapshot.Snapshot) error {
	launchers, _ := sys.Launchers() // reported by the apps collector
	r := &reader{fs: ctx.Fs, cfg: ctx.Config.Collectors}
	res := newResolver(ctx, stdCtx)
	var errs []error
	for i := range out.Apps {
		if err := stdCtx.Err(); err != nil {
			return err
		}
		app := &out.Apps[i]
		e, blocks, err := r.read(app, launchers)
		if err != nil {
			errs = append(errs, collector.Warn(fmt.Errorf("%s: %w", app.Path, err)))
		}
		app.Dependencies = append(dependencies(e, r.cfg.Envfiles, res), blockDependencies(blocks, r.cfg.Envfiles, res)...)
		for _, key := range InfoKeys {
			if v, ok := e[key]; ok && v.value != "" {
				if app.Info == nil {
					app.Info = map[string]string{}
				}
				app.Info[key] = v.value
			}
		}
	}
	return errors.Join(errs...)
}

type reader struct {
	fs  afero.Fs
	cfg config.CollectorsConfig
}

// read merges the configuration of an app: its framework files, those of
// a declarative detector, then the environment of its launchers. A
// structured configuration gives blocks rather than keys.
func (r *reader) read(app *snapshot.App, launchers []system.Launcher) (env, []block, error) {
	root := app.ResolvedPath
	if root == "" {
		root = app.Path
	}
	e := env{}
	var errs []error
	name := ""
	if app.Framework != nil {
		name = app.Framework.Name
	}
	var blocks []block
	switch set, builtin := sources[name]; {
	case name == "zend1":
		var err error
		blocks, err = r.zend1Blocks(root, app.Env)
		errs = append(errs, err)
	case builtin:
		errs = append(errs, set(r, e, root))
	case r.declared(name) != nil:
		errs = append(errs, r.declaredSources(e, root, r.declared(name)))
	default:
		errs = append(errs, r.dotenv(e, path.Join(root, ".env")))
	}
	for _, l := range app.Launchers {
		for _, sl := range launchers {
			if sl.Name != l.Name {
				continue
			}
			for _, key := range sortedKeys(sl.Environment) {
				e.set(key, sl.Environment[key], sl.File, 0)
			}
		}
	}
	e.resolve()
	return e, blocks, errors.Join(errs...)
}

func (r *reader) declared(name string) *config.FrameworkDetector {
	for i, d := range r.cfg.Apps.Frameworks {
		if d.Name == name {
			return &r.cfg.Apps.Frameworks[i]
		}
	}
	return nil
}

// declaredSources reads the env sources of a declarative detector, in
// order; the other formats are read by the structured readers.
func (r *reader) declaredSources(e env, root string, d *config.FrameworkDetector) error {
	var errs []error
	for _, src := range d.Config {
		if src.Format != "env" {
			continue
		}
		files, _ := afero.Glob(r.fs, path.Join(root, src.Path)) // only ErrBadPattern
		slices.Sort(files)
		for _, file := range files {
			errs = append(errs, r.dotenv(e, file))
		}
	}
	return errors.Join(errs...)
}

// dotenv reads a .env file when it exists.
func (r *reader) dotenv(e env, file string) error {
	b, err := afero.ReadFile(r.fs, file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return e.parseDotenv(file, string(b))
}

// symfonySources loads like Symfony Dotenv: .env.local.php alone when
// composer dump-env wrote it, else .env, .env.local (skipped in test),
// .env.<APP_ENV>, .env.<APP_ENV>.local.
func symfonySources(r *reader, e env, root string) error {
	if b, err := afero.ReadFile(r.fs, path.Join(root, ".env.local.php")); err == nil {
		e.parsePHPEnv(path.Join(root, ".env.local.php"), string(b))
		return nil
	}
	errs := []error{r.dotenv(e, path.Join(root, ".env"))}
	appEnv := func() string {
		e.resolve()
		if v, ok := e["APP_ENV"]; ok && v.value != "" {
			return v.value
		}
		return "dev"
	}
	if appEnv() != "test" {
		errs = append(errs, r.dotenv(e, path.Join(root, ".env.local")))
	}
	current := appEnv()
	errs = append(errs, r.dotenv(e, path.Join(root, ".env."+current)))
	if current != "test" {
		errs = append(errs, r.dotenv(e, path.Join(root, ".env."+current+".local")))
	}
	return errors.Join(errs...)
}

// nextSources loads like next start: .env, .env.production, .env.local,
// .env.production.local, the last winning.
func nextSources(r *reader, e env, root string) error {
	var errs []error
	for _, name := range []string{".env", ".env.production", ".env.local", ".env.production.local"} {
		errs = append(errs, r.dotenv(e, path.Join(root, name)))
	}
	return errors.Join(errs...)
}

// allowed tells the keys to read: allowed and not denied.
func allowed(key string, cfg config.EnvfilesConfig) bool {
	match := func(patterns []string) bool {
		return slices.ContainsFunc(patterns, func(p string) bool {
			ok, _ := path.Match(p, key)
			return ok
		})
	}
	return match(cfg.AllowKeys) && !match(cfg.DenyKeys)
}

// dependencies turns the allowed values of e into dependencies, sorted by
// key: URLs and DSNs, and the X_HOST keys, with the port of X_PORT.
func dependencies(e env, cfg config.EnvfilesConfig, res *resolver) []snapshot.Dependency {
	out := []snapshot.Dependency{}
	for _, key := range sortedKeys(e) {
		if !allowed(key, cfg) || slices.Contains(InfoKeys, key) || strings.HasSuffix(key, "_PORT") {
			continue
		}
		v := e[key]
		if v.value == "" {
			continue
		}
		source := snapshot.DependencySource{File: v.file, Line: v.line}
		all, err := dsn.ParseAll(v.value)
		if errors.Is(err, dsn.ErrNotNetwork) {
			continue
		}
		if err != nil || v.unresolved {
			out = append(out, snapshot.Dependency{Key: key, Source: source, Unparsed: true})
			continue
		}
		for _, parsed := range all {
			if prefix, ok := strings.CutSuffix(key, "_HOST"); ok && parsed.Port == 0 {
				if port, err := strconv.Atoi(e[prefix+"_PORT"].value); err == nil {
					parsed.Port = port
				}
			}
			out = append(out, dependency(key, source, parsed, res))
		}
	}
	return out
}

// dependency describes a parsed target; a URL with several hosts gives one
// dependency per host, under the same key.
func dependency(key string, source snapshot.DependencySource, d dsn.DSN, res *resolver) snapshot.Dependency {
	return snapshot.Dependency{Key: key, Source: source, Scheme: d.Scheme, Port: d.Port, Path: d.Path, Target: res.target(d)}
}

// blockDependencies turns the blocks of a structured configuration into
// dependencies keyed by their path: a dsn or url key is parsed as a URL,
// else host, port and dbname are read. Denied keys are never read.
func blockDependencies(blocks []block, cfg config.EnvfilesConfig, res *resolver) []snapshot.Dependency {
	out := []snapshot.Dependency{}
	for _, b := range blocks {
		get := func(names ...string) (value, bool) {
			for _, n := range names {
				if v, ok := b.values[n]; ok && v.value != "" && !denied(n, cfg) {
					return v, true
				}
			}
			return value{}, false
		}
		if v, ok := get("dsn", "url"); ok {
			source := snapshot.DependencySource{File: v.file, Line: v.line, KeyPath: b.path}
			all, err := dsn.ParseAll(v.value)
			if errors.Is(err, dsn.ErrNotNetwork) {
				continue
			}
			if err != nil {
				out = append(out, snapshot.Dependency{Key: b.path, Source: source, Unparsed: true})
				continue
			}
			for _, d := range all {
				out = append(out, dependency(b.path, source, d, res))
			}
			continue
		}
		v, ok := get("host", "hostname", "server")
		if !ok {
			continue
		}
		source := snapshot.DependencySource{File: v.file, Line: v.line, KeyPath: b.path}
		host, port, err := dsn.ParseHostPort(v.value)
		if err != nil {
			out = append(out, snapshot.Dependency{Key: b.path, Source: source, Unparsed: true})
			continue
		}
		d := dsn.DSN{Host: host, Port: port}
		if adapter, ok := get("adapter"); ok {
			d.Scheme = adapterSchemes[strings.ToLower(adapter.value)]
		}
		if p, ok := get("port"); ok && d.Port == 0 {
			d.Port, _ = strconv.Atoi(p.value)
		}
		if d.Port == 0 {
			d.Port = dsn.DefaultPort(d.Scheme)
		}
		if name, ok := get("dbname", "database"); ok {
			d.Path = name.value
		}
		if d.Scheme == "sqlite" {
			continue
		}
		out = append(out, dependency(b.path, source, d, res))
	}
	return out
}

// denied tells a key of deny_keys, compared in upper case: the leaves of a
// structured configuration are lower case.
func denied(key string, cfg config.EnvfilesConfig) bool {
	return slices.ContainsFunc(cfg.DenyKeys, func(p string) bool {
		ok, _ := path.Match(p, strings.ToUpper(key))
		return ok
	})
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// resolver qualifies the targets as the host of the app sees them:
// /etc/hosts first, then the DNS, each name once per collection.
type resolver struct {
	ctx    *context.Context
	stdCtx stdContext.Context
	hosts  map[string][]string
	cache  map[string]*snapshot.Target
}

func newResolver(ctx *context.Context, stdCtx stdContext.Context) *resolver {
	return &resolver{ctx: ctx, stdCtx: stdCtx, hosts: readHosts(ctx.Fs), cache: map[string]*snapshot.Target{}}
}

func (r *resolver) target(d dsn.DSN) *snapshot.Target {
	switch d.Kind() {
	case dsn.KindSocket:
		return &snapshot.Target{Kind: string(dsn.KindSocket), Value: d.Socket}
	case dsn.KindIP:
		return &snapshot.Target{Kind: string(dsn.KindIP), Value: d.Host}
	}
	if t, ok := r.cache[d.Host]; ok {
		return t
	}
	t := &snapshot.Target{Kind: string(dsn.KindName), Value: d.Host}
	if ips, ok := r.hosts[d.Host]; ok {
		t.Resolved, t.Resolver = ips, snapshot.ResolverHosts
	} else {
		lookupCtx, cancel := stdContext.WithTimeout(r.stdCtx, LookupTimeout)
		ips, err := r.ctx.Kernel.LookupHost(lookupCtx, d.Host)
		cancel()
		if err != nil || len(ips) == 0 {
			t.Resolver = snapshot.ResolverFailed
		} else {
			slices.Sort(ips)
			t.Resolved, t.Resolver = ips, snapshot.ResolverDNS
		}
	}
	r.cache[d.Host] = t
	return t
}

// readHosts maps the names of /etc/hosts to their addresses.
func readHosts(fsys afero.Fs) map[string][]string {
	hosts := map[string][]string{}
	f, err := fsys.Open(HostsFile)
	if err != nil {
		return hosts
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line, _, _ := strings.Cut(scanner.Text(), "#")
		fields := strings.Fields(line)
		if len(fields) < 2 || net.ParseIP(fields[0]) == nil {
			continue
		}
		for _, name := range fields[1:] {
			name = strings.ToLower(name)
			if !slices.Contains(hosts[name], fields[0]) {
				hosts[name] = append(hosts[name], fields[0])
			}
		}
	}
	return hosts
}

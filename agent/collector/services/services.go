// Package services collects the services running on the host: databases,
// caches, queues, search engines, secret stores, object stores (MinIO, Silo)
// and PHP-FPM with its pools. A service is a running process, its version
// comes from its package and its listening sockets from its process tree.
package services

import (
	stdContext "context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/collector"
	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/spf13/afero"
)

const Name = "services"

// Service types.
const (
	TypeMySQL         = "mysql"
	TypeMariaDB       = "mariadb"
	TypePostgreSQL    = "postgresql"
	TypeRedis         = "redis"
	TypePHPFPM        = "php-fpm"
	TypeElasticsearch = "elasticsearch"
	TypeKeycloak      = "keycloak"
	TypeClickHouse    = "clickhouse"
	TypeRabbitMQ      = "rabbitmq"
	TypeMongoDB       = "mongodb"
	TypeVarnish       = "varnish"
	TypeVault         = "vault"
	TypeMinIO         = "minio"
	// TypeSilo is the community fork of MinIO by Pigsty, its binary renamed.
	TypeSilo = "silo"
)

func init() {
	collector.Register(Name, func(*context.Context) (collector.Collector, error) { return &Collector{}, nil })
}

// detector recognizes the main process of a service type.
type detector struct {
	typ   string
	match func(p system.Process) bool
	// packages hold the version, first installed wins; %s is replaced by
	// the version found in the binary path, when versioned.
	packages []string
	// describe fills what is specific to the type; optional.
	describe func(c *detection, svc *snapshot.Service) error
	// versionRe reads the version printed by "<binary> --version" (or
	// versionArgs), asked when no package gives it; optional.
	versionRe   *regexp.Regexp
	versionArgs []string
}

// mysqlVersion reads "mysqld  Ver 8.0.39 for Linux" and "mariadbd  Ver
// 10.11.6-MariaDB-0+deb12u1 for debian-linux-gnu".
var mysqlVersion = regexp.MustCompile(`\bVer (\d+(?:\.\d+)+)`)

// releaseVersion reads the release of MinIO and Silo: "minio version
// RELEASE.2024-01-16T16-07-38Z (commit-id=…)".
var releaseVersion = regexp.MustCompile(`\b(RELEASE\.\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}Z)`)

// packageRelease reads the package version of a MinIO release:
// 20240116160738.0.0 is RELEASE.2024-01-16T16-07-38Z.
var packageRelease = regexp.MustCompile(`^(\d{4})(\d{2})(\d{2})(\d{2})(\d{2})(\d{2})(?:\.|$)`)

var (
	pgBinary  = regexp.MustCompile(`/(?:usr/lib/postgresql/|usr/pgsql-)(\d+)/bin/postgres$`)
	fpmBinary = regexp.MustCompile(`php-fpm(\d+\.?\d*)$`)
	remiFPM   = regexp.MustCompile(`/opt/remi/php(\d+)/`)
	// debianCluster reads /etc/postgresql/<version>/<cluster>/postgresql.conf.
	debianCluster = regexp.MustCompile(`^/etc/postgresql/([^/]+)/([^/]+)/`)
)

func base(p system.Process) string { return filepath.Base(p.Exe) }

// argv0 is the base name the process was started with, which tells apart
// the commands of a multi-call binary: /usr/bin/clickhouse-server is a
// link to /usr/bin/clickhouse.
func argv0(p system.Process) string {
	if len(p.Cmdline) == 0 {
		return ""
	}
	first, _, _ := strings.Cut(p.Cmdline[0], " ")
	return filepath.Base(first)
}

func cmdlineContains(p system.Process, s string) bool {
	return slices.ContainsFunc(p.Cmdline, func(arg string) bool { return strings.Contains(arg, s) })
}

var detectors = []detector{
	{
		typ:       TypeMariaDB,
		match:     func(p system.Process) bool { return base(p) == "mariadbd" },
		packages:  []string{"mariadb-server", "MariaDB-server", "mariadb-server-core"},
		describe:  describeMySQL,
		versionRe: mysqlVersion,
	},
	{
		typ:       TypeMySQL,
		match:     func(p system.Process) bool { return base(p) == "mysqld" },
		packages:  []string{"mariadb-server", "MariaDB-server", "mysql-server", "mysql-community-server", "percona-server-server"},
		describe:  describeMySQL,
		versionRe: mysqlVersion,
	},
	{
		typ:       TypePostgreSQL,
		match:     func(p system.Process) bool { return base(p) == "postgres" },
		packages:  []string{"postgresql-%s", "postgresql%s-server"},
		describe:  describePostgreSQL,
		versionRe: regexp.MustCompile(`\(PostgreSQL\) (\d+(?:\.\d+)+)`),
	},
	{
		typ:       TypeRedis,
		match:     func(p system.Process) bool { return base(p) == "redis-server" },
		packages:  []string{"redis-server", "redis", "redis-tools"},
		versionRe: regexp.MustCompile(`\bv=(\d+(?:\.\d+)+)`),
	},
	{
		typ:      TypePHPFPM,
		match:    func(p system.Process) bool { return strings.HasPrefix(base(p), "php-fpm") },
		packages: []string{"php%s-fpm", "php%s-php-fpm", "php-fpm"},
		describe: describePHPFPM,
	},
	{
		typ: TypeElasticsearch,
		match: func(p system.Process) bool {
			// Elasticsearch 8 starts a CLI process, parent of the server:
			// whichever matches first, the ports of the tree are kept.
			return base(p) == "java" && (cmdlineContains(p, "org.elasticsearch.bootstrap.Elasticsearch") || cmdlineContains(p, "-Des.path.home="))
		},
		packages: []string{"elasticsearch"},
	},
	{
		typ: TypeKeycloak,
		match: func(p system.Process) bool {
			return base(p) == "java" && cmdlineContains(p, "kc.home.dir")
		},
		packages: []string{"keycloak"},
		describe: describeKeycloak,
	},
	{
		typ: TypeClickHouse,
		match: func(p system.Process) bool {
			return base(p) == "clickhouse-server" || argv0(p) == "clickhouse-server" ||
				(base(p) == "clickhouse" && len(p.Cmdline) > 1 && p.Cmdline[1] == "server")
		},
		packages: []string{"clickhouse-server", "clickhouse-common-static"},
	},
	{
		typ:      TypeMongoDB,
		match:    func(p system.Process) bool { return base(p) == "mongod" },
		packages: []string{"mongodb-org-server", "mongodb-server", "mongodb"},
		describe: describeMongoDB,
	},
	{
		typ:   TypeVarnish,
		match: func(p system.Process) bool { return base(p) == "varnishd" },
		// The cache worker is a child varnishd: one instance.
		packages:    []string{"varnish", "varnish-plus"},
		versionRe:   regexp.MustCompile(`\(varnish-(\d+(?:\.\d+)+)`),
		versionArgs: []string{"-V"},
	},
	{
		typ: TypeVault,
		match: func(p system.Process) bool {
			return base(p) == "vault" && len(p.Cmdline) > 1 && p.Cmdline[1] == "server"
		},
		packages:    []string{"vault", "vault-enterprise"},
		versionRe:   regexp.MustCompile(`\bVault v(\d+(?:\.\d+)+)`),
		versionArgs: []string{"version"},
	},
	{
		typ:       TypeMinIO,
		match:     objectStore("minio"),
		packages:  []string{"minio"},
		describe:  describeObjectStore,
		versionRe: releaseVersion,
	},
	{
		typ:       TypeSilo,
		match:     objectStore("silo"),
		packages:  []string{"silo"},
		describe:  describeObjectStore,
		versionRe: releaseVersion,
	},
	{
		typ:      TypeRabbitMQ,
		match:    func(p system.Process) bool { return base(p) == "beam.smp" && cmdlineContains(p, "rabbit") },
		packages: []string{"rabbitmq-server"},
	},
}

type Collector struct {
	found []detection
}

// detection is the main process of a service and what describes it.
type detection struct {
	detector *detector
	process  system.Process
	// pathVersion is the version read in the binary path: "16", "8.2".
	pathVersion string
	// pkg is the package the version comes from.
	pkg string
	ctx *context.Context
	// sys and stdCtx are those of Collect: describe reads files with them.
	sys    *system.System
	stdCtx stdContext.Context
}

func (*Collector) Name() string { return Name }

// Applicable looks for the main process of a known service.
func (c *Collector) Applicable(ctx *context.Context, sys *system.System) bool {
	processes, err := sys.Processes()
	if err != nil {
		return true // reported by Collect
	}
	byPID := map[int]system.Process{}
	for _, p := range processes {
		byPID[p.PID] = p
	}
	c.found = nil
	for _, p := range processes {
		if p.InContainer {
			continue // reported by the docker collector
		}
		for i := range detectors {
			d := &detectors[i]
			if !d.match(p) {
				continue
			}
			// A worker forked by the main process is not another instance.
			if parent, ok := byPID[p.PPID]; ok && d.match(parent) {
				break
			}
			c.found = append(c.found, detection{detector: d, process: p, pathVersion: pathVersion(p.Exe), ctx: ctx})
			break
		}
	}
	return len(c.found) > 0
}

// pathVersion reads the major version of a versioned binary path.
func pathVersion(exe string) string {
	for _, re := range []*regexp.Regexp{pgBinary, fpmBinary} {
		if m := re.FindStringSubmatch(exe); m != nil {
			return m[1]
		}
	}
	if m := remiFPM.FindStringSubmatch(exe); m != nil {
		return m[1]
	}
	return ""
}

func (c *Collector) Collect(stdCtx stdContext.Context, ctx *context.Context, sys *system.System, out *snapshot.Snapshot) error {
	processes, err := sys.Processes()
	if err != nil {
		return err
	}
	var errs []error
	packages, err := sys.Packages(stdCtx)
	if err != nil {
		errs = append(errs, err) // services still reported, without version
	}
	listeners, err := sys.Listeners()
	if err != nil {
		errs = append(errs, err)
	}
	children := map[int][]int{}
	for _, p := range processes {
		children[p.PPID] = append(children[p.PPID], p.PID)
	}

	for i := range c.found {
		d := &c.found[i]
		d.sys, d.stdCtx = sys, stdCtx
		version, packageVersion := d.version(packages)
		svc := snapshot.Service{
			Type:           d.detector.typ,
			Version:        version,
			PackageVersion: packageVersion,
			Listen:         listen(listeners, tree(children, d.process.PID)),
		}
		if svc.Version == "" && d.detector.versionRe != nil {
			svc.Version = d.binaryVersion()
		}
		if d.detector.describe != nil {
			if err := d.detector.describe(d, &svc); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", svc.Type, err))
			}
		}
		out.Services = append(out.Services, svc)
	}
	nameDuplicates(out.Services)
	slices.SortFunc(out.Services, func(a, b snapshot.Service) int {
		return strings.Compare(a.Type+"\x00"+a.Instance, b.Type+"\x00"+b.Instance)
	})
	return errors.Join(errs...)
}

// binaryVersion asks the binary of the process its version, for a service
// installed without package: as nobody, else as the user of the process.
func (d *detection) binaryVersion() string {
	args := d.detector.versionArgs
	if len(args) == 0 {
		args = []string{"--version"}
	}
	out, err := d.sys.Probe(d.stdCtx, &d.process, d.process.Exe, args...)
	if err != nil {
		return ""
	}
	if m := d.detector.versionRe.FindSubmatch(out); m != nil {
		return string(m[1])
	}
	return ""
}

// version returns the upstream version of the first installed package, and
// its version as the package manager gives it.
func (d *detection) version(packages system.Packages) (version, packageVersion string) {
	for _, name := range d.detector.packages {
		if strings.Contains(name, "%s") {
			if d.pathVersion == "" {
				continue
			}
			name = fmt.Sprintf(name, d.pathVersion)
		}
		if v, ok := packages[name]; ok {
			d.pkg = name
			return system.UpstreamVersion(v), v
		}
	}
	return "", ""
}

// tree returns pid and its descendants.
func tree(children map[int][]int, pid int) map[int]bool {
	out := map[int]bool{pid: true}
	queue := []int{pid}
	for len(queue) > 0 {
		for _, child := range children[queue[0]] {
			if !out[child] {
				out[child] = true
				queue = append(queue, child)
			}
		}
		queue = queue[1:]
	}
	return out
}

// listen returns the addresses listened on by pids, unix sockets prefixed
// with "unix:".
func listen(listeners []system.Listener, pids map[int]bool) []string {
	var out []string
	for _, l := range listeners {
		if l.PID == 0 || !pids[l.PID] {
			continue
		}
		addr := l.Address
		if l.Proto == system.ProtoUnix {
			addr = "unix:" + addr
		}
		if !slices.Contains(out, addr) {
			out = append(out, addr)
		}
	}
	slices.Sort(out)
	return out
}

// nameDuplicates gives an instance to services of a type running several
// times without one: their first address.
func nameDuplicates(services []snapshot.Service) {
	count := map[string]int{}
	for _, s := range services {
		if s.Instance == "" {
			count[s.Type]++
		}
	}
	for i := range services {
		if s := &services[i]; s.Instance == "" && count[s.Type] > 1 && len(s.Listen) > 0 {
			s.Instance = s.Listen[0]
		}
	}
}

// flagValue returns the value of --name=value or --name value in args.
func flagValue(args []string, name string) string {
	for i, arg := range args {
		if v, ok := strings.CutPrefix(arg, name+"="); ok {
			return v
		}
		if arg == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// describeMySQL tells MariaDB apart: before 10.5 its binary is mysqld.
func describeMySQL(d *detection, svc *snapshot.Service) error {
	if strings.HasPrefix(strings.ToLower(d.pkg), "mariadb") {
		svc.Type = TypeMariaDB
	}
	svc.ConfigPath = flagValue(d.process.Cmdline, "--defaults-file")
	return nil
}

func describeMongoDB(d *detection, svc *snapshot.Service) error {
	svc.ConfigPath = flagValue(d.process.Cmdline, "--config")
	if svc.ConfigPath == "" {
		svc.ConfigPath = flagValue(d.process.Cmdline, "-f")
	}
	return nil
}

// describePostgreSQL names the cluster: Debian runs one process per
// cluster, configured in /etc/postgresql/<version>/<cluster>/.
func describePostgreSQL(d *detection, svc *snapshot.Service) error {
	cmd := d.process.Cmdline
	if len(cmd) == 1 {
		cmd = strings.Fields(cmd[0]) // title rewritten by postgres
	}
	for i, arg := range cmd {
		if v, ok := strings.CutPrefix(arg, "config_file="); ok {
			svc.ConfigPath = v
		} else if arg == "-c" && i+1 < len(cmd) && strings.HasPrefix(cmd[i+1], "config_file=") {
			svc.ConfigPath = strings.TrimPrefix(cmd[i+1], "config_file=")
		}
	}
	if m := debianCluster.FindStringSubmatch(svc.ConfigPath); m != nil {
		svc.Cluster = m[2]
		svc.Instance = m[1] + "/" + m[2]
	} else if dataDir := flagValue(cmd, "-D"); dataDir != "" {
		svc.Instance = dataDir
	}
	return nil
}

// objectStore matches "<name> server …", the S3 server of MinIO or Silo,
// not its other commands.
func objectStore(name string) func(p system.Process) bool {
	return func(p system.Process) bool {
		return (base(p) == name || argv0(p) == name) && len(p.Cmdline) > 1 && p.Cmdline[1] == "server"
	}
}

// describeObjectStore gives the version of a package the form of the
// releases, as the binary and the images name them.
func describeObjectStore(_ *detection, svc *snapshot.Service) error {
	if m := packageRelease.FindStringSubmatch(svc.Version); m != nil {
		svc.Version = fmt.Sprintf("RELEASE.%s-%s-%sT%s-%s-%sZ", m[1], m[2], m[3], m[4], m[5], m[6])
	}
	return nil
}

// keycloakCoreJar is the jar of kc.home.dir/lib/lib/main named after the
// version of Keycloak.
var keycloakCoreJar = regexp.MustCompile(`^org\.keycloak\.keycloak-core-(.+)\.jar$`)

// describeKeycloak reads the version from the keycloak-core jar the process
// has open. An upgrade unpacked over an older one leaves both jars in
// kc.home.dir: when the open files cannot be read, the newest one wins.
func describeKeycloak(d *detection, svc *snapshot.Service) error {
	var home string
	for _, arg := range d.process.Cmdline {
		if v, ok := strings.CutPrefix(arg, "-Dkc.home.dir="); ok {
			home = strings.TrimSuffix(v, "/bin/..")
		}
	}
	if home == "" {
		return nil
	}
	svc.ConfigPath = filepath.Join(home, "conf", "keycloak.conf")
	if v := newestVersion(keycloakCoreJar, d.openFiles()); v != "" {
		svc.Version = v
		return nil
	}
	dir := filepath.Join(home, "lib", "lib", "main")
	matches, _ := afero.Glob(d.ctx.Fs, filepath.Join(dir, "org.keycloak.keycloak-core-*.jar"))
	if len(matches) == 0 {
		return errors.New("no keycloak-core jar in " + home)
	}
	svc.Version = newestVersion(keycloakCoreJar, matches)
	if len(matches) > 1 {
		d.ctx.Logger.Warn("several keycloak-core jars, the newest is reported: remove the older ones",
			"dir", dir, "version", svc.Version, "jars", len(matches))
	}
	return nil
}

// openFiles are the paths the process has open, read from /proc/<pid>/fd;
// none when they cannot be read.
func (d *detection) openFiles() []string {
	dir := fmt.Sprintf("%s/%d/fd", system.ProcDir, d.process.PID)
	fds, err := afero.ReadDir(d.ctx.Fs, dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, fd := range fds {
		if target, err := d.sys.Readlink(dir + "/" + fd.Name()); err == nil && strings.HasPrefix(target, "/") {
			out = append(out, target)
		}
	}
	return out
}

// newestVersion is the highest version that re captures in the base names
// of paths; empty when none matches.
func newestVersion(re *regexp.Regexp, paths []string) string {
	var newest string
	for _, path := range paths {
		if m := re.FindStringSubmatch(filepath.Base(path)); m != nil && (newest == "" || system.CompareVersions(m[1], newest) > 0) {
			newest = m[1]
		}
	}
	return newest
}

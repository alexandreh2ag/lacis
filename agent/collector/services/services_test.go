package services

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"testing"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/fakefs"
	mockExec "github.com/alexandreh2ag/lacis/agent/mocks/exec"
	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// host builds a fake /proc: processes, their listening sockets and the
// installed packages.
type host struct {
	t        *testing.T
	fs       *fakefs.FS
	tcp      []string
	unix     []string
	inode    int
	packages []string
	// versions answer "<binary> --version".
	versions map[string]string
}

func newHost(t *testing.T) *host {
	return &host{t: t, fs: fakefs.New(), inode: 1000, versions: map[string]string{}}
}

func (h *host) process(pid, ppid int, exe string, cmdline ...string) {
	dir := fmt.Sprintf("/proc/%d/", pid)
	h.fs.Write(h.t, dir+"status", fmt.Sprintf("Name:\t%s\nPPid:\t%d\nUid:\t0\t0\t0\t0\n", exe[strings.LastIndex(exe, "/")+1:], ppid))
	h.fs.Write(h.t, dir+"cmdline", strings.Join(cmdline, "\x00")+"\x00")
	h.fs.Link(dir+"exe", exe)
}

// listen adds a socket held by pid: "ip:port" or a unix path.
func (h *host) listen(pid int, addr string) {
	h.inode++
	fd := fmt.Sprintf("/proc/%d/fd/%d", pid, h.inode)
	h.fs.Write(h.t, fd, "")
	h.fs.Link(fd, fmt.Sprintf("socket:[%d]", h.inode))
	if strings.HasPrefix(addr, "/") {
		h.unix = append(h.unix, fmt.Sprintf("0: 00000002 00000000 00010000 0001 01 %d %s", h.inode, addr))
		return
	}
	ap := netip.MustParseAddrPort(addr)
	b := ap.Addr().As4()
	hexAddr := fmt.Sprintf("%08X", binary.LittleEndian.Uint32(b[:]))
	h.tcp = append(h.tcp, fmt.Sprintf("0: %s:%04X 00000000:0000 0A 0:0 0:0 0 0 0 %d", hexAddr, ap.Port(), h.inode))
}

func (h *host) pkg(name, version string) {
	h.packages = append(h.packages, "ii \t"+name+"\t"+version)
}

// context returns the agent context of the host, with dpkg answering.
func (h *host) context(dpkgErr error) *context.Context {
	h.fs.Write(h.t, system.TCPFile, "header\n"+strings.Join(h.tcp, "\n")+"\n")
	h.fs.Write(h.t, system.UnixFile, "header\n"+strings.Join(h.unix, "\n")+"\n")
	h.fs.Write(h.t, system.DpkgQueryCommand, "")
	h.fs.Write(h.t, "/etc/os-release", "ID=debian\n")
	ctx := context.TestContext(nil)
	ctx.Fs = h.fs
	e := mockExec.NewMockExecutor(gomock.NewController(h.t))
	e.EXPECT().Run(gomock.Any(), system.DpkgQueryCommand, gomock.Any()).Return([]byte(strings.Join(h.packages, "\n")+"\n"), dpkgErr).AnyTimes()
	e.EXPECT().Run(gomock.Any(), gomock.Any(), "--version").DoAndReturn(func(_ any, binary string, _ ...string) ([]byte, error) {
		v, ok := h.versions[binary]
		if !ok {
			return nil, errors.New("exit status 1")
		}
		return []byte(v), nil
	}).AnyTimes()
	ctx.Executor = e
	return ctx
}

func collect(t *testing.T, ctx *context.Context) (*snapshot.Snapshot, bool, error) {
	sys, err := system.Load(ctx)
	require.NoError(t, err)
	c := &Collector{}
	out := &snapshot.Snapshot{}
	if !c.Applicable(ctx, sys) {
		return out, false, nil
	}
	return out, true, c.Collect(t.Context(), ctx, sys, out)
}

const fpmConf = `[global]
pid = /run/php/php8.2-fpm.pid
include=/etc/php/8.2/fpm/pool.d/*.conf
`

func TestCollect(t *testing.T) {
	h := newHost(t)
	// MariaDB 10.3 still runs as mysqld.
	h.process(100, 1, "/usr/sbin/mysqld", "/usr/sbin/mysqld", "--defaults-file=/etc/mysql/my.cnf")
	h.listen(100, "127.0.0.1:3306")
	h.listen(100, "/run/mysqld/mysqld.sock")
	h.pkg("mariadb-server", "1:10.3.39-0+deb10u2")
	// Two PostgreSQL clusters, one with its workers.
	h.process(200, 1, "/usr/lib/postgresql/16/bin/postgres", "/usr/lib/postgresql/16/bin/postgres", "-D", "/var/lib/postgresql/16/main", "-c", "config_file=/etc/postgresql/16/main/postgresql.conf")
	h.process(201, 200, "/usr/lib/postgresql/16/bin/postgres", "postgres: 16/main: checkpointer ")
	h.listen(200, "127.0.0.1:5432")
	h.pkg("postgresql-16", "16.4-1.pgdg120+1")
	h.process(210, 1, "/usr/lib/postgresql/15/bin/postgres", "/usr/lib/postgresql/15/bin/postgres -D /srv/pg15")
	// Installed without package: its binary gives its version.
	h.versions["/usr/lib/postgresql/15/bin/postgres"] = "postgres (PostgreSQL) 15.8\n"
	h.listen(210, "127.0.0.1:5433")
	// Two redis without instance name.
	h.process(300, 1, "/usr/bin/redis-server", "/usr/bin/redis-server 127.0.0.1:6379")
	h.listen(300, "127.0.0.1:6379")
	h.process(310, 1, "/usr/bin/redis-server", "/usr/bin/redis-server 127.0.0.1:6380")
	h.listen(310, "127.0.0.1:6380")
	h.pkg("redis-server", "5:7.0.15-1~deb12u1")
	// PHP-FPM, its worker and its pools.
	h.process(400, 1, "/usr/sbin/php-fpm8.2", "php-fpm: master process (/etc/php/8.2/fpm/php-fpm.conf)")
	h.process(401, 400, "/usr/sbin/php-fpm8.2", "php-fpm: pool www")
	h.listen(400, "/run/php/php8.2-fpm.sock")
	h.listen(400, "127.0.0.1:9001")
	h.pkg("php8.2-fpm", "8.2.24-1+deb12u1")
	h.fs.Files(t, map[string]string{
		"/etc/php/8.2/fpm/php-fpm.conf":    fpmConf,
		"/etc/php/8.2/fpm/pool.d/www.conf": "; default pool\n[www]\nuser = www-data\nlisten = /run/php/php8.2-fpm.sock\n",
		"/etc/php/8.2/fpm/pool.d/api.conf": "[api]\nuser = deploy\nlisten = '127.0.0.1:9001'\n[admin]\nlisten = /run/php/$pool.sock\n",
	})
	// Elasticsearch, its HTTP port held by a child.
	h.process(500, 499, "/usr/share/elasticsearch/jdk/bin/java", "/usr/share/elasticsearch/jdk/bin/java", "-Xms1g", "org.elasticsearch.bootstrap.Elasticsearch")
	h.process(499, 1, "/usr/share/elasticsearch/jdk/bin/java", "/usr/share/elasticsearch/jdk/bin/java", "-Des.path.home=/usr/share/elasticsearch", "org.elasticsearch.launcher.CliToolLauncher")
	h.process(501, 500, "/usr/share/elasticsearch/modules/x-pack-ml/platform/linux-x86_64/bin/controller", "controller")
	h.listen(500, "0.0.0.0:9300")
	h.listen(501, "0.0.0.0:9200")
	h.pkg("elasticsearch", "8.15.0")
	// Keycloak, versioned by its jars.
	h.process(600, 1, "/usr/lib/jvm/java-21-openjdk-amd64/bin/java", "java", "-Dkc.home.dir=/opt/keycloak/bin/..", "io.quarkus.bootstrap.runner.QuarkusEntryPoint")
	h.fs.Write(t, "/opt/keycloak/lib/lib/main/org.keycloak.keycloak-core-25.0.6.jar", "")
	h.listen(600, "0.0.0.0:8080")
	// RabbitMQ and ClickHouse.
	h.process(700, 1, "/usr/lib/erlang/erts-14.2/bin/beam.smp", "/usr/lib/erlang/erts-14.2/bin/beam.smp", "--", "-s", "rabbit", "boot")
	h.pkg("rabbitmq-server", "3.12.1-1")
	// ClickHouse: a watchdog, then the server, both the clickhouse binary.
	h.process(2515, 1, "/usr/bin/clickhouse", "clickhouse-watchdog        --config=/etc/clickhouse-server/config.xml")
	h.process(2520, 2515, "/usr/bin/clickhouse", "/usr/bin/clickhouse-server", "--config=/etc/clickhouse-server/config.xml")
	h.listen(2520, "127.0.0.1:8123")
	h.pkg("clickhouse-server", "24.8.4.13")
	// MongoDB.
	h.process(97, 1, "/usr/bin/mongod", "/usr/bin/mongod", "--config", "/etc/mongod.conf")
	h.listen(97, "127.0.0.1:27017")
	h.pkg("mongodb-org-server", "7.0.14")
	// Unrelated processes.
	h.process(900, 1, "/usr/lib/jvm/java-21-openjdk-amd64/bin/java", "java", "-jar", "app.jar")
	h.process(901, 1, "/usr/bin/bash", "bash")
	// A redis in a Docker container, left to the docker collector.
	h.process(950, 940, "/usr/local/bin/redis-server", "redis-server *:6379")
	h.fs.Link("/proc/self/ns/pid", "pid:[1]")
	h.fs.Link("/proc/950/ns/pid", "pid:[2]")
	// MariaDB runs with PrivateTmp: another mount namespace, same PID one.
	h.fs.Link("/proc/100/ns/pid", "pid:[1]")
	h.fs.Link("/proc/100/ns/mnt", "mnt:[9]")

	out, applicable, err := collect(t, h.context(nil))
	require.NoError(t, err)
	assert.True(t, applicable)
	assert.Equal(t, []snapshot.Service{
		{Type: "clickhouse", Version: "24.8.4.13", Listen: []string{"127.0.0.1:8123"}},
		{Type: "elasticsearch", Version: "8.15.0", Listen: []string{"0.0.0.0:9200", "0.0.0.0:9300"}},
		{Type: "keycloak", Version: "25.0.6", Listen: []string{"0.0.0.0:8080"}, ConfigPath: "/opt/keycloak/conf/keycloak.conf"},
		{Type: "mariadb", Version: "10.3.39", Listen: []string{"127.0.0.1:3306", "unix:/run/mysqld/mysqld.sock"}, ConfigPath: "/etc/mysql/my.cnf"},
		{Type: "mongodb", Version: "7.0.14", Listen: []string{"127.0.0.1:27017"}, ConfigPath: "/etc/mongod.conf"},
		{
			Type: "php-fpm", Version: "8.2.24", Instance: "8.2",
			Listen:     []string{"127.0.0.1:9001", "unix:/run/php/php8.2-fpm.sock"},
			ConfigPath: "/etc/php/8.2/fpm/php-fpm.conf",
			Pools: []snapshot.FPMPool{
				{Name: "admin", Listen: "unix:/run/php/admin.sock"},
				{Name: "api", Listen: "127.0.0.1:9001", User: "deploy"},
				{Name: "www", Listen: "unix:/run/php/php8.2-fpm.sock", User: "www-data"},
			},
		},
		{Type: "postgresql", Version: "15.8", Instance: "/srv/pg15", Listen: []string{"127.0.0.1:5433"}},
		{Type: "postgresql", Version: "16.4", Instance: "16/main", Cluster: "main", Listen: []string{"127.0.0.1:5432"}, ConfigPath: "/etc/postgresql/16/main/postgresql.conf"},
		{Type: "rabbitmq", Version: "3.12.1"},
		{Type: "redis", Version: "7.0.15", Instance: "127.0.0.1:6379", Listen: []string{"127.0.0.1:6379"}},
		{Type: "redis", Version: "7.0.15", Instance: "127.0.0.1:6380", Listen: []string{"127.0.0.1:6380"}},
	}, out.Services)
}

func TestCollect_NoService(t *testing.T) {
	h := newHost(t)
	h.process(1, 0, "/usr/lib/systemd/systemd", "/sbin/init")
	_, applicable, _ := collect(t, h.context(nil))
	assert.False(t, applicable)
}

func TestCollect_Errors(t *testing.T) {
	h := newHost(t)
	h.process(400, 1, "/opt/remi/php82/root/usr/sbin/php-fpm", "php-fpm: master process (/etc/opt/remi/php82/php-fpm.conf)")
	h.process(600, 1, "/usr/bin/java", "java", "-Dkc.home.dir=/opt/kc")
	out, applicable, err := collect(t, h.context(errors.New("dpkg locked")))
	assert.True(t, applicable)
	assert.ErrorContains(t, err, "list packages: dpkg locked")
	assert.ErrorContains(t, err, "php-fpm: read /etc/opt/remi/php82/php-fpm.conf")
	assert.ErrorContains(t, err, "keycloak: no keycloak-core jar in /opt/kc")
	require.Len(t, out.Services, 2, "still reported, without version")
	assert.Equal(t, "82", out.Services[1].Instance, "version of the Remi path")
}

func TestApplicable_ProcessesUnreadable(t *testing.T) {
	ctx := context.TestContext(nil)
	ctx.Fs = fakefs.New()
	_, applicable, err := collect(t, ctx)
	assert.True(t, applicable)
	assert.ErrorContains(t, err, "list processes")
}

func TestPHPFPMInstanceFromVersion(t *testing.T) {
	d := &detection{process: system.Process{}, ctx: context.TestContext(nil)}
	svc := &snapshot.Service{Version: "8.3.1"}
	require.NoError(t, describePHPFPM(d, svc))
	assert.Equal(t, "8.3", svc.Instance)
}

func TestReadFPMConfig_IncludeLoop(t *testing.T) {
	f := fakefs.New()
	f.Write(t, "/etc/fpm.conf", "include=fpm.conf\n[www]\nlisten=9000\n")
	pools, err := readFPMConfig(f, "/etc/fpm.conf", 0)
	require.NoError(t, err)
	assert.Len(t, pools, maxIncludeDepth+1, "bounded")
}

func TestName(t *testing.T) {
	assert.Equal(t, Name, (&Collector{}).Name())
}

func TestDescribeMongoDB_ShortFlag(t *testing.T) {
	svc := &snapshot.Service{}
	require.NoError(t, describeMongoDB(&detection{process: system.Process{Cmdline: []string{"mongod", "-f", "/etc/mongodb.conf"}}}, svc))
	assert.Equal(t, "/etc/mongodb.conf", svc.ConfigPath)
}

func TestArgv0(t *testing.T) {
	assert.Empty(t, argv0(system.Process{}))
	assert.Equal(t, "clickhouse-watchdog", argv0(system.Process{Cmdline: []string{"clickhouse-watchdog  --config=x"}}))
}

func TestBinaryVersion(t *testing.T) {
	for out, want := range map[string]string{
		"Redis server v=7.2.4 sha=00000000:0 malloc=jemalloc-5.3.0 bits=64 build=1": "7.2.4",
		"mariadbd  Ver 10.11.6-MariaDB-0+deb12u1 for debian-linux-gnu on x86_64":    "10.11.6",
		"/usr/sbin/mysqld  Ver 8.0.39 for Linux on x86_64 (MySQL Community Server)": "8.0.39",
		"unexpected": "",
	} {
		h := newHost(t)
		exe := "/usr/bin/redis-server"
		switch {
		case strings.Contains(out, "mariadbd"):
			exe = "/usr/sbin/mariadbd"
		case strings.Contains(out, "mysqld"):
			exe = "/usr/sbin/mysqld"
		}
		h.process(10, 1, exe, exe)
		h.versions[exe] = out
		got, _, err := collect(t, h.context(nil))
		require.NoError(t, err)
		require.Len(t, got.Services, 1)
		assert.Equal(t, want, got.Services[0].Version, out)
	}
}

package system

import (
	"testing"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const passwd = `root:x:0:0:root:/root:/bin/bash
www-data:x:33:33:www-data:/var/www:/usr/sbin/nologin
deploy:x:1000:1000::/home/deploy:/bin/bash
alias:x:1000:1000::/home/alias:/bin/bash
broken
`

func TestProcesses(t *testing.T) {
	sys, f, _ := newSystem(t, FamilyDebian, map[string]string{
		PasswdFile: passwd,
		// nginx master, as root
		"/proc/812/status":  "Name:\tnginx\nState:\tS (sleeping)\nPPid:\t1\nUid:\t0\t0\t0\t0\n",
		"/proc/812/cmdline": "nginx: master process /usr/sbin/nginx -g daemon on;\x00",
		// node under nvm, binary replaced by an upgrade, unknown user
		"/proc/1234/status":  "Name:\tnode\nPPid:\t812\nUid:\t1000\t1000\t1000\t1000\n",
		"/proc/1234/cmdline": "/home/deploy/.nvm/versions/node/v20.11.1/bin/node\x00server.js\x00--port\x003000\x00",
		"/proc/77/status":    "Name:\tphp-fpm8.2\nPPid:\t1\nUid:\t4242\t4242\t4242\t4242\n",
		"/proc/77/cmdline":   "php-fpm: pool www\x00",
		// kernel thread: no cmdline
		"/proc/2/status":  "Name:\tkthreadd\nPPid:\t0\nUid:\t0\t0\t0\t0\n",
		"/proc/2/cmdline": "",
		// vanished between the listing and the read
		"/proc/999/cmdline": "gone\x00",
		"/proc/self/status": "Name:\tlacis-agent\n",
		"/proc/meminfo":     "",
	})
	f.Link("/proc/812/exe", "/usr/sbin/nginx")
	f.Link("/proc/812/cwd", "/")
	f.Link("/proc/1234/exe", "/home/deploy/.nvm/versions/node/v20.11.1/bin/node (deleted)")
	f.Link("/proc/1234/cwd", "/var/www/front/current")
	f.Link("/proc/self/ns/pid", "pid:[4026531841]")
	f.Link("/proc/812/ns/pid", "pid:[4026531841]")
	f.Link("/proc/77/ns/pid", "pid:[4026532999]") // in a container

	got, err := sys.Processes()
	require.NoError(t, err)
	assert.Equal(t, []Process{
		{PID: 77, PPID: 1, Name: "php-fpm8.2", Cmdline: []string{"php-fpm: pool www"}, UID: 4242, InContainer: true},
		{PID: 812, PPID: 1, Name: "nginx", Exe: "/usr/sbin/nginx", Cwd: "/", Cmdline: []string{"nginx: master process /usr/sbin/nginx -g daemon on;"}, UID: 0, User: "root"},
		{
			PID: 1234, PPID: 812, Name: "node", UID: 1000, User: "deploy",
			Exe:     "/home/deploy/.nvm/versions/node/v20.11.1/bin/node",
			Cwd:     "/var/www/front/current",
			Cmdline: []string{"/home/deploy/.nvm/versions/node/v20.11.1/bin/node", "server.js", "--port", "3000"},
		},
	}, got)
}

func TestProcesses_NoProc(t *testing.T) {
	sys, _, _ := newSystem(t, FamilyDebian, nil)
	_, err := sys.Processes()
	assert.ErrorContains(t, err, "list processes")
}

func TestReadlink_Unsupported(t *testing.T) {
	ctx := context.TestContext(nil)
	ctx.Fs = afero.NewMemMapFs()
	_, err := (&System{ctx: ctx}).Readlink("/proc/1/exe")
	assert.ErrorIs(t, err, ErrNoLinks)
}

func TestResolve(t *testing.T) {
	sys, f, _ := newSystem(t, FamilyDebian, nil)
	f.Link("/usr/bin/php", "/etc/alternatives/php")
	f.Link("/etc/alternatives/php", "../../usr/bin/php8.2")
	f.Link("/usr/lib/jvm/default-java", "java-21-openjdk-amd64")
	f.Link("/loop/a", "b")
	f.Link("/loop/b", "a")

	got, err := sys.Resolve("/usr/bin/php")
	require.NoError(t, err)
	assert.Equal(t, "/usr/bin/php8.2", got)

	got, err = sys.Resolve("/usr/bin/php8.2")
	require.NoError(t, err)
	assert.Equal(t, "/usr/bin/php8.2", got, "not a link")

	got, err = sys.Resolve("/usr/lib/jvm/default-java/bin/../bin/java")
	require.NoError(t, err)
	assert.Equal(t, "/usr/lib/jvm/java-21-openjdk-amd64/bin/java", got, "links of directories too")

	_, err = sys.Resolve("/loop/a")
	assert.ErrorContains(t, err, "too many links")

	ctx := context.TestContext(nil)
	ctx.Fs = afero.NewMemMapFs()
	_, err = (&System{ctx: ctx}).Resolve("/usr/bin/php")
	assert.ErrorIs(t, err, ErrNoLinks)
}

package runtimes

import (
	"errors"
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

func newContext(t *testing.T, files map[string]string) (*context.Context, *fakefs.FS, *mockExec.MockExecutor) {
	ctx := context.TestContext(nil)
	f := fakefs.New()
	f.Files(t, files)
	ctx.Fs = f
	e := mockExec.NewMockExecutor(gomock.NewController(t))
	ctx.Executor = e
	return ctx, f, e
}

// collect runs the collector as collector.Run does.
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

func TestCollect(t *testing.T) {
	ctx, f, e := newContext(t, map[string]string{
		"/usr/bin/php":          "",
		"/usr/bin/php8.2":       "",
		"/usr/bin/php7.4":       "",
		"/usr/bin/phpize":       "",
		"/usr/bin/php-cgi":      "",
		"/etc/alternatives/php": "",
		"/usr/bin/node":         "",
		"/home/deploy/.nvm/versions/node/v20.11.1/bin/node": "",
		"/home/deploy/.nvm/versions/node/v18.20.4/bin/node": "",
		"/root/.volta/tools/image/node/22.9.0/bin/node":     "",
		"/usr/local/n/versions/node/16.20.2/bin/node":       "",
		"/var/lib/nvm/versions/node/v22.14.0/bin/node":      "",
		"/home/deploy/.nvm/versions/node/v20.9.0/bin/node":  "",
		"/usr/lib/jvm/java-17-openjdk-amd64/bin/java":       "",
		"/usr/lib/jvm/java-17-openjdk-amd64/release":        "IMPLEMENTOR=\"Debian\"\nJAVA_VERSION=\"17.0.12\"\n",
		"/usr/lib/jvm/default-java/bin/java":                "",
		// a process running a node outside the known paths
		"/proc/1234/status":  "Name:\tnode\nPPid:\t1\nUid:\t1000\t1000\t1000\t1000\n",
		"/proc/1234/cmdline": "node\x00server.js\x00",
		"/proc/77/status":    "Name:\tbash\nPPid:\t1\nUid:\t0\t0\t0\t0\n",
		"/proc/77/cmdline":   "bash\x00",
		"/proc/88/status":    "Name:\tnode\nPPid:\t1\n",
		"/proc/88/cmdline":   "node\x00",
	})
	f.Link("/usr/bin/php", "/etc/alternatives/php")
	f.Link("/etc/alternatives/php", "/usr/bin/php8.2")
	f.Link("/usr/lib/jvm/default-java", "java-17-openjdk-amd64")
	f.Link("/proc/1234/exe", "/opt/front/node/bin/node")
	f.Link("/proc/77/exe", "/usr/bin/bash")
	f.Link("/proc/88/exe", "/usr/local/bin/node") // in a container
	f.Link("/proc/88/ns/pid", "pid:[2]")
	f.Link("/proc/self/ns/pid", "pid:[1]")
	e.EXPECT().Run(gomock.Any(), "/usr/bin/php8.2", "-n", "-v").Return([]byte("PHP 8.2.24 (cli) (built: Sep 27 2024)\nCopyright (c) The PHP Group\n"), nil)
	e.EXPECT().Run(gomock.Any(), "/usr/bin/php7.4", "-n", "-v").Return([]byte("PHP 7.4.33 (cli)\n"), nil)
	e.EXPECT().Run(gomock.Any(), "/usr/bin/node", "--version").Return([]byte("v18.19.1\n"), nil)
	e.EXPECT().Run(gomock.Any(), "/opt/front/node/bin/node", "--version").Return([]byte("v20.18.0\n"), nil)

	out, applicable, err := collect(t, ctx)
	require.NoError(t, err)
	assert.True(t, applicable)
	assert.Equal(t, []snapshot.Runtime{
		{Type: "java", Version: "17.0.12", Path: "/usr/lib/jvm/java-17-openjdk-amd64/bin/java"},
		{Type: "node", Version: "16.20.2", Path: "/usr/local/n/versions/node/16.20.2/bin/node", Manager: "n"},
		{Type: "node", Version: "18.19.1", Path: "/usr/bin/node"},
		{Type: "node", Version: "18.20.4", Path: "/home/deploy/.nvm/versions/node/v18.20.4/bin/node", Manager: "nvm"},
		{Type: "node", Version: "20.9.0", Path: "/home/deploy/.nvm/versions/node/v20.9.0/bin/node", Manager: "nvm"},
		{Type: "node", Version: "20.11.1", Path: "/home/deploy/.nvm/versions/node/v20.11.1/bin/node", Manager: "nvm"},
		{Type: "node", Version: "20.18.0", Path: "/opt/front/node/bin/node"},
		{Type: "node", Version: "22.9.0", Path: "/root/.volta/tools/image/node/22.9.0/bin/node", Manager: "volta"},
		{Type: "node", Version: "22.14.0", Path: "/var/lib/nvm/versions/node/v22.14.0/bin/node", Manager: "nvm"},
		{Type: "php", Version: "7.4.33", Path: "/usr/bin/php7.4"},
		{Type: "php", Version: "8.2.24", Path: "/usr/bin/php8.2"},
	}, out.Runtimes)
}

func TestCollect_NothingFound(t *testing.T) {
	ctx, _, _ := newContext(t, map[string]string{"/proc/1/status": "Name:\tinit\n", "/proc/1/cmdline": "init\x00"})
	_, applicable, _ := collect(t, ctx)
	assert.False(t, applicable)
}

func TestCollect_Errors(t *testing.T) {
	ctx, f, e := newContext(t, map[string]string{
		"/usr/bin/php8.2":                   "",
		"/usr/bin/php8.3":                   "",
		"/usr/bin/node":                     "",
		"/usr/lib/jvm/jre-minimal/bin/java": "",
		"/usr/lib/jvm/broken/bin/java":      "",
		"/usr/lib/jvm/broken/release":       "IMPLEMENTOR=x\n",
		"/usr/lib/jvm/raw/bin/java":         "",
		"/usr/lib/jvm/raw/release":          "JAVA_VERSION=11.0.2\n",
		"/usr/local/bin/php":                "",
		"/proc/1/status":                    "Name:\tinit\n",
		"/proc/1/cmdline":                   "init\x00",
	})
	f.Link("/usr/local/bin/php", "php")
	e.EXPECT().Run(gomock.Any(), "/usr/bin/php8.2", "-n", "-v").Return(nil, errors.New("exit status 255"))
	e.EXPECT().Run(gomock.Any(), "/usr/bin/php8.3", "-n", "-v").Return([]byte("Segmentation fault\n"), nil)
	e.EXPECT().Run(gomock.Any(), "/usr/bin/node", "--version").Return([]byte(""), nil)

	out, applicable, err := collect(t, ctx)
	assert.True(t, applicable)
	assert.ErrorContains(t, err, "/usr/bin/php8.2: exit status 255")
	assert.ErrorContains(t, err, `/usr/bin/php8.3: unexpected version "Segmentation fault"`)
	assert.ErrorContains(t, err, "/usr/bin/node: no version printed")
	assert.ErrorContains(t, err, "/usr/lib/jvm/jre-minimal/bin/java: open /usr/lib/jvm/jre-minimal/release")
	assert.ErrorContains(t, err, "/usr/lib/jvm/broken/bin/java: no JAVA_VERSION in release")
	assert.ErrorContains(t, err, "/usr/local/bin/php: too many links")
	assert.Equal(t, []snapshot.Runtime{{Type: "java", Version: "11.0.2", Path: "/usr/lib/jvm/raw/bin/java"}}, out.Runtimes,
		"the readable runtimes are kept")
}

func TestApplicable_ProcessesUnreadable(t *testing.T) {
	ctx, _, _ := newContext(t, nil) // no /proc
	_, applicable, err := collect(t, ctx)
	assert.True(t, applicable, "reported as an error rather than skipped")
	assert.ErrorContains(t, err, "list processes")
}

func TestName(t *testing.T) {
	assert.Equal(t, Name, (&Collector{}).Name())
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"20.9.0", "20.19.3", -1},
		{"20.19.3", "20.9.0", 1},
		{"8.2", "8.2.24", -1},
		{"17.0.12", "17.0.12", 0},
		{"21.0.5-ea", "21.0.5-ga", -1},
	}
	for _, tt := range tests {
		got := compareVersions(tt.a, tt.b)
		assert.Equal(t, tt.want, max(-1, min(1, got)), "%s vs %s", tt.a, tt.b)
	}
}

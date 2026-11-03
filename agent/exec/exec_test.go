package exec

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/agent/fsutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testExecutor allows the shell tools used by the tests.
var testExecutor = OsExecutor{allowed: []*regexp.Regexp{regexp.MustCompile(`^(echo|sh|false|sleep)$`)}}

func TestOsExecutor_Run(t *testing.T) {
	out, err := testExecutor.Run(context.Background(), "/bin/echo", "hello")
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(out))
}

func TestOsExecutor_RunRelativePath(t *testing.T) {
	_, err := testExecutor.Run(context.Background(), "echo", "hello")
	assert.ErrorIs(t, err, ErrRelativePath)
}

func TestOsExecutor_RunFailureIncludesStderr(t *testing.T) {
	_, err := testExecutor.Run(context.Background(), "/bin/sh", "-c", "echo boom >&2; exit 3")
	assert.ErrorContains(t, err, "exit status 3")
	assert.ErrorContains(t, err, "boom")
}

func TestOsExecutor_RunFailureWithoutStderr(t *testing.T) {
	_, err := testExecutor.Run(context.Background(), "/bin/false")
	assert.EqualError(t, err, "/bin/false: exit status 1")
}

func TestOsExecutor_RunTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := testExecutor.Run(ctx, "/bin/sleep", "5")
	assert.Error(t, err)
}

func TestOsExecutor_RunNotAllowed(t *testing.T) {
	_, err := NewOsExecutor().Run(context.Background(), "/bin/sh", "-c", "id")
	assert.ErrorIs(t, err, ErrNotAllowed)
}

func TestAllowed(t *testing.T) {
	match := func(name string) bool {
		return slices.ContainsFunc(Allowed, func(re *regexp.Regexp) bool { return re.MatchString(name) })
	}
	for _, name := range []string{"systemd-detect-virt", "dpkg-query", "rpm", "php", "php8.2", "php82", "node", "redis-server"} {
		assert.True(t, match(name), name)
	}
	for _, name := range []string{"sh", "bash", "php-fpm8.2", "phpize", "node-gyp", "rm", "nginx"} {
		assert.False(t, match(name), name)
	}
}

func TestOsExecutor_RunAs(t *testing.T) {
	cred := Credential{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}
	if os.Geteuid() == 0 {
		cred = Nobody
	}
	out, err := testExecutor.RunAs(context.Background(), cred, "/bin/sh", "-c", "id -u")
	require.NoError(t, err)
	assert.Equal(t, cred.UID, uint32(mustAtoi(t, strings.TrimSpace(string(out)))))

	_, err = testExecutor.RunAs(context.Background(), cred, "/bin/sh", "-c", "id")
	require.NoError(t, err)
	_, err = testExecutor.RunAs(context.Background(), cred, "/usr/bin/id")
	assert.ErrorIs(t, err, ErrNotAllowed)
}

func TestOsExecutor_RunAsStderrFallback(t *testing.T) {
	cred := Credential{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}
	out, err := testExecutor.RunAs(context.Background(), cred, "/bin/sh", "-c", "echo 'varnishd (varnish-7.5.0)' >&2")
	require.NoError(t, err)
	assert.Equal(t, "varnishd (varnish-7.5.0)\n", string(out))
	out, err = testExecutor.RunAs(context.Background(), cred, "/bin/sh", "-c", "echo out; echo err >&2")
	require.NoError(t, err)
	assert.Equal(t, "out\n", string(out))
}

func TestOsExecutor_RunOutputCapped(t *testing.T) {
	_, err := testExecutor.RunAs(context.Background(), Nobody, "/bin/sh", "-c", "head -c 100000 /dev/zero")
	assert.ErrorIs(t, err, ErrOutputTooLarge)
}

func TestOsExecutor_RunKillsProcessGroup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	// The child keeps stdout open: without the group kill and WaitDelay,
	// Run would wait for it.
	_, err := testExecutor.Run(ctx, "/bin/sh", "-c", "sleep 30 & sleep 30")
	assert.Error(t, err)
	assert.Less(t, time.Since(start), 10*time.Second)
}

func TestOsExecutor_ReadFileAs(t *testing.T) {
	dir := t.TempDir()
	self := filepath.Join(dir, "lacis-agent")
	// Stands for "lacis-agent internal read --max N -- path".
	script := `#!/bin/sh
[ "$1 $2 $3 $5" = "internal read --max --" ] || exit 1
case "$6" in
*/missing) echo "no such file" >&2; exit 2 ;;
*/big) head -c 100 /dev/zero; exit 0 ;;
esac
cat "$6"
`
	require.NoError(t, os.WriteFile(self, []byte(script), 0o755))
	file := filepath.Join(dir, "conf")
	require.NoError(t, os.WriteFile(file, []byte("[www]\n"), 0o644))
	e := OsExecutor{self: self}
	cred := Credential{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}

	b, err := e.ReadFileAs(context.Background(), cred, file, 10)
	require.NoError(t, err)
	assert.Equal(t, "[www]\n", string(b))

	_, err = e.ReadFileAs(context.Background(), cred, filepath.Join(dir, "missing"), 10)
	assert.ErrorIs(t, err, fs.ErrNotExist)
	assert.ErrorContains(t, err, "no such file")

	_, err = e.ReadFileAs(context.Background(), cred, filepath.Join(dir, "big"), 10)
	assert.ErrorIs(t, err, fsutil.ErrTooLarge)

	_, err = OsExecutor{}.ReadFileAs(context.Background(), cred, file, 10)
	assert.Error(t, err)
}

func TestNewOsExecutor(t *testing.T) {
	e := NewOsExecutor().(OsExecutor)
	assert.True(t, filepath.IsAbs(e.self))
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, c := range s {
		require.True(t, c >= '0' && c <= '9', s)
		n = n*10 + int(c-'0')
	}
	return n
}

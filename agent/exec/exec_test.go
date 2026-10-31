package exec

import (
	"context"
	"regexp"
	"slices"
	"testing"
	"time"

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
	for _, name := range []string{"systemd-detect-virt", "dpkg-query", "rpm", "php", "php8.2", "php82", "node"} {
		assert.True(t, match(name), name)
	}
	for _, name := range []string{"sh", "bash", "php-fpm8.2", "phpize", "node-gyp", "rm"} {
		assert.False(t, match(name), name)
	}
}

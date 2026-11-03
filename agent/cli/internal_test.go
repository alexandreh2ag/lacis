package cli

import (
	"errors"
	"testing"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInternalReadCmd(t *testing.T) {
	ctx := context.TestContext(nil)
	// No configuration file: internal read never loads it.
	require.NoError(t, afero.WriteFile(ctx.Fs, "/etc/php/8.2/fpm/php-fpm.conf", []byte("[global]\n"), 0o644))
	require.NoError(t, afero.WriteFile(ctx.Fs, "/etc/big", []byte("0123456789"), 0o644))

	out, err := run(ctx, "internal", "read", "--", "/etc/php/8.2/fpm/php-fpm.conf")
	require.NoError(t, err)
	assert.Equal(t, "[global]\n", out)

	_, err = run(ctx, "internal", "read", "--max", "5", "/etc/big")
	assert.Equal(t, fsutil.ExitTooLarge, ExitCode(err))
	_, err = run(ctx, "internal", "read", "/etc/none")
	assert.Equal(t, fsutil.ExitNotExist, ExitCode(err))
}

func TestExitCode(t *testing.T) {
	assert.Equal(t, 0, ExitCode(nil))
	assert.Equal(t, 1, ExitCode(errors.New("x")))
	assert.Equal(t, 3, ExitCode(&ExitCodeError{Code: 3, Err: errors.New("x")}))
}

//go:build integration

package cli

import (
	"io"
	"log/slog"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/database/dbtest"
	"github.com/alexandreh2ag/lacis/queue"
	"github.com/riverqueue/river/rivertype"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWorkerCmd runs worker on a real database: it works a task asked by
// hand, then stops on SIGTERM.
func TestWorkerCmd(t *testing.T) {
	db := dbtest.Open(t, "cli")
	require.NoError(t, db.Exec("delete from river_job").Error)
	ctx := context.TestContext(nil)
	yaml := strings.Replace(validYAML, "postgres://lacis@db/lacis", dbtest.DSN(t, "cli"), 1)
	require.NoError(t, afero.WriteFile(ctx.Fs, "/etc/lacis/server.yaml", []byte(yaml), 0o600))

	errs := make(chan error, 1)
	go func() {
		_, err := run(ctx, "worker")
		errs <- err
	}()

	sqlDB, err := db.DB()
	require.NoError(t, err)
	// Its own configuration: the command loads ctx.Config meanwhile.
	inserter, err := queue.New(sqlDB, config.DefaultConfig().Workers, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	require.NoError(t, err)
	_, err = inserter.RunTask(t.Context(), "purge_expired_tokens")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		rows, err := inserter.Runs(t.Context(), []string{"purge_expired_tokens"}, []rivertype.JobState{rivertype.JobStateCompleted}, 1)
		require.NoError(t, err)
		return len(rows) == 1
	}, 15*time.Second, 100*time.Millisecond, "the worker runs the task")

	ctx.Signal() <- syscall.SIGTERM
	select {
	case err := <-errs:
		assert.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not stop")
	}
}

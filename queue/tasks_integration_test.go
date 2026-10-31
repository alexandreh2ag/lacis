//go:build integration

package queue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/database/dbtest"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// maintainer counts the purges of snapshots, and fails the detection of
// conflicts until fixed.
type maintainer struct {
	purged atomic.Int64
	fixed  atomic.Bool
}

func (m *maintainer) PurgeSnapshots(context.Context) (int64, error) { return m.purged.Add(1) * 10, nil }
func (m *maintainer) PurgeRemovedEntities(context.Context) (int64, error) {
	return 0, nil
}
func (m *maintainer) PurgeExpiredTokens(context.Context) (int64, int64, error) { return 1, 2, nil }
func (m *maintainer) DetectTokenConflicts(context.Context) (int, error) {
	if !m.fixed.Load() {
		return 0, errors.New("db down")
	}
	return 3, nil
}
func (m *maintainer) PurgeInactiveHosts(context.Context) (int, error) { return 0, nil }

func TestTasks(t *testing.T) {
	db := dbtest.Open(t, "queue")
	require.NoError(t, db.Exec("delete from river_job").Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	m := &maintainer{}
	cfg := config.DefaultConfig().Workers
	cfg.Schedules = map[string]string{"purge_snapshots": "@yearly"}
	c, err := New(sqlDB, cfg, discard, &Workers{Maintainer: m})
	require.NoError(t, err)
	require.NoError(t, c.Start(t.Context()))
	t.Cleanup(func() { assert.NoError(t, c.Stop(context.Background())) })

	run, err := c.RunTask(t.Context(), "purge_expired_tokens")
	require.NoError(t, err)
	assert.Equal(t, TaskMaxAttempts, run.MaxAttempts)
	var done *rivertype.JobRow
	require.Eventually(t, func() bool {
		rows, err := c.Runs(t.Context(), []string{"purge_expired_tokens"}, nil, 5)
		require.NoError(t, err)
		if len(rows) == 1 && rows[0].State == rivertype.JobStateCompleted {
			done = rows[0]
			return true
		}
		return false
	}, 10*time.Second, 100*time.Millisecond)
	assert.Equal(t, run.ID, done.ID)
	assert.JSONEq(t, `{"tokens":1,"sessions":2}`, string(done.Output()), "the run records what it changed")

	// A failed run is retried by River, and by hand.
	failed, err := c.RunTask(t.Context(), "detect_token_conflicts")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		rows, err := c.Runs(t.Context(), nil, []rivertype.JobState{rivertype.JobStateRetryable}, 5)
		require.NoError(t, err)
		return len(rows) == 1 && rows[0].ID == failed.ID && rows[0].Errors[0].Error == "db down"
	}, 10*time.Second, 100*time.Millisecond)
	again, err := c.RunTask(t.Context(), "detect_token_conflicts")
	require.NoError(t, err)
	assert.Equal(t, failed.ID, again.ID, "a run waiting already is the one asked")
	m.fixed.Store(true)
	_, err = c.Retry(t.Context(), failed.ID)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		rows, err := c.Runs(t.Context(), []string{"detect_token_conflicts"}, []rivertype.JobState{rivertype.JobStateCompleted}, 5)
		require.NoError(t, err)
		return len(rows) == 1
	}, 10*time.Second, 100*time.Millisecond)

	_, err = c.RunTask(t.Context(), "nope")
	assert.ErrorIs(t, err, ErrUnknownTask)
	_, err = c.Retry(t.Context(), -1)
	assert.ErrorIs(t, err, rivertype.ErrNotFound)
}

func TestTasks_WithoutMaintainer(t *testing.T) {
	db := dbtest.Open(t, "queue")
	require.NoError(t, db.Exec("delete from river_job").Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	c, err := New(sqlDB, config.DefaultConfig().Workers, discard, &Workers{})
	require.NoError(t, err)
	require.NoError(t, c.Start(t.Context()))
	t.Cleanup(func() { assert.NoError(t, c.Stop(context.Background())) })
	_, err = c.RunTask(t.Context(), "purge_snapshots")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		rows, err := c.Runs(t.Context(), []string{"purge_snapshots"}, []rivertype.JobState{rivertype.JobStateRetryable}, 1)
		require.NoError(t, err)
		return len(rows) == 1 && rows[0].Errors[0].Error == errNoMaintainer.Error()
	}, 10*time.Second, 100*time.Millisecond)
}

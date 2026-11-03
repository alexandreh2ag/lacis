package service

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	mockQueue "github.com/alexandreh2ag/lacis/mocks/queue"
	"github.com/alexandreh2ag/lacis/queue"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestTaskService_List(t *testing.T) {
	jobs := mockQueue.NewMockAdmin(gomock.NewController(t))
	appCtx := testContext(t)
	appCtx.Config.Workers.Schedules = map[string]string{"purge_snapshots": "15 3 * * *", "purge_inactive_hosts": ""}
	finished := now.Add(-time.Hour)
	jobs.EXPECT().Runs(ctx, []string{"purge_snapshots"}, nil, TaskRunsShown).Return([]*rivertype.JobRow{
		{ID: 7, Kind: "purge_snapshots", State: rivertype.JobStateCompleted, Attempt: 1, MaxAttempts: 5, CreatedAt: finished, FinalizedAt: &finished,
			Metadata: []byte(`{"output":{"snapshots":12}}`)},
		{ID: 6, Kind: "purge_snapshots", State: rivertype.JobStateDiscarded, Attempt: 5, MaxAttempts: 5,
			Errors: []rivertype.AttemptError{{Error: "db down"}, {Error: "timeout"}}},
	}, nil)
	jobs.EXPECT().Runs(ctx, gomock.Any(), nil, TaskRunsShown).Return(nil, nil).Times(len(queue.Tasks) - 1)

	got, err := NewTaskService(appCtx, jobs).List(ctx)
	require.NoError(t, err)
	require.Len(t, got, len(queue.Tasks))
	snapshots := got[0]
	assert.Equal(t, "purge_snapshots", snapshots.Kind)
	assert.Equal(t, "15 3 * * *", *snapshots.Schedule)
	assert.Equal(t, time.Date(2026, 10, 28, 3, 15, 0, 0, time.UTC), snapshots.NextRunAt.UTC(), "the next 03:15 after the clock")
	require.Len(t, snapshots.Runs, 2)
	assert.JSONEq(t, `{"snapshots":12}`, string(snapshots.Runs[0].Output))
	assert.Equal(t, []string{}, snapshots.Runs[0].Errors)
	assert.Equal(t, []string{"db down", "timeout"}, snapshots.Runs[1].Errors)
	assert.Nil(t, snapshots.Runs[1].Output)
	inactive := got[len(got)-1]
	assert.Equal(t, "purge_inactive_hosts", inactive.Kind)
	assert.Nil(t, inactive.Schedule)
	assert.Nil(t, inactive.NextRunAt, "only run by hand")
	assert.Equal(t, []TaskRun{}, inactive.Runs)

	errDB := errors.New("db down")
	jobs.EXPECT().Runs(ctx, gomock.Any(), nil, TaskRunsShown).Return(nil, errDB)
	_, err = NewTaskService(appCtx, jobs).List(ctx)
	assert.ErrorIs(t, err, errDB)
}

func TestTaskService_RunRetryFailures(t *testing.T) {
	jobs := mockQueue.NewMockAdmin(gomock.NewController(t))
	svc := NewTaskService(testContext(t), jobs)

	jobs.EXPECT().RunTask(ctx, "purge_snapshots").Return(&rivertype.JobRow{ID: 8, Kind: "purge_snapshots", State: rivertype.JobStateAvailable}, nil)
	run, err := svc.Run(ctx, "purge_snapshots")
	require.NoError(t, err)
	assert.Equal(t, TaskRun{ID: 8, Kind: "purge_snapshots", State: "available", Errors: []string{}}, *run)
	jobs.EXPECT().RunTask(ctx, "nope").Return(nil, queue.ErrUnknownTask)
	_, err = svc.Run(ctx, "nope")
	assert.ErrorIs(t, err, ErrNotFound)

	jobs.EXPECT().Retry(ctx, int64(6)).Return(&rivertype.JobRow{ID: 6, State: rivertype.JobStateAvailable}, nil)
	run, err = svc.Retry(ctx, 6)
	require.NoError(t, err)
	assert.Equal(t, int64(6), run.ID)
	jobs.EXPECT().Retry(ctx, int64(9)).Return(nil, rivertype.ErrNotFound)
	_, err = svc.Retry(ctx, 9)
	assert.ErrorIs(t, err, ErrNotFound)

	jobs.EXPECT().Runs(ctx, nil, []rivertype.JobState{rivertype.JobStateDiscarded, rivertype.JobStateRetryable}, 20).
		Return([]*rivertype.JobRow{{ID: 3, Kind: "normalize_snapshot", Metadata: json.RawMessage(`{}`)}}, nil)
	failures, err := svc.Failures(ctx, 20)
	require.NoError(t, err)
	require.Len(t, failures, 1)
	assert.Equal(t, "normalize_snapshot", failures[0].Kind)
}

package queue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/robfig/cron/v3"
)

// TaskTimeout bounds a run of a scheduled task.
const TaskTimeout = 15 * time.Minute

// TaskMaxAttempts is how many times a scheduled task runs before River
// discards it, its backoff growing between attempts.
const TaskMaxAttempts = 5

// Maintainer runs the scheduled tasks; the service package implements it.
type Maintainer interface {
	PurgeSnapshots(ctx context.Context) (int64, error)
	PurgeRemovedEntities(ctx context.Context) (int64, error)
	PurgeExpiredTokens(ctx context.Context) (tokens, sessions int64, err error)
	DetectTokenConflicts(ctx context.Context) (int, error)
	PurgeInactiveHosts(ctx context.Context) (int, error)
}

var errNoMaintainer = errors.New("no maintainer: this client only inserts jobs")

// The jobs of the scheduled tasks: they take no argument.
type (
	PurgeSnapshotsArgs       struct{}
	PurgeRemovedEntitiesArgs struct{}
	PurgeExpiredTokensArgs   struct{}
	DetectTokenConflictsArgs struct{}
	PurgeInactiveHostsArgs   struct{}
)

func (PurgeSnapshotsArgs) Kind() string       { return "purge_snapshots" }
func (PurgeRemovedEntitiesArgs) Kind() string { return "purge_removed_entities" }
func (PurgeExpiredTokensArgs) Kind() string   { return "purge_expired_tokens" }
func (DetectTokenConflictsArgs) Kind() string { return "detect_token_conflicts" }
func (PurgeInactiveHostsArgs) Kind() string   { return "purge_inactive_hosts" }

// taskOpts runs a task 5 times at most, and once at a time: a run asked
// while one waits or runs is the same run.
func taskOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: TaskMaxAttempts,
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
			rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning,
			rivertype.JobStateRetryable, rivertype.JobStateScheduled,
		}},
	}
}

func (PurgeSnapshotsArgs) InsertOpts() river.InsertOpts       { return taskOpts() }
func (PurgeRemovedEntitiesArgs) InsertOpts() river.InsertOpts { return taskOpts() }
func (PurgeExpiredTokensArgs) InsertOpts() river.InsertOpts   { return taskOpts() }
func (DetectTokenConflictsArgs) InsertOpts() river.InsertOpts { return taskOpts() }
func (PurgeInactiveHostsArgs) InsertOpts() river.InsertOpts   { return taskOpts() }

// Task is a scheduled task: its job, given a schedule by
// workers.schedules[Kind].
type Task struct {
	Kind string
	args river.JobArgs
}

// Tasks are the scheduled tasks, in the order the administration lists
// them.
var Tasks = []Task{
	{Kind: PurgeSnapshotsArgs{}.Kind(), args: PurgeSnapshotsArgs{}},
	{Kind: PurgeRemovedEntitiesArgs{}.Kind(), args: PurgeRemovedEntitiesArgs{}},
	{Kind: PurgeExpiredTokensArgs{}.Kind(), args: PurgeExpiredTokensArgs{}},
	{Kind: DetectTokenConflictsArgs{}.Kind(), args: DetectTokenConflictsArgs{}},
	{Kind: PurgeInactiveHostsArgs{}.Kind(), args: PurgeInactiveHostsArgs{}},
}

// TaskByKind returns the task of the kind.
func TaskByKind(kind string) (Task, bool) {
	for _, t := range Tasks {
		if t.Kind == kind {
			return t, true
		}
	}
	return Task{}, false
}

// ParseSchedule reads a cron schedule of five fields, or a descriptor such
// as @daily.
func ParseSchedule(spec string) (cron.Schedule, error) {
	s, err := cron.ParseStandard(spec)
	if err != nil {
		return nil, fmt.Errorf("invalid cron schedule %q: %w", spec, err)
	}
	return s, nil
}

// taskWorker runs a scheduled task and records what it changed as the
// output of its job. It reads the maintainer of w when it runs: the
// maintainer itself enqueues through the client.
type taskWorker[T river.JobArgs] struct {
	river.WorkerDefaults[T]
	run func(ctx context.Context, m Maintainer) (any, error)
	w   *Workers
}

func (w *taskWorker[T]) Timeout(*river.Job[T]) time.Duration { return TaskTimeout }

func (w *taskWorker[T]) Work(ctx context.Context, _ *river.Job[T]) error {
	if w.w.Maintainer == nil {
		return errNoMaintainer
	}
	out, err := w.run(ctx, w.w.Maintainer)
	if err != nil {
		return err
	}
	return river.RecordOutput(ctx, out)
}

// addTaskWorkers registers the workers of the scheduled tasks.
func addTaskWorkers(workers *river.Workers, w *Workers) {
	river.AddWorker(workers, &taskWorker[PurgeSnapshotsArgs]{w: w, run: func(ctx context.Context, m Maintainer) (any, error) {
		n, err := m.PurgeSnapshots(ctx)
		return map[string]int64{"snapshots": n}, err
	}})
	river.AddWorker(workers, &taskWorker[PurgeRemovedEntitiesArgs]{w: w, run: func(ctx context.Context, m Maintainer) (any, error) {
		n, err := m.PurgeRemovedEntities(ctx)
		return map[string]int64{"entities": n}, err
	}})
	river.AddWorker(workers, &taskWorker[PurgeExpiredTokensArgs]{w: w, run: func(ctx context.Context, m Maintainer) (any, error) {
		tokens, sessions, err := m.PurgeExpiredTokens(ctx)
		return map[string]int64{"tokens": tokens, "sessions": sessions}, err
	}})
	river.AddWorker(workers, &taskWorker[DetectTokenConflictsArgs]{w: w, run: func(ctx context.Context, m Maintainer) (any, error) {
		n, err := m.DetectTokenConflicts(ctx)
		return map[string]int{"conflicts": n}, err
	}})
	river.AddWorker(workers, &taskWorker[PurgeInactiveHostsArgs]{w: w, run: func(ctx context.Context, m Maintainer) (any, error) {
		n, err := m.PurgeInactiveHosts(ctx)
		return map[string]int{"hosts": n}, err
	}})
}

// periodicJobs gives the tasks of schedules their schedule; an empty one
// leaves its task to the runs asked by hand.
func periodicJobs(schedules map[string]string) ([]*river.PeriodicJob, error) {
	var jobs []*river.PeriodicJob
	for _, t := range Tasks {
		spec := schedules[t.Kind]
		if spec == "" {
			continue
		}
		s, err := ParseSchedule(spec)
		if err != nil {
			return nil, fmt.Errorf("workers.schedules.%s: %w", t.Kind, err)
		}
		args := t.args
		jobs = append(jobs, river.NewPeriodicJob(s, func() (river.JobArgs, *river.InsertOpts) { return args, nil },
			&river.PeriodicJobOpts{ID: t.Kind}))
	}
	return jobs, nil
}

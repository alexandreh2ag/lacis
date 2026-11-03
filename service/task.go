package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/queue"
	"github.com/riverqueue/river/rivertype"
)

// TaskRunsShown is how many runs of a task the administration shows.
const TaskRunsShown = 5

// Task is a scheduled task: its cron schedule, nil when only run by hand,
// its next run and its last runs.
type Task struct {
	Kind      string
	Schedule  *string
	NextRunAt *time.Time
	Runs      []TaskRun
}

// TaskRun is a run of a job: its state as River gives it (available,
// running, completed, retryable, discarded…), its errors, oldest first,
// and what it changed.
type TaskRun struct {
	ID          int64
	Kind        string
	State       string
	Attempt     int
	MaxAttempts int
	CreatedAt   time.Time
	AttemptedAt *time.Time
	FinalizedAt *time.Time
	Errors      []string
	Output      json.RawMessage
}

type TaskService interface {
	List(ctx context.Context) ([]Task, error)
	// Failures returns the jobs of any kind River discarded, or retries,
	// newest first.
	Failures(ctx context.Context, limit int) ([]TaskRun, error)
	// Run asks for a run of the scheduled task now; ErrNotFound for an
	// unknown kind.
	Run(ctx context.Context, kind string) (*TaskRun, error)
	// Retry runs a job failed again; ErrNotFound for an unknown one.
	Retry(ctx context.Context, id int64) (*TaskRun, error)
}

type taskService struct {
	appCtx *appContext.Context
	jobs   queue.Admin
}

func NewTaskService(appCtx *appContext.Context, jobs queue.Admin) TaskService {
	return &taskService{appCtx: appCtx, jobs: jobs}
}

func (s *taskService) List(ctx context.Context) ([]Task, error) {
	now := s.appCtx.Clock.Now()
	out := make([]Task, 0, len(queue.Tasks))
	for _, t := range queue.Tasks {
		task := Task{Kind: t.Kind, Runs: []TaskRun{}}
		if spec := s.appCtx.Config.Workers.Schedules[t.Kind]; spec != "" {
			task.Schedule = &spec
			if sched, err := queue.ParseSchedule(spec); err == nil {
				next := sched.Next(now)
				task.NextRunAt = &next
			}
		}
		rows, err := s.jobs.Runs(ctx, []string{t.Kind}, nil, TaskRunsShown)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			task.Runs = append(task.Runs, taskRun(row))
		}
		out = append(out, task)
	}
	return out, nil
}

func (s *taskService) Failures(ctx context.Context, limit int) ([]TaskRun, error) {
	rows, err := s.jobs.Runs(ctx, nil, []rivertype.JobState{rivertype.JobStateDiscarded, rivertype.JobStateRetryable}, limit)
	if err != nil {
		return nil, err
	}
	out := make([]TaskRun, 0, len(rows))
	for _, row := range rows {
		out = append(out, taskRun(row))
	}
	return out, nil
}

func (s *taskService) Run(ctx context.Context, kind string) (*TaskRun, error) {
	row, err := s.jobs.RunTask(ctx, kind)
	if errors.Is(err, queue.ErrUnknownTask) {
		return nil, fmt.Errorf("%w: task %s", ErrNotFound, kind)
	}
	if err != nil {
		return nil, err
	}
	run := taskRun(row)
	return &run, nil
}

func (s *taskService) Retry(ctx context.Context, id int64) (*TaskRun, error) {
	row, err := s.jobs.Retry(ctx, id)
	if errors.Is(err, rivertype.ErrNotFound) {
		return nil, fmt.Errorf("%w: job %d", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	run := taskRun(row)
	return &run, nil
}

func taskRun(row *rivertype.JobRow) TaskRun {
	run := TaskRun{ID: row.ID, Kind: row.Kind, State: string(row.State), Attempt: row.Attempt, MaxAttempts: row.MaxAttempts,
		CreatedAt: row.CreatedAt, AttemptedAt: row.AttemptedAt, FinalizedAt: row.FinalizedAt, Errors: []string{}}
	for _, e := range row.Errors {
		run.Errors = append(run.Errors, e.Error)
	}
	if out := row.Output(); len(out) > 0 {
		run.Output = out
	}
	return run
}

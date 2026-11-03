// Package queue runs the background jobs on River, in the Lacis database.
package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/alexandreh2ag/lacis/config"
	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivertype"
	"gorm.io/gorm"
)

// Queue enqueues the jobs.
type Queue interface {
	// InsertNormalizeSnapshot enqueues the normalization of a stored
	// snapshot, in the transaction of db when db is one: a stored snapshot
	// always has its job.
	InsertNormalizeSnapshot(ctx context.Context, db *gorm.DB, snapshotID int64) error
	// InsertRecomputeLinks enqueues the recomputation of the links of a
	// namespace at the end of the current window, in the transaction of db
	// when db is one.
	InsertRecomputeLinks(ctx context.Context, db *gorm.DB, namespaceID uuid.UUID) error
}

// Admin shows the runs of the jobs and asks for new ones, for the
// administration of the scheduled tasks.
type Admin interface {
	// Runs returns the jobs of these kinds in these states, any when
	// empty, newest first.
	Runs(ctx context.Context, kinds []string, states []rivertype.JobState, limit int) ([]*rivertype.JobRow, error)
	// RunTask asks for a run of the scheduled task now; a run waiting or
	// running already is returned instead. ErrUnknownTask for another kind.
	RunTask(ctx context.Context, kind string) (*rivertype.JobRow, error)
	// Retry runs again a job failed or discarded; rivertype.ErrNotFound
	// for an unknown one.
	Retry(ctx context.Context, id int64) (*rivertype.JobRow, error)
}

// ErrUnknownTask refuses a kind no scheduled task has.
var ErrUnknownTask = errors.New("unknown task")

// Client is the River client. It inserts jobs and, once started, works
// them.
type Client struct {
	river *river.Client[*sql.Tx]
}

// Workers are what the jobs call; a client that only inserts takes none.
// Maintainer may be set once the client is built, before Start.
type Workers struct {
	Normalizer Normalizer
	Linker     Linker
	Maintainer Maintainer
}

// New returns a client on db. With w, Start works the jobs with
// cfg.Concurrency workers and runs the scheduled tasks on their schedule,
// once across the processes; without, the client only inserts.
func New(db *sql.DB, cfg config.WorkersConfig, logger *slog.Logger, w *Workers) (*Client, error) {
	work := w != nil
	if w == nil {
		w = &Workers{}
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, &NormalizeSnapshotWorker{normalizer: w.Normalizer, enqueue: insertRecompute})
	river.AddWorker(workers, &RecomputeLinksWorker{linker: w.Linker})
	addTaskWorkers(workers, w)
	riverCfg := &river.Config{
		Logger:                      logger,
		Workers:                     workers,
		CompletedJobRetentionPeriod: cfg.Retention.Completed,
		CancelledJobRetentionPeriod: cfg.Retention.Cancelled,
		DiscardedJobRetentionPeriod: cfg.Retention.Discarded,
	}
	if work {
		riverCfg.Queues = map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: cfg.Concurrency}}
		periodic, err := periodicJobs(cfg.Schedules)
		if err != nil {
			return nil, err
		}
		riverCfg.PeriodicJobs = periodic
	}
	c, err := river.NewClient(riverdatabasesql.New(db), riverCfg)
	if err != nil {
		return nil, fmt.Errorf("create job queue: %w", err)
	}
	return &Client{river: c}, nil
}

// Start works the jobs until Stop; it fails on a client that only inserts.
func (c *Client) Start(ctx context.Context) error {
	return c.river.Start(ctx)
}

// Stop waits for the running jobs, until ctx is done.
func (c *Client) Stop(ctx context.Context) error {
	return c.river.Stop(ctx)
}

func (c *Client) InsertNormalizeSnapshot(ctx context.Context, db *gorm.DB, snapshotID int64) error {
	return c.insert(ctx, db, NormalizeSnapshotArgs{SnapshotID: snapshotID})
}

func (c *Client) InsertRecomputeLinks(ctx context.Context, db *gorm.DB, namespaceID uuid.UUID) error {
	return c.insert(ctx, db, RecomputeLinksArgs{NamespaceID: namespaceID}, recomputeOpts(time.Now()))
}

func (c *Client) insert(ctx context.Context, db *gorm.DB, args river.JobArgs, opts ...*river.InsertOpts) error {
	var o *river.InsertOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	var err error
	if tx, ok := db.Statement.ConnPool.(*sql.Tx); ok {
		_, err = c.river.InsertTx(ctx, tx, args, o)
	} else {
		_, err = c.river.Insert(ctx, args, o)
	}
	if err != nil {
		return fmt.Errorf("enqueue %s: %w", args.Kind(), err)
	}
	return nil
}

func (c *Client) Runs(ctx context.Context, kinds []string, states []rivertype.JobState, limit int) ([]*rivertype.JobRow, error) {
	params := river.NewJobListParams().OrderBy(river.JobListOrderByID, river.SortOrderDesc).First(limit)
	if len(kinds) > 0 {
		params = params.Kinds(kinds...)
	}
	if len(states) > 0 {
		params = params.States(states...)
	}
	res, err := c.river.JobList(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	return res.Jobs, nil
}

func (c *Client) RunTask(ctx context.Context, kind string) (*rivertype.JobRow, error) {
	t, ok := TaskByKind(kind)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownTask, kind)
	}
	res, err := c.river.Insert(ctx, t.args, nil)
	if err != nil {
		return nil, fmt.Errorf("enqueue %s: %w", kind, err)
	}
	return res.Job, nil
}

func (c *Client) Retry(ctx context.Context, id int64) (*rivertype.JobRow, error) {
	return c.river.JobRetry(ctx, id)
}

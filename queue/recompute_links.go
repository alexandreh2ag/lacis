package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
)

// RecomputePeriod is the window the recomputations of a namespace are
// gathered in: the snapshots normalized in one window share one
// recomputation, run at its end.
const RecomputePeriod = 30 * time.Second

// RecomputeLinksArgs is the job computing again the links of a namespace.
type RecomputeLinksArgs struct {
	NamespaceID uuid.UUID `json:"namespace_id"`
}

func (RecomputeLinksArgs) Kind() string { return "recompute_links" }

// Linker computes again the links of a namespace; the service package
// implements it.
type Linker interface {
	Recompute(ctx context.Context, namespaceID uuid.UUID) (int, error)
}

var errNoLinker = errors.New("no linker: this client only inserts jobs")

// RecomputeLinksWorker runs the recomputation; River retries it on error.
type RecomputeLinksWorker struct {
	river.WorkerDefaults[RecomputeLinksArgs]
	linker Linker
}

func (w *RecomputeLinksWorker) Work(ctx context.Context, job *river.Job[RecomputeLinksArgs]) error {
	if w.linker == nil {
		return errNoLinker
	}
	_, err := w.linker.Recompute(ctx, job.Args.NamespaceID)
	return err
}

// recomputeOpts schedules the job at the end of the window of now, unique
// in it: River computes the window of a unique job from its scheduled time,
// so every insert of a window lands on the same job of the next one.
func recomputeOpts(now time.Time) *river.InsertOpts {
	return &river.InsertOpts{
		ScheduledAt: now.Truncate(RecomputePeriod).Add(RecomputePeriod),
		UniqueOpts:  river.UniqueOpts{ByArgs: true, ByPeriod: RecomputePeriod},
	}
}

// insertRecompute enqueues the recomputation with the client working the
// current job.
func insertRecompute(ctx context.Context, namespaceID uuid.UUID) error {
	client, err := river.ClientFromContextSafely[*sql.Tx](ctx)
	if err != nil {
		return err
	}
	if _, err := client.Insert(ctx, RecomputeLinksArgs{NamespaceID: namespaceID}, recomputeOpts(time.Now())); err != nil {
		return fmt.Errorf("enqueue recompute_links: %w", err)
	}
	return nil
}

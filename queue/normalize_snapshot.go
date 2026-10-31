package queue

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
)

// NormalizeSnapshotArgs is the job normalizing a stored snapshot into the
// tables of its host.
type NormalizeSnapshotArgs struct {
	SnapshotID int64 `json:"snapshot_id"`
}

func (NormalizeSnapshotArgs) Kind() string { return "normalize_snapshot" }

// Normalizer normalizes a stored snapshot and returns the namespace of its
// host, uuid.Nil when skipped; the service package implements it, which
// itself enqueues through this package.
type Normalizer interface {
	Normalize(ctx context.Context, snapshotID int64) (uuid.UUID, error)
}

var errNoNormalizer = errors.New("no normalizer: this client only inserts jobs")

// NormalizeSnapshotWorker runs the normalization, then enqueues the
// recomputation of the links of the namespace; River retries it on error.
type NormalizeSnapshotWorker struct {
	river.WorkerDefaults[NormalizeSnapshotArgs]
	normalizer Normalizer
	// enqueue inserts the recomputation; tests replace it.
	enqueue func(ctx context.Context, namespaceID uuid.UUID) error
}

func (w *NormalizeSnapshotWorker) Work(ctx context.Context, job *river.Job[NormalizeSnapshotArgs]) error {
	if w.normalizer == nil {
		return errNoNormalizer
	}
	namespaceID, err := w.normalizer.Normalize(ctx, job.Args.SnapshotID)
	if err != nil || namespaceID == uuid.Nil {
		return err
	}
	return w.enqueue(ctx, namespaceID)
}

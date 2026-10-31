package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type normalizerFunc func(ctx context.Context, id int64) (uuid.UUID, error)

func (f normalizerFunc) Normalize(ctx context.Context, id int64) (uuid.UUID, error) {
	return f(ctx, id)
}

func TestNormalizeSnapshotWorker(t *testing.T) {
	job := &river.Job[NormalizeSnapshotArgs]{Args: NormalizeSnapshotArgs{SnapshotID: 42}}
	namespaceID := uuid.New()
	errDB := errors.New("db down")
	errQueue := errors.New("queue down")
	tests := []struct {
		name       string
		normalized uuid.UUID
		normalize  error
		enqueue    error
		want       error
		enqueued   []uuid.UUID
	}{
		{name: "normalized: links recomputed", normalized: namespaceID, enqueued: []uuid.UUID{namespaceID}},
		{name: "skipped: nothing to recompute", normalized: uuid.Nil},
		{name: "failed: returned for River to retry", normalize: errDB, want: errDB},
		{name: "enqueue failed: retried", normalized: namespaceID, enqueue: errQueue, want: errQueue, enqueued: []uuid.UUID{namespaceID}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var enqueued []uuid.UUID
			w := &NormalizeSnapshotWorker{
				normalizer: normalizerFunc(func(_ context.Context, id int64) (uuid.UUID, error) {
					assert.Equal(t, int64(42), id)
					return tt.normalized, tt.normalize
				}),
				enqueue: func(_ context.Context, id uuid.UUID) error {
					enqueued = append(enqueued, id)
					return tt.enqueue
				},
			}
			assert.ErrorIs(t, w.Work(t.Context(), job), tt.want)
			assert.Equal(t, tt.enqueued, enqueued)
		})
	}

	assert.ErrorIs(t, (&NormalizeSnapshotWorker{}).Work(t.Context(), job), errNoNormalizer)
}

type linkerFunc func(ctx context.Context, namespaceID uuid.UUID) (int, error)

func (f linkerFunc) Recompute(ctx context.Context, namespaceID uuid.UUID) (int, error) {
	return f(ctx, namespaceID)
}

func TestRecomputeLinksWorker(t *testing.T) {
	namespaceID := uuid.New()
	job := &river.Job[RecomputeLinksArgs]{Args: RecomputeLinksArgs{NamespaceID: namespaceID}}
	errDB := errors.New("db down")
	w := &RecomputeLinksWorker{linker: linkerFunc(func(_ context.Context, id uuid.UUID) (int, error) {
		assert.Equal(t, namespaceID, id)
		return 0, errDB
	})}
	assert.ErrorIs(t, w.Work(t.Context(), job), errDB)
	assert.ErrorIs(t, (&RecomputeLinksWorker{}).Work(t.Context(), job), errNoLinker)
}

func TestRecomputeOpts(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.TimeOnly, s)
		require.NoError(t, err)
		return v
	}
	assert.Equal(t, at("10:00:30"), recomputeOpts(at("10:00:00")).ScheduledAt)
	assert.Equal(t, at("10:00:30"), recomputeOpts(at("10:00:29")).ScheduledAt, "same window, same job")
	assert.Equal(t, at("10:01:00"), recomputeOpts(at("10:00:30")).ScheduledAt)
	opts := recomputeOpts(at("10:00:00")).UniqueOpts
	assert.True(t, opts.ByArgs)
	assert.Equal(t, RecomputePeriod, opts.ByPeriod)
}

func TestInsertRecompute_NoClient(t *testing.T) {
	assert.Error(t, insertRecompute(t.Context(), uuid.New()), "outside a job, no client to insert with")
}

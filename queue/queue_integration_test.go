//go:build integration

package queue

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/database/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

// jobState returns the state of the last normalize_snapshot job of
// snapshotID, or "" when there is none.
func jobState(t *testing.T, db *gorm.DB, snapshotID int64) string {
	var state string
	err := db.Raw(`select state from river_job where kind = 'normalize_snapshot' and (args->>'snapshot_id')::bigint = ? order by id desc limit 1`, snapshotID).
		Row().Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	require.NoError(t, err)
	return state
}

func TestInsertNormalizeSnapshot(t *testing.T) {
	db := dbtest.Open(t, "queue")
	sqlDB, err := db.DB()
	require.NoError(t, err)
	c, err := New(sqlDB, config.DefaultConfig().Workers, discard, nil)
	require.NoError(t, err)
	base := time.Now().UnixMilli() % 1_000_000_000 * 10

	// Rolled back with its transaction.
	errRollback := errors.New("rollback")
	err = db.Transaction(func(tx *gorm.DB) error {
		require.NoError(t, c.InsertNormalizeSnapshot(t.Context(), tx, base+1))
		assert.Equal(t, "available", jobState(t, tx, base+1), "visible in the transaction")
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
	assert.Empty(t, jobState(t, db, base+1))

	// Committed with its transaction, or inserted alone outside of one.
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return c.InsertNormalizeSnapshot(t.Context(), tx, base+2)
	}))
	assert.Equal(t, "available", jobState(t, db, base+2))
	require.NoError(t, c.InsertNormalizeSnapshot(t.Context(), db, base+3))
	assert.Equal(t, "available", jobState(t, db, base+3))
}

func TestWorkers(t *testing.T) {
	db := dbtest.Open(t, "queue")
	sqlDB, err := db.DB()
	require.NoError(t, err)
	namespaceID := uuid.New()
	normalizer := normalizerFunc(func(context.Context, int64) (uuid.UUID, error) { return namespaceID, nil })
	c, err := New(sqlDB, config.DefaultConfig().Workers, discard, &Workers{Normalizer: normalizer})
	require.NoError(t, err)
	require.NoError(t, c.Start(t.Context()))
	t.Cleanup(func() { assert.NoError(t, c.Stop(context.Background())) }) // t.Context() is done by then

	id := time.Now().UnixMilli()%1_000_000_000*10 + 5
	require.NoError(t, c.InsertNormalizeSnapshot(t.Context(), db, id))
	require.NoError(t, c.InsertNormalizeSnapshot(t.Context(), db, id+1))
	assert.Eventually(t, func() bool {
		return jobState(t, db, id) == "completed" && jobState(t, db, id+1) == "completed"
	}, 10*time.Second, 100*time.Millisecond)

	// Both normalizations of the window share one recomputation, run at its
	// end.
	var states []string
	require.NoError(t, db.Raw(`select state from river_job where kind = 'recompute_links' and args->>'namespace_id' = ?`, namespaceID.String()).
		Scan(&states).Error)
	assert.Equal(t, []string{"scheduled"}, states)
}

func TestNew_InsertOnlyCannotStart(t *testing.T) {
	db := dbtest.Open(t, "queue")
	sqlDB, err := db.DB()
	require.NoError(t, err)
	c, err := New(sqlDB, config.DefaultConfig().Workers, discard, nil)
	require.NoError(t, err)
	assert.Error(t, c.Start(t.Context()))
}

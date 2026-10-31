package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestAlternating(t *testing.T) {
	tests := []struct {
		hostnames []string
		want      []string
	}{
		{hostnames: nil},
		{hostnames: []string{"web"}},
		{hostnames: []string{"web", "web", "web"}},
		{hostnames: []string{"old", "new", "new"}, want: nil},
		{hostnames: []string{"web-b", "web-a", "web-b"}, want: []string{"web-a", "web-b"}},
		{hostnames: []string{"a", "a", "b", "c", "a"}, want: []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, alternating(tt.hostnames), "%v", tt.hostnames)
	}
}

// hostDeleter records the hosts deleted, and refuses those of errs.
type hostDeleter struct {
	MetadataService
	deleted []uuid.UUID
	errs    map[uuid.UUID]error
}

func (d *hostDeleter) DeleteHost(_ context.Context, _ string, id uuid.UUID) error {
	if err := d.errs[id]; err != nil {
		return err
	}
	d.deleted = append(d.deleted, id)
	return nil
}

func TestMaintenanceService(t *testing.T) {
	m := newMocks(t)
	appCtx := testContext(t)
	appCtx.Config.Workers.Params.PurgeRemovedEntitiesAfter = 90 * 24 * time.Hour
	appCtx.Config.Workers.Params.PurgeInactiveHostsAfter = 180 * 24 * time.Hour
	gone, back, active, failing := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	errDB := errors.New("db down")
	deleter := &hostDeleter{errs: map[uuid.UUID]error{back: ErrNotFound, active: ErrHostActive, failing: errDB}}
	svc := NewMaintenanceService(appCtx, m.tx, deleter)

	m.snapshot.EXPECT().PurgeOutdated(ctx, now).Return(int64(12), nil)
	n, err := svc.PurgeSnapshots(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 12, n)

	m.inventory.EXPECT().PurgeRemoved(ctx, now.Add(-90*24*time.Hour)).Return(int64(3), nil)
	n, err = svc.PurgeRemovedEntities(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 3, n)

	m.token.EXPECT().PurgeExpired(ctx, now).Return(int64(2), nil)
	m.session.EXPECT().DeleteExpired(ctx, now).Return(int64(5), nil)
	tokens, sessions, err := svc.PurgeExpiredTokens(ctx)
	require.NoError(t, err)
	assert.Equal(t, [2]int64{2, 5}, [2]int64{tokens, sessions})
	m.token.EXPECT().PurgeExpired(ctx, now).Return(int64(0), errDB)
	_, _, err = svc.PurgeExpiredTokens(ctx)
	assert.ErrorIs(t, err, errDB)

	cloned, renamed := uuid.New(), uuid.New()
	m.snapshot.EXPECT().FindHostnames(ctx, now.Add(-ConflictWindow)).Return(map[uuid.UUID][]string{
		cloned: {"web-b", "web-a", "web-b"}, renamed: {"old", "new"},
	}, nil)
	m.agent.EXPECT().SetConflicts(ctx, map[uuid.UUID][]string{cloned: {"web-a", "web-b"}}, now).Return(nil)
	conflicts, err := svc.DetectTokenConflicts(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, conflicts)

	m.host.EXPECT().FindSeenBefore(ctx, now.Add(-180*24*time.Hour)).Return([]repository.HostRef{
		{ID: gone, Namespace: "acme"}, {ID: back, Namespace: "acme"}, {ID: active, Namespace: "acme"},
	}, nil)
	purged, err := svc.PurgeInactiveHosts(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, purged, "hosts deleted meanwhile or back are left alone")
	assert.Equal(t, []uuid.UUID{gone}, deleter.deleted)

	m.host.EXPECT().FindSeenBefore(ctx, gomock.Any()).Return([]repository.HostRef{{ID: failing, Namespace: "acme"}}, nil)
	_, err = svc.PurgeInactiveHosts(ctx)
	assert.ErrorIs(t, err, errDB)
	m.host.EXPECT().FindSeenBefore(ctx, gomock.Any()).Return(nil, errDB)
	_, err = svc.PurgeInactiveHosts(ctx)
	assert.ErrorIs(t, err, errDB)
}

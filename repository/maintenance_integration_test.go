//go:build integration

package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// snapshot stores a snapshot of the host named hostname, received at.
func (f *fixture) snapshot(t *testing.T, hostID uuid.UUID, hostname string, at time.Time) *model.Snapshot {
	t.Helper()
	s := &model.Snapshot{HostID: hostID, CollectedAt: at, ReceivedAt: at, Hash: uuid.NewString(), SchemaVersion: 1,
		Payload: json.RawMessage(`{"host":{"hostname":"` + hostname + `"}}`)}
	require.NoError(t, f.repos.Snapshot.Create(ctx, s))
	return s
}

func TestSnapshotRepository_PurgeOutdated(t *testing.T) {
	f := newFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, f.db.Exec("update namespaces set snapshot_retention = interval '7 days' where id = ?", f.namespace.ID).Error)
	old := f.snapshot(t, f.host.ID, "web", now.Add(-10*24*time.Hour))
	kept := f.snapshot(t, f.host.ID, "web", now.Add(-24*time.Hour))
	recent := f.snapshot(t, f.host.ID, "web", now.Add(-time.Hour))

	// A host seen 20 days ago keeps its latest snapshot, out of retention.
	agent := &model.Agent{TokenHash: "other", EnrollmentTokenID: f.token.ID}
	require.NoError(t, f.repos.Agent.Create(ctx, agent))
	quiet := &model.Host{AgentID: agent.ID, NamespaceID: f.namespace.ID, Hostname: "quiet", FirstSeenAt: now, LastSeenAt: now}
	require.NoError(t, f.repos.Host.Create(ctx, quiet))
	quietOld := f.snapshot(t, quiet.ID, "quiet", now.Add(-30*24*time.Hour))
	quietLatest := f.snapshot(t, quiet.ID, "quiet", now.Add(-20*24*time.Hour))

	n, err := f.repos.Snapshot.PurgeOutdated(ctx, now)
	require.NoError(t, err)
	assert.EqualValues(t, 2, n)
	for _, s := range []*model.Snapshot{old, quietOld} {
		_, err := f.repos.Snapshot.FindByID(ctx, s.ID)
		assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	}
	for _, s := range []*model.Snapshot{kept, recent, quietLatest} {
		_, err := f.repos.Snapshot.FindByID(ctx, s.ID)
		assert.NoError(t, err, "the latest of each host stays, out of retention or not")
	}
	n, err = f.repos.Snapshot.PurgeOutdated(ctx, now)
	require.NoError(t, err)
	assert.Zero(t, n, "idempotent")
}

func TestSnapshotRepository_FindHostnames(t *testing.T) {
	f := newFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	f.snapshot(t, f.host.ID, "too-old", now.Add(-2*24*time.Hour))
	f.snapshot(t, f.host.ID, "web-a", now.Add(-3*time.Hour))
	f.snapshot(t, f.host.ID, "web-b", now.Add(-2*time.Hour))
	f.snapshot(t, f.host.ID, "web-a", now.Add(-time.Hour))

	got, err := f.repos.Snapshot.FindHostnames(ctx, now.Add(-24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, map[uuid.UUID][]string{f.agent.ID: {"web-a", "web-b", "web-a"}}, got)
}

func TestMaintenance_Purges(t *testing.T) {
	f := newFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)

	// Hosts seen before.
	refs, err := f.repos.Host.FindSeenBefore(ctx, now.Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, []HostRef{{ID: f.host.ID, Hostname: "acme-site-prod-1", Namespace: "acme"}}, refs)
	refs, err = f.repos.Host.FindSeenBefore(ctx, now.Add(-time.Hour))
	require.NoError(t, err)
	assert.Empty(t, refs)

	// Removed content.
	long, recently := now.Add(-100*24*time.Hour), now.Add(-time.Hour)
	gone := &model.Service{NamespaceID: f.namespace.ID, HostID: f.host.ID, Type: "redis", Instance: "a", Details: json.RawMessage(`{}`), FirstSeenAt: long, RemovedAt: &long}
	removed := &model.Service{NamespaceID: f.namespace.ID, HostID: f.host.ID, Type: "redis", Instance: "b", Details: json.RawMessage(`{}`), FirstSeenAt: long, RemovedAt: &recently}
	live := &model.Runtime{NamespaceID: f.namespace.ID, HostID: f.host.ID, Type: "php", Version: "8.3", Path: "/usr/bin/php", FirstSeenAt: long}
	require.NoError(t, f.repos.Service.Save(ctx, gone))
	require.NoError(t, f.repos.Service.Save(ctx, removed))
	require.NoError(t, f.repos.Runtime.Save(ctx, live))
	require.NoError(t, f.repos.Endpoint.Replace(ctx, f.host.ID, model.EndpointOwnerService, []model.Endpoint{
		{NamespaceID: f.namespace.ID, HostID: f.host.ID, Kind: model.EndpointKindTCP, Address: "10.0.0.1", OwnerType: model.EndpointOwnerService, OwnerID: gone.ID, RemovedAt: &long},
	}))
	n, err := f.repos.Inventory.PurgeRemoved(ctx, now.Add(-90*24*time.Hour))
	require.NoError(t, err)
	assert.EqualValues(t, 2, n, "the service and its endpoint")
	services, err := f.repos.Service.FindByHost(ctx, f.host.ID)
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Equal(t, removed.ID, services[0].ID, "removed recently, it stays")

	// Expired tokens: the one of the fixture enrolled an agent and stays.
	past := now.Add(-time.Hour)
	expired := &model.EnrollmentToken{TokenHash: "expired", ExpiresAt: &past}
	later := now.Add(time.Hour)
	valid := &model.EnrollmentToken{TokenHash: "valid", ExpiresAt: &later}
	require.NoError(t, f.repos.EnrollmentToken.Create(ctx, expired))
	require.NoError(t, f.repos.EnrollmentToken.Create(ctx, valid))
	require.NoError(t, f.db.Exec("update enrollment_tokens set expires_at = ? where id = ?", past, f.token.ID).Error)
	n, err = f.repos.EnrollmentToken.PurgeExpired(ctx, now)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	_, err = f.repos.EnrollmentToken.FindByID(ctx, expired.ID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	for _, id := range []uuid.UUID{valid.ID, f.token.ID} {
		_, err = f.repos.EnrollmentToken.FindByID(ctx, id)
		assert.NoError(t, err)
	}
}

func TestAgentRepository_SetConflicts(t *testing.T) {
	f := newFixture(t)
	other := &model.Agent{TokenHash: "other", EnrollmentTokenID: f.token.ID}
	require.NoError(t, f.repos.Agent.Create(ctx, other))
	first := time.Now().UTC().Truncate(time.Microsecond)

	require.NoError(t, f.repos.Agent.SetConflicts(ctx, map[uuid.UUID][]string{f.agent.ID: {"web-a", "web-b"}, other.ID: {"x", "y"}}, first))
	require.NoError(t, f.repos.Agent.SetConflicts(ctx, map[uuid.UUID][]string{f.agent.ID: {"web-a", "web-b", "web-c"}}, first.Add(time.Hour)))

	got, err := f.repos.Agent.FindByID(ctx, f.agent.ID)
	require.NoError(t, err)
	require.NotNil(t, got.ConflictAt)
	assert.True(t, first.Equal(*got.ConflictAt), "marked since the first detection")
	assert.Equal(t, model.StringArray{"web-a", "web-b", "web-c"}, got.ConflictHostnames)
	got, err = f.repos.Agent.FindByID(ctx, other.ID)
	require.NoError(t, err)
	assert.Nil(t, got.ConflictAt, "no longer seen, cleared")
	assert.Empty(t, got.ConflictHostnames)

	rows, err := f.repos.Agent.FindAll(ctx)
	require.NoError(t, err)
	for _, r := range rows {
		if r.ID == f.agent.ID {
			assert.Equal(t, model.StringArray{"web-a", "web-b", "web-c"}, r.ConflictHostnames, "listed with the agents")
		}
	}

	require.NoError(t, f.repos.Agent.SetConflicts(ctx, nil, first))
	got, err = f.repos.Agent.FindByID(ctx, f.agent.ID)
	require.NoError(t, err)
	assert.Nil(t, got.ConflictAt)
}

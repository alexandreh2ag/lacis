//go:build integration

package repository

import (
	"encoding/json"
	"net/netip"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestNetworkRepository_EnsureAndFindByIP(t *testing.T) {
	f := newFixture(t)
	repo := f.repos.Network

	wide := &model.Network{NamespaceID: f.namespace.ID, Slug: "10-0-0-0-16", Name: "10.0.0.0/16"}
	require.NoError(t, repo.Ensure(ctx, wide))
	again := &model.Network{NamespaceID: f.namespace.ID, Slug: "10-0-0-0-16", Name: "renamed"}
	require.NoError(t, repo.Ensure(ctx, again))
	assert.Equal(t, wide.ID, again.ID, "an existing network is loaded")
	assert.Equal(t, "10.0.0.0/16", again.Name)

	cidr := func(n *model.Network, p string) *model.NetworkCIDR {
		return &model.NetworkCIDR{NetworkID: n.ID, NamespaceID: f.namespace.ID, CIDR: model.CIDR{Prefix: netip.MustParsePrefix(p)}}
	}
	require.NoError(t, repo.AddCIDR(ctx, cidr(wide, "10.0.0.0/16")))
	require.NoError(t, repo.AddCIDR(ctx, cidr(wide, "10.0.0.0/16")), "adding it again does nothing")
	require.NoError(t, repo.AddCIDR(ctx, cidr(f.network, "10.0.1.0/24")))

	got, err := repo.FindByIP(ctx, f.namespace.ID, netip.MustParseAddr("10.0.1.20"))
	require.NoError(t, err)
	assert.Equal(t, f.network.ID, got.ID, "the most specific CIDR wins")
	got, err = repo.FindByIP(ctx, f.namespace.ID, netip.MustParseAddr("10.0.2.20"))
	require.NoError(t, err)
	assert.Equal(t, wide.ID, got.ID)
	_, err = repo.FindByIP(ctx, f.namespace.ID, netip.MustParseAddr("192.168.1.1"))
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, err = repo.FindByIP(ctx, uuid.New(), netip.MustParseAddr("10.0.1.20"))
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound, "never another namespace")
}

func TestCatalogRepositories(t *testing.T) {
	f := newFixture(t)
	p1, err := f.repos.Project.Ensure(ctx, f.namespace.ID, "shop")
	require.NoError(t, err)
	assert.Equal(t, "shop", p1.Name)
	p2, err := f.repos.Project.Ensure(ctx, f.namespace.ID, "shop")
	require.NoError(t, err)
	assert.Equal(t, p1.ID, p2.ID)

	e1, err := f.repos.Environment.Ensure(ctx, f.namespace.ID, "prod")
	require.NoError(t, err)
	e2, err := f.repos.Environment.Ensure(ctx, f.namespace.ID, "prod")
	require.NoError(t, err)
	assert.Equal(t, e1.ID, e2.ID)

	_, err = f.repos.Project.WithTx(f.db).Ensure(ctx, uuid.New(), "x")
	assert.Error(t, err, "unknown namespace")
	_, err = f.repos.Environment.WithTx(f.db).Ensure(ctx, uuid.New(), "x")
	assert.Error(t, err)
}

func TestHostContentRepositories(t *testing.T) {
	f := newFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)

	ips := []model.HostIP{
		{HostID: f.host.ID, IP: model.IP{Addr: netip.MustParseAddr("10.0.1.20")}, PrefixLen: 24, DefaultRoute: true},
		{HostID: f.host.ID, IP: model.IP{Addr: netip.MustParseAddr("203.0.113.20")}, PrefixLen: 27},
	}
	require.NoError(t, f.repos.HostIP.Replace(ctx, f.host.ID, ips))
	require.NoError(t, f.repos.HostIP.WithTx(f.db).Replace(ctx, f.host.ID, ips[:1]))
	gotIPs, err := f.repos.HostIP.FindByHost(ctx, f.host.ID)
	require.NoError(t, err)
	require.Len(t, gotIPs, 1)
	assert.Equal(t, 24, gotIPs[0].PrefixLen)
	assert.True(t, gotIPs[0].DefaultRoute)
	require.NoError(t, f.repos.HostIP.Replace(ctx, f.host.ID, nil))

	r1 := &model.Runtime{NamespaceID: f.namespace.ID, HostID: f.host.ID, Type: "php", Version: "8.3.14", Path: "/usr/bin/php8.3", FirstSeenAt: now}
	r2 := &model.Runtime{NamespaceID: f.namespace.ID, HostID: f.host.ID, Type: "node", Version: "22.14.0", Path: "/usr/bin/node", FirstSeenAt: now}
	require.NoError(t, f.repos.Runtime.Save(ctx, r1))
	require.NoError(t, f.repos.Runtime.WithTx(f.db).Save(ctx, r2))
	require.NoError(t, f.repos.Runtime.MarkRemoved(ctx, f.host.ID, []uuid.UUID{r1.ID}, now))
	runtimes, err := f.repos.Runtime.FindByHost(ctx, f.host.ID)
	require.NoError(t, err)
	require.Len(t, runtimes, 2)
	for _, r := range runtimes {
		assert.Equal(t, r.ID == r2.ID, r.RemovedAt != nil, r.Type)
	}

	svc := &model.Service{NamespaceID: f.namespace.ID, HostID: f.host.ID, Type: "redis", Details: json.RawMessage(`{}`), FirstSeenAt: now}
	require.NoError(t, f.repos.Service.Save(ctx, svc))
	assert.Equal(t, "default", svc.Instance, "database default")
	require.NoError(t, f.repos.Service.WithTx(f.db).MarkRemoved(ctx, f.host.ID, nil, now))
	services, err := f.repos.Service.FindByHost(ctx, f.host.ID)
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.NotNil(t, services[0].RemovedAt, "nothing kept: removed")

	port := 6379
	endpoints := []model.Endpoint{{NamespaceID: f.namespace.ID, HostID: f.host.ID, Kind: model.EndpointKindTCP, Address: "0.0.0.0", Port: &port, OwnerType: model.EndpointOwnerService, OwnerID: svc.ID}}
	require.NoError(t, f.repos.Endpoint.Replace(ctx, f.host.ID, model.EndpointOwnerService, endpoints))
	require.NoError(t, f.repos.Endpoint.WithTx(f.db).Replace(ctx, f.host.ID, model.EndpointOwnerService, endpoints[:1:1]))
	got, err := f.repos.Endpoint.FindByHost(ctx, f.host.ID)
	require.NoError(t, err)
	require.Len(t, got, 1, "replaced, not added")
	require.NoError(t, f.repos.Endpoint.Replace(ctx, f.host.ID, model.EndpointOwnerService, nil))
	got, err = f.repos.Endpoint.FindByHost(ctx, f.host.ID)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestChangeEventRepository_Purge(t *testing.T) {
	f := newFixture(t)
	other := uuid.New()
	base := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	event := func(ns uuid.UUID, minutes int) model.ChangeEvent {
		return model.ChangeEvent{NamespaceID: ns, EntityType: model.ChangeEntityHost, Kind: model.ChangeAdded,
			After: json.RawMessage(`{}`), OccurredAt: base.Add(time.Duration(minutes) * time.Minute)}
	}
	// Minutes 0 to 4, two events at minute 2; another namespace.
	events := []model.ChangeEvent{event(f.namespace.ID, 0), event(f.namespace.ID, 1), event(f.namespace.ID, 2),
		event(f.namespace.ID, 2), event(f.namespace.ID, 3), event(f.namespace.ID, 4), event(other, 0)}
	require.NoError(t, f.repos.ChangeEvent.Create(ctx, events))
	require.NoError(t, f.repos.ChangeEvent.Create(ctx, nil))

	deleted, err := f.repos.ChangeEvent.Purge(ctx, f.namespace.ID, 10)
	require.NoError(t, err)
	assert.Zero(t, deleted, "fewer events than kept")

	deleted, err = f.repos.ChangeEvent.Purge(ctx, f.namespace.ID, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted, "the events at the time of the oldest kept stay")
	rows, err := f.repos.ChangeEvent.List(ctx, ChangeEventFilter{NamespaceID: f.namespace.ID})
	require.NoError(t, err)
	require.Len(t, rows, 4)
	assert.Equal(t, base.Add(2*time.Minute), rows[3].OccurredAt.UTC())
	rows, err = f.repos.ChangeEvent.List(ctx, ChangeEventFilter{NamespaceID: other})
	require.NoError(t, err)
	assert.Len(t, rows, 1, "never another namespace")
}

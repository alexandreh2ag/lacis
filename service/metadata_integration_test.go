//go:build integration

package service

import (
	"context"
	"testing"
	"time"

	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/linker"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// recomputes records the recomputations enqueued.
type recomputes struct {
	noQueue
	namespaces []uuid.UUID
}

func (q *recomputes) InsertRecomputeLinks(_ context.Context, _ *gorm.DB, id uuid.UUID) error {
	q.namespaces = append(q.namespaces, id)
	return nil
}

func newMetadataFixture(t *testing.T) (*normalizeFixture, MetadataService, *fakeClock, *recomputes) {
	f := newNormalizeFixture(t)
	appCtx := appContext.TestContext(nil)
	clock := &fakeClock{}
	appCtx.Clock = clock
	q := &recomputes{}
	return f, NewMetadataService(appCtx, repository.NewTransactor(f.db), q), clock, q
}

func TestMetadata_Namespace(t *testing.T) {
	f, svc, _, _ := newMetadataFixture(t)
	f.ingest(load(t, "acme-crm-prod-1", nil))

	ns, err := svc.UpdateNamespace(ctx, "acme", NamespaceInput{Name: ptr("  Acme Corp "), Description: ptr("The shop"),
		StaleAfter: ptr(2 * time.Hour), InactiveAfter: ptr(48 * time.Hour)})
	require.NoError(t, err)
	stored, err := f.repos.Namespace.FindBySlug(ctx, "acme")
	require.NoError(t, err)
	assert.Equal(t, ns.ID, stored.ID)
	assert.Equal(t, "Acme Corp", stored.Name)
	assert.Equal(t, "The shop", *stored.Description)
	assert.Equal(t, 2*time.Hour, stored.StaleAfter.Duration())
	assert.Equal(t, 48*time.Hour, stored.InactiveAfter.Duration())

	// Unset fields stay, empty ones are cleared.
	_, err = svc.UpdateNamespace(ctx, "acme", NamespaceInput{Description: ptr("")})
	require.NoError(t, err)
	stored, err = f.repos.Namespace.FindBySlug(ctx, "acme")
	require.NoError(t, err)
	assert.Equal(t, "Acme Corp", stored.Name)
	assert.Nil(t, stored.Description)

	for name, in := range map[string]NamespaceInput{
		"empty name":            {Name: ptr(" ")},
		"inactive before stale": {InactiveAfter: ptr(time.Hour)},
		"seconds":               {StaleAfter: ptr(time.Second)},
	} {
		_, err := svc.UpdateNamespace(ctx, "acme", in)
		assert.ErrorIs(t, err, ErrInvalid, name)
	}
	_, err = svc.UpdateNamespace(ctx, "nope", NamespaceInput{})
	assert.ErrorIs(t, err, ErrNamespaceNotFound)
}

func TestMetadata_Catalogs(t *testing.T) {
	f, svc, _, _ := newMetadataFixture(t)
	f.ingest(load(t, "acme-crm-prod-1", nil))
	f.ingest(load(t, "acme-crm-staging-1", nil))

	p, err := svc.UpdateProject(ctx, "acme", "crm", ProjectInput{Name: ptr("CRM"), Description: ptr("Customers")})
	require.NoError(t, err)
	assert.Equal(t, "CRM", p.Name)
	assert.Equal(t, "crm", p.Slug, "the slug comes from the labels")
	_, err = svc.UpdateProject(ctx, "acme", "nope", ProjectInput{})
	assert.ErrorIs(t, err, ErrNotFound)

	e, err := svc.UpdateEnvironment(ctx, "acme", "production", EnvironmentInput{Name: ptr("Production")})
	require.NoError(t, err)
	assert.Equal(t, "Production", e.Name)

	envs, err := svc.ReorderEnvironments(ctx, "acme", []string{"staging", "production"})
	require.NoError(t, err)
	assert.Equal(t, []string{"staging", "production"}, []string{envs[0].Slug, envs[1].Slug})
	assert.Equal(t, []int{0, 1}, []int{envs[0].SortOrder, envs[1].SortOrder})
	stored, err := f.repos.Environment.FindAll(ctx, envs[0].NamespaceID)
	require.NoError(t, err)
	assert.Equal(t, "staging", stored[0].Slug)
	for _, slugs := range [][]string{{"staging"}, {"staging", "production", "qa"}, {"staging", "staging"}} {
		_, err = svc.ReorderEnvironments(ctx, "acme", slugs)
		assert.ErrorIs(t, err, ErrInvalid, slugs)
	}

	n, err := svc.UpdateNetwork(ctx, "acme", "10-0-1-0-24", NetworkInput{Name: ptr("LAN prod"), Kind: ptr("public")})
	require.NoError(t, err)
	assert.Equal(t, "LAN prod", n.Name)
	assert.Equal(t, model.NetworkKindPublic, n.Kind)
	_, err = svc.UpdateNetwork(ctx, "acme", "10-0-1-0-24", NetworkInput{Kind: ptr("dmz")})
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestMetadata_DeleteHost(t *testing.T) {
	f, svc, clock, q := newMetadataFixture(t)
	host := f.ingest(load(t, "acme-crm-prod-1", nil))
	f.ingest(load(t, "acme-redis-prod-1", nil))
	ns, err := f.repos.Namespace.FindBySlug(ctx, "acme")
	require.NoError(t, err)

	clock.now = host.LastSeenAt.Add(time.Hour)
	assert.ErrorIs(t, svc.DeleteHost(ctx, "acme", host.ID), ErrHostActive, "seen within staleAfter, it would come back")
	assert.Empty(t, q.namespaces)

	clock.now = host.LastSeenAt.Add(ns.StaleAfter.Duration() + time.Minute)
	require.NoError(t, svc.DeleteHost(ctx, "acme", host.ID))
	_, err = f.repos.Host.FindByID(ctx, host.ID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	var apps int64
	require.NoError(t, f.db.Model(&model.App{}).Where("host_id = ?", host.ID).Count(&apps).Error)
	assert.Zero(t, apps, "its content goes with it")
	assert.Equal(t, []uuid.UUID{ns.ID}, q.namespaces, "the links are computed again")

	rows, err := f.repos.ChangeEvent.List(ctx, repository.ChangeEventFilter{NamespaceID: ns.ID, HostID: &host.ID, EntityType: model.ChangeEntityHost})
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	assert.Equal(t, model.ChangeRemoved, rows[0].Kind)
	assert.Contains(t, compact(t, rows[0].Before), `"hostname":"acme-crm-prod-1"`)

	assert.ErrorIs(t, svc.DeleteHost(ctx, "acme", host.ID), ErrNotFound)
	other := &model.Namespace{Slug: "other", Name: "Other"}
	require.NoError(t, f.repos.Namespace.Create(ctx, other))
	redis, err := f.repos.Host.FindByHostname(ctx, ns.ID, "acme-redis-prod-1")
	require.NoError(t, err)
	assert.ErrorIs(t, svc.DeleteHost(ctx, "other", redis.ID), ErrNotFound, "a host of another namespace")
}

// networksOf returns the network of each host of the namespace, by
// hostname: the one of its first IP in a network.
func networksOf(t *testing.T, f *normalizeFixture, namespaceID uuid.UUID) map[string]string {
	t.Helper()
	inv, err := f.repos.Inventory.Load(ctx, namespaceID)
	require.NoError(t, err)
	nets := linker.NewNetworks(inv)
	slugs, hostnames, out := map[uuid.UUID]string{}, map[uuid.UUID]string{}, map[string]string{}
	for _, n := range inv.Networks {
		slugs[n.ID] = n.Slug
	}
	for _, h := range inv.Hosts {
		hostnames[h.ID] = h.Hostname
	}
	for _, ip := range inv.IPs {
		if n, ok := nets.Of(ip.HostID, ip.IP.Addr); ok && out[hostnames[ip.HostID]] == "" && slugs[n] != "public" {
			out[hostnames[ip.HostID]] = slugs[n]
		}
	}
	return out
}

func TestMetadata_SplitNetwork(t *testing.T) {
	f, svc, _, q := newMetadataFixture(t)
	crm := f.ingest(load(t, "acme-crm-prod-1", nil))
	redis := f.ingest(load(t, "acme-redis-prod-1", nil))
	staging := f.ingest(load(t, "acme-crm-staging-1", nil))
	rp := f.ingest(load(t, "acme-rp-1", nil))
	lan := "10-0-1-0-24"

	split, err := svc.SplitNetwork(ctx, "acme", lan, "  Réseau prod B ", []uuid.UUID{redis.ID})
	require.NoError(t, err)
	assert.Equal(t, "reseau-prod-b", split.Slug)
	assert.Equal(t, "Réseau prod B", split.Name)
	assert.Equal(t, model.NetworkKindPrivate, split.Kind)
	root, err := f.repos.Network.FindBySlug(ctx, crm.NamespaceID, lan)
	require.NoError(t, err)
	assert.Equal(t, &root.ID, split.SplitFromID)
	assert.Equal(t, map[string]string{"acme-crm-prod-1": lan, "acme-redis-prod-1": "reseau-prod-b", "acme-crm-staging-1": "10-0-2-0-24", "acme-rp-1": lan},
		networksOf(t, f, crm.NamespaceID))
	assert.Len(t, q.namespaces, 1, "the links computed again")

	refused := []struct {
		name, slug, split string
		hosts             []uuid.UUID
		err               error
	}{
		{name: "same name", slug: lan, split: "Réseau prod B", hosts: []uuid.UUID{crm.ID}, err: ErrAlreadyExists},
		{name: "no host", slug: lan, split: "C", err: ErrInvalid},
		{name: "a host without IP in the range", slug: lan, split: "C", hosts: []uuid.UUID{staging.ID}, err: ErrInvalid},
		{name: "a host moved already", slug: lan, split: "C", hosts: []uuid.UUID{redis.ID}, err: ErrInvalid},
		{name: "unknown host", slug: lan, split: "C", hosts: []uuid.UUID{uuid.New()}, err: ErrNotFound},
		{name: "public network", slug: "public", split: "C", hosts: []uuid.UUID{rp.ID}, err: ErrInvalid},
		{name: "name without letter", slug: lan, split: " -- ", hosts: []uuid.UUID{crm.ID}, err: ErrInvalid},
		{name: "unknown network", slug: "nope", split: "C", hosts: []uuid.UUID{crm.ID}, err: ErrNotFound},
	}
	for _, tt := range refused {
		_, err := svc.SplitNetwork(ctx, "acme", tt.slug, tt.split, tt.hosts)
		assert.ErrorIs(t, err, tt.err, tt.name)
	}

	// Moved between the networks of the range, kept across snapshots.
	_, err = svc.MoveHostsToNetwork(ctx, "acme", "reseau-prod-b", []uuid.UUID{crm.ID})
	require.NoError(t, err)
	_, err = svc.MoveHostsToNetwork(ctx, "acme", "reseau-prod-b", []uuid.UUID{crm.ID})
	assert.ErrorIs(t, err, ErrInvalid, "already there")
	_, err = svc.MoveHostsToNetwork(ctx, "acme", lan, []uuid.UUID{redis.ID})
	require.NoError(t, err)
	f.ingest(load(t, "acme-crm-prod-1", nil))
	f.ingest(load(t, "acme-redis-prod-1", nil))
	assert.Equal(t, map[string]string{"acme-crm-prod-1": "reseau-prod-b", "acme-redis-prod-1": lan, "acme-crm-staging-1": "10-0-2-0-24", "acme-rp-1": lan},
		networksOf(t, f, crm.NamespaceID))

	// Split again from a split network: from the same range.
	again, err := svc.SplitNetwork(ctx, "acme", "reseau-prod-b", "Réseau prod C", []uuid.UUID{crm.ID})
	require.NoError(t, err)
	assert.Equal(t, &root.ID, again.SplitFromID)

	// Only a split network is deleted; its hosts go back.
	assert.ErrorIs(t, svc.DeleteNetwork(ctx, "acme", lan), ErrInvalid)
	require.NoError(t, svc.DeleteNetwork(ctx, "acme", "reseau-prod-c"))
	assert.Equal(t, lan, networksOf(t, f, crm.NamespaceID)["acme-crm-prod-1"])
	assert.ErrorIs(t, svc.DeleteNetwork(ctx, "acme", "reseau-prod-c"), ErrNotFound)
	assert.Len(t, q.namespaces, 5)
}

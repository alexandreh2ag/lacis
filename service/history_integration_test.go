//go:build integration

package service

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/common/snapshot"
	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// events describes the change events of the host, the oldest first:
// "<type> <kind> <before> → <after>".
func events(t *testing.T, f *normalizeFixture, host *model.Host) []string {
	rows, err := f.repos.ChangeEvent.List(ctx, repository.ChangeEventFilter{NamespaceID: host.NamespaceID, HostID: &host.ID})
	require.NoError(t, err)
	out := make([]string, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		r := rows[i]
		assert.Equal(t, host.Hostname, *r.Hostname)
		out = append(out, r.EntityType+" "+r.Kind+" "+compact(t, r.Before)+" → "+compact(t, r.After))
	}
	return out
}

// compact prints JSON with its keys sorted, as PostgreSQL does not keep
// them.
func compact(t *testing.T, raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	var v any
	require.NoError(t, json.Unmarshal(raw, &v))
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

func TestHistory_Normalize(t *testing.T) {
	f := newNormalizeFixture(t)
	host := f.ingest(load(t, "acme-crm-prod-1", nil))
	first := events(t, f, host)
	require.Len(t, first, 1, "the content of the first snapshot is told by the host")
	assert.Regexp(t, `^host added  → \{"arch":"x86_64","hostname":"acme-crm-prod-1","kernel":"6.8.12-4-pve","os":\{"id":"debian",`, first[0])

	// PHP is upgraded, the local Redis is gone, the API is deployed, the
	// kernel is updated.
	f.ingest(load(t, "acme-crm-prod-1", func(s *snapshot.Snapshot) {
		s.Host.Kernel = "6.1.0-27-amd64"
		s.Services = s.Services[:1]
		s.Services[0].Version = "8.2.30"
		for i := range s.Apps {
			if s.Apps[i].Name == "crm-api" {
				s.Apps[i].Code.Version = "v3.1.0"
			}
		}
		s.CollectedAt = s.CollectedAt.Add(time.Hour)
	}))
	got := events(t, f, host)[1:]
	assert.Equal(t, []string{
		`host changed {"hostname":"acme-crm-prod-1","kernel":"6.8.12-4-pve"} → {"hostname":"acme-crm-prod-1","kernel":"6.1.0-27-amd64"}`,
		`service changed {"instance":"8.2","type":"php-fpm","version":"8.2.29"} → {"instance":"8.2","type":"php-fpm","version":"8.2.30"}`,
		`service removed {"instance":"default","type":"redis","version":"7.0.15"} → `,
	}, got[:3])
	require.Len(t, got, 4)
	var before, after map[string]any
	require.Regexp(t, `^app changed `, got[3])
	rows, err := f.repos.ChangeEvent.List(ctx, repository.ChangeEventFilter{NamespaceID: host.NamespaceID, EntityType: model.ChangeEntityApp, Limit: 1})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(rows[0].Before, &before))
	require.NoError(t, json.Unmarshal(rows[0].After, &after))
	assert.Equal(t, []any{"crm-api", "v5.1.3"}, []any{before["name"], before["code"].(map[string]any)["version"]})
	assert.Equal(t, []any{"crm-api", "v3.1.0"}, []any{after["name"], after["code"].(map[string]any)["version"]})
	assert.NotContains(t, after, "role", "only the fields changed, and the identity")

	// Normalizing again the same snapshot changes nothing.
	_, err = f.normalize.NormalizeAll(ctx)
	require.NoError(t, err)
	assert.Len(t, events(t, f, host), 5)

	// The Redis comes back.
	f.ingest(load(t, "acme-crm-prod-1", func(s *snapshot.Snapshot) { s.CollectedAt = s.CollectedAt.Add(2 * time.Hour) }))
	assert.Contains(t, events(t, f, host), `service added  → {"instance":"default","type":"redis","version":"7.0.15"}`)
}

// The links of an app change when its database enrolls: the external
// targets are removed, the database added. The first computation records
// nothing.
func TestHistory_Links(t *testing.T) {
	f := newNormalizeFixture(t)
	app := f.ingest(load(t, "acme-crm-prod-1", nil))
	svc := newLinkService(t, f)
	_, err := svc.Recompute(ctx, app.NamespaceID)
	require.NoError(t, err)
	links := func() []string {
		rows, err := f.repos.ChangeEvent.List(ctx, repository.ChangeEventFilter{NamespaceID: app.NamespaceID, EntityType: model.ChangeEntityLink})
		require.NoError(t, err)
		var out []string
		for _, r := range rows {
			assert.Equal(t, app.ID, *r.HostID, "the host of the app the link starts from")
			var s map[string]any
			if r.Kind == model.ChangeAdded {
				require.NoError(t, json.Unmarshal(r.After, &s))
			} else {
				require.NoError(t, json.Unmarshal(r.Before, &s))
			}
			if s["key"] == "MONGODB_URL" {
				out = append(out, r.Kind+" "+s["to"].(string)+" "+s["reason"].(string))
			}
		}
		return out
	}
	assert.Empty(t, links())

	for _, name := range []string{"acme-mongodb-prod-1", "acme-mongodb-prod-2", "acme-mongodb-prod-3"} {
		f.ingest(load(t, name, nil))
	}
	_, err = svc.Recompute(ctx, app.NamespaceID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"removed 10.0.1.70 unknown_private_ip", "removed 10.0.1.71 unknown_private_ip", "removed 10.0.1.72 unknown_private_ip",
		"added acme-mongodb-prod-1/mongodb ", "added acme-mongodb-prod-2/mongodb ", "added acme-mongodb-prod-3/mongodb ",
	}, links())

	// Computing again the same links records nothing.
	_, err = svc.Recompute(ctx, app.NamespaceID)
	require.NoError(t, err)
	assert.Len(t, links(), 6)
}

func TestHistoryService_List(t *testing.T) {
	f := newNormalizeFixture(t)
	crm := f.ingest(load(t, "acme-crm-prod-1", nil))
	f.ingest(load(t, "acme-redis-prod-1", nil))
	f.ingest(load(t, "acme-crm-prod-1", func(s *snapshot.Snapshot) {
		s.Services = s.Services[:1]
		s.CollectedAt = s.CollectedAt.Add(time.Hour)
	}))
	svc := NewHistoryService(appContext.TestContext(nil), repository.NewTransactor(f.db))

	all, err := svc.List(ctx, "acme", HistoryFilter{})
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, "service removed", all[0].EntityType+" "+all[0].Kind, "the latest first")
	assert.Equal(t, "acme-redis-prod-1", *all[1].Hostname)

	oldest, err := svc.List(ctx, "acme", HistoryFilter{OldestFirst: true, Offset: 1, Limit: 2})
	require.NoError(t, err)
	require.Len(t, oldest, 2)
	assert.Equal(t, []int64{all[1].ID, all[0].ID}, []int64{oldest[0].ID, oldest[1].ID}, "the oldest first, the first skipped")

	byHost, err := svc.List(ctx, "acme", HistoryFilter{Hostname: "acme-crm-prod-1", EntityType: model.ChangeEntityHost})
	require.NoError(t, err)
	require.Len(t, byHost, 1)
	assert.Equal(t, crm.ID, *byHost[0].EntityID)

	limited, err := svc.List(ctx, "acme", HistoryFilter{Limit: 1, Since: time.Now().Add(-time.Hour)})
	require.NoError(t, err)
	assert.Len(t, limited, 1)
	none, err := svc.List(ctx, "acme", HistoryFilter{Since: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	assert.Empty(t, none)

	_, err = svc.List(ctx, "nope", HistoryFilter{})
	assert.ErrorIs(t, err, ErrNamespaceNotFound)
	_, err = svc.List(ctx, "acme", HistoryFilter{Hostname: "nope"})
	assert.ErrorIs(t, err, ErrHostNotFound)
}

// The history of a namespace keeps its latest events.
func TestHistory_Purge(t *testing.T) {
	f := newNormalizeFixture(t)
	appCtx := appContext.TestContext(nil)
	appCtx.Config.History.MaxEvents = 3
	f.normalize = NewNormalizeService(appCtx, repository.NewTransactor(f.db))
	host := f.ingest(load(t, "acme-crm-prod-1", nil))
	for i := 1; i <= 4; i++ {
		f.ingest(load(t, "acme-crm-prod-1", func(s *snapshot.Snapshot) {
			s.Services[0].Version = "8.2." + strconv.Itoa(30+i)
			s.CollectedAt = s.CollectedAt.Add(time.Duration(i) * time.Hour)
		}))
	}
	got := events(t, f, host)
	require.Len(t, got, 3)
	assert.Contains(t, got[2], `"version":"8.2.34"`)

	// 0 keeps all.
	appCtx.Config.History.MaxEvents = 0
	for i := 5; i <= 8; i++ {
		f.ingest(load(t, "acme-crm-prod-1", func(s *snapshot.Snapshot) {
			s.Services[0].Version = "8.2." + strconv.Itoa(30+i)
			s.CollectedAt = s.CollectedAt.Add(time.Duration(i) * time.Hour)
		}))
	}
	assert.Len(t, events(t, f, host), 7)
}

func TestNamespaceService(t *testing.T) {
	f := newNormalizeFixture(t)
	host := f.ingest(load(t, "acme-crm-prod-1", nil))
	svc := NewNamespaceService(appContext.TestContext(nil), repository.NewTransactor(f.db))

	all, err := svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
	ns, err := svc.BySlug(ctx, "acme")
	require.NoError(t, err)
	assert.Equal(t, host.NamespaceID, ns.ID)
	_, err = svc.BySlug(ctx, "nope")
	assert.ErrorIs(t, err, ErrNamespaceNotFound)

	inv, err := svc.Inventory(ctx, ns.ID)
	require.NoError(t, err)
	assert.Len(t, inv.Hosts, 1)
	assert.NotEmpty(t, inv.Runtimes)
}

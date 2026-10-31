//go:build integration

package service

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/linker"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newLinkService(t *testing.T, f *normalizeFixture) LinkService {
	appCtx := appContext.TestContext(nil)
	appCtx.Config.Linking.CDNRanges = []string{"104.16.0.0/13"}
	svc, err := NewLinkService(appCtx, repository.NewTransactor(f.db))
	require.NoError(t, err)
	return svc
}

// linkTo describes the links of the app named on the host: key → target,
// the target a host and type, or "external <target> (<reason>)".
func linkTo(t *testing.T, f *normalizeFixture, namespaceID uuid.UUID, hostname, app string) map[string][]string {
	inv, err := f.repos.Inventory.Load(ctx, namespaceID)
	require.NoError(t, err)
	hostnames := map[uuid.UUID]string{}
	for _, h := range inv.Hosts {
		hostnames[h.ID] = h.Hostname
	}
	names := map[uuid.UUID]string{}
	var appID uuid.UUID
	for _, a := range inv.Apps {
		names[a.ID] = hostnames[a.HostID] + "/" + a.Name
		if hostnames[a.HostID] == hostname && a.Name == app {
			appID = a.ID
		}
	}
	for _, s := range inv.Services {
		names[s.ID] = hostnames[s.HostID] + "/" + s.Type
	}
	for _, w := range inv.Webservers {
		names[w.ID] = hostnames[w.HostID] + "/" + w.Type
	}
	require.NotEqual(t, uuid.Nil, appID, "app %s on %s", app, hostname)
	out := map[string][]string{}
	for _, l := range inv.Links {
		if l.FromID != appID {
			continue
		}
		var d linker.Detail
		require.NoError(t, json.Unmarshal(l.Detail, &d))
		var to string
		if l.ToID != nil {
			to = names[*l.ToID]
		} else {
			to = "external " + *l.ExternalTarget + " (" + d.Reason + ")"
		}
		out[d.Key] = append(out[d.Key], to)
	}
	return out
}

func TestRecompute_Fixtures(t *testing.T) {
	f := newNormalizeFixture(t)
	files, err := filepath.Glob("../testdata/snapshots/*.json")
	require.NoError(t, err)
	var host *model.Host
	for _, file := range files {
		host = f.ingest(load(t, strings.TrimSuffix(filepath.Base(file), ".json"), nil))
	}
	svc := newLinkService(t, f)
	stored, err := svc.Recompute(ctx, host.NamespaceID)
	require.NoError(t, err)
	assert.Equal(t, 86, stored)

	assert.Equal(t, map[string][]string{
		"API_URL":               {"acme-rp-1/traefik"},
		"NEXT_PUBLIC_MEDIA_URL": {"external media.acme.example (cdn)"},
	}, linkTo(t, f, host.NamespaceID, "acme-shop-prod-1", "shop-front"))
	assert.Equal(t, map[string][]string{
		"DATABASE_URL":            {"acme-postgresql-staging-1/postgresql"},
		"ELASTICSEARCH_URL":       {"acme-elasticsearch-staging-1/elasticsearch"},
		"LEGACY_DSN":              {"external acme-crm-staging-1:LEGACY_DSN (unparsed)"},
		"MESSENGER_TRANSPORT_DSN": {"acme-rabbitmq-staging-1/rabbitmq"},
		"MONGODB_URL":             {"acme-mongodb-staging-1/mongodb"},
		"PARTNER_API_URL":         {"external api.partner.example (dns_failed)"},
		"REDIS_CACHE_URL":         {"acme-crm-staging-1/redis"},
		"REDIS_URL":               {"acme-redis-staging-1/redis"},
		"S3_ENDPOINT_URL":         {"external s3.eu-west-3.amazonaws.com (public_ip)"},
		"SENTRY_DSN":              {"external o1.ingest.sentry.io (public_ip)"},
	}, linkTo(t, f, host.NamespaceID, "acme-crm-staging-1", "crm-api"))

	// Recomputing replaces the links.
	stored, err = svc.Recompute(ctx, host.NamespaceID)
	require.NoError(t, err)
	inv, err := f.repos.Inventory.Load(ctx, host.NamespaceID)
	require.NoError(t, err)
	assert.Len(t, inv.Links, stored)

	total, err := svc.RecomputeAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, 86, total)
}

// An app enrolled before its database gets its link once the database
// comes: the namespace is recomputed whole.
func TestRecompute_LateEnrollment(t *testing.T) {
	f := newNormalizeFixture(t)
	app := f.ingest(load(t, "acme-crm-prod-1", nil))
	svc := newLinkService(t, f)
	_, err := svc.Recompute(ctx, app.NamespaceID)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"external 10.0.1.70 (unknown_private_ip)", "external 10.0.1.71 (unknown_private_ip)", "external 10.0.1.72 (unknown_private_ip)",
	}, linkTo(t, f, app.NamespaceID, "acme-crm-prod-1", "crm-api")["MONGODB_URL"])

	for _, name := range []string{"acme-mongodb-prod-1", "acme-mongodb-prod-2", "acme-mongodb-prod-3"} {
		f.ingest(load(t, name, nil))
	}
	_, err = svc.Recompute(ctx, app.NamespaceID)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"acme-mongodb-prod-1/mongodb", "acme-mongodb-prod-2/mongodb", "acme-mongodb-prod-3/mongodb",
	}, linkTo(t, f, app.NamespaceID, "acme-crm-prod-1", "crm-api")["MONGODB_URL"])
}

func TestRecompute_UnknownNamespace(t *testing.T) {
	f := newNormalizeFixture(t)
	_, err := newLinkService(t, f).Recompute(ctx, uuid.New())
	assert.ErrorContains(t, err, "record not found")
}

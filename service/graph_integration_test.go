//go:build integration

package service

import (
	"testing"

	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/alexandreh2ag/lacis/topology"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraph(t *testing.T) {
	f := newNormalizeFixture(t)
	host := f.ingest(load(t, "acme-crm-prod-1", nil))
	f.ingest(load(t, "acme-redis-prod-1", nil))
	_, err := newLinkService(t, f).Recompute(ctx, host.NamespaceID)
	require.NoError(t, err)

	svc := NewGraphService(appContext.TestContext(nil), repository.NewTransactor(f.db))
	g, err := svc.Graph(ctx, "acme", topology.Filter{Level: topology.LevelHost})
	require.NoError(t, err)
	var hosts []string
	for _, n := range g.Nodes {
		if n.Kind == "host" {
			hosts = append(hosts, n.Label)
		}
	}
	assert.Equal(t, []string{"acme-crm-prod-1", "acme-redis-prod-1"}, hosts)

	_, err = svc.Graph(ctx, "nope", topology.Filter{})
	assert.ErrorIs(t, err, ErrNamespaceNotFound)
}

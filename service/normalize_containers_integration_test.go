//go:build integration

package service

import (
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalize_Containers(t *testing.T) {
	f := newNormalizeFixture(t)
	host := f.ingest(load(t, "acme-crm-staging-1", nil))
	containers, err := f.repos.Container.FindByHost(ctx, host.ID)
	require.NoError(t, err)
	require.Len(t, containers, 3)
	byName := map[string]model.Container{}
	for _, c := range containers {
		byName[c.Name] = c
	}
	cache := byName["crm-cache"]
	assert.Equal(t, "5e8a1c7d2b90", cache.ContainerID)
	assert.Equal(t, "redis:7.4-alpine", cache.Image)
	assert.Equal(t, "7.4", *cache.Version)
	assert.Equal(t, "running", cache.State)
	assert.Equal(t, []string{"127.0.0.1:6380->6379/tcp"}, cache.Ports)
	assert.Equal(t, []string{"crm_default"}, cache.Networks)
	assert.Equal(t, "crm", *cache.ComposeProject)
	assert.Equal(t, "cache", *cache.ComposeService)
	assert.Equal(t, []string{}, byName["crm-migrate"].Ports, "none: an empty list")

	// The migration container is gone, the cache created again and stopped,
	// a duplicate name keeps the first.
	f.ingest(load(t, "acme-crm-staging-1", func(s *snapshot.Snapshot) {
		s.Containers = s.Containers[:2]
		s.Containers[0].ID, s.Containers[0].State = "77aa00bb11cc", "exited"
		s.Containers = append(s.Containers, snapshot.DockerContainer{ID: "dup", Name: "crm-mailpit", Image: "x", State: "running"})
		s.CollectedAt = s.CollectedAt.Add(time.Hour)
	}))
	containers, err = f.repos.Container.FindByHost(ctx, host.ID)
	require.NoError(t, err)
	require.Len(t, containers, 3, "kept, with removed_at")
	for _, c := range containers {
		switch c.Name {
		case "crm-cache":
			assert.Equal(t, cache.ID, c.ID, "same row: found by name")
			assert.Equal(t, "77aa00bb11cc", c.ContainerID)
			assert.Equal(t, "exited", c.State)
			assert.Nil(t, c.RemovedAt)
		case "crm-mailpit":
			assert.Equal(t, "axllent/mailpit:v1.20", c.Image)
		case "crm-migrate":
			assert.NotNil(t, c.RemovedAt)
		}
	}

	inv, err := f.repos.Inventory.Load(ctx, host.NamespaceID)
	require.NoError(t, err)
	assert.Len(t, inv.Containers, 2, "the removed one left out")
}

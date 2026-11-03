//go:build integration

package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTechIconRepository(t *testing.T) {
	f := newFixture(t)
	repo := f.repos.TechIcon
	admin := &model.User{Username: "admin"}
	require.NoError(t, f.repos.User.Create(ctx, admin))
	at := time.Now().UTC().Truncate(time.Microsecond)

	require.NoError(t, repo.Save(ctx, &model.TechIcon{Tech: "traefik", ContentType: "image/png", Content: []byte("png"), UpdatedBy: &admin.ID, UpdatedAt: at}))
	require.NoError(t, repo.Save(ctx, &model.TechIcon{Tech: "traefik", ContentType: "image/svg+xml", Content: []byte("<svg/>"), UpdatedAt: at.Add(time.Hour)}))
	require.NoError(t, repo.Save(ctx, &model.TechIcon{Tech: "redis", ContentType: "image/png", Content: []byte("png"), UpdatedBy: &admin.ID, UpdatedAt: at}))

	got, err := repo.Find(ctx, "traefik")
	require.NoError(t, err)
	assert.Equal(t, "image/svg+xml", got.ContentType, "a second upload replaces the first")
	assert.Equal(t, []byte("<svg/>"), got.Content)
	assert.Nil(t, got.UpdatedBy)
	assert.True(t, at.Add(time.Hour).Equal(got.UpdatedAt))

	all, err := repo.FindAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, []string{"redis", "traefik"}, []string{all[0].Tech, all[1].Tech})
	assert.Empty(t, all[0].Content, "listed without content")

	require.NoError(t, f.repos.User.Delete(ctx, admin.ID))
	got, err = repo.Find(ctx, "redis")
	require.NoError(t, err)
	assert.Nil(t, got.UpdatedBy, "the logo stays when its author goes")

	require.NoError(t, repo.Delete(ctx, "traefik"))
	require.NoError(t, repo.Delete(ctx, "traefik"), "deleting none is a no-op")
	_, err = repo.Find(ctx, "traefik")
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	now := time.Now()
	require.NoError(t, f.repos.Service.Save(ctx, &model.Service{NamespaceID: f.namespace.ID, HostID: f.host.ID, Type: "Redis", Instance: "default", Details: json.RawMessage(`{}`), FirstSeenAt: now}))
	require.NoError(t, f.repos.Service.Save(ctx, &model.Service{NamespaceID: f.namespace.ID, HostID: f.host.ID, Type: "mysql", Instance: "old", Details: json.RawMessage(`{}`), FirstSeenAt: now, RemovedAt: &now}))
	require.NoError(t, f.repos.Webserver.Save(ctx, &model.Webserver{NamespaceID: f.namespace.ID, HostID: f.host.ID, Type: "traefik", Routing: json.RawMessage(`{}`), FirstSeenAt: now}))
	techs, err := repo.FindTechnologies(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"redis", "traefik"}, techs, "lowercased, removed ones left out")
}

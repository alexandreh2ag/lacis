//go:build integration

package repository

import (
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUserRepository_Admin(t *testing.T) {
	f := newFixture(t)
	repo := f.repos.User
	bob := &model.User{Username: "bob"}
	alice := &model.User{Username: "alice"}
	require.NoError(t, repo.Create(ctx, bob))
	require.NoError(t, repo.Create(ctx, alice))
	admin, err := f.repos.Role.FindByCode(ctx, model.RoleAdmin)
	require.NoError(t, err)
	require.NoError(t, f.repos.Role.AssignToUser(ctx, bob.ID, admin.ID))
	now := time.Now().UTC()
	for _, s := range []model.Session{{ID: "bob-1", UserID: bob.ID}, {ID: "bob-2", UserID: bob.ID}, {ID: "alice-1", UserID: alice.ID}} {
		s.CreatedAt, s.LastSeenAt, s.ExpiresAt = now, now, now.Add(time.Hour)
		require.NoError(t, f.repos.Session.Create(ctx, &s))
	}

	users, err := repo.FindAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"alice", "bob"}, []string{users[0].Username, users[1].Username})

	require.NoError(t, f.repos.Session.DeleteByUser(ctx, bob.ID, "bob-2"))
	_, err = f.repos.Session.FindValid(ctx, "bob-1", now)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, err = f.repos.Session.FindValid(ctx, "bob-2", now)
	assert.NoError(t, err, "the kept session stays")
	_, err = f.repos.Session.FindValid(ctx, "alice-1", now)
	assert.NoError(t, err, "the sessions of the others stay")

	require.NoError(t, repo.Delete(ctx, bob.ID))
	_, err = repo.FindByID(ctx, bob.ID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, err = f.repos.Session.FindValid(ctx, "bob-2", now)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound, "its sessions go with it")
	n, err := f.repos.Role.CountUsers(ctx, admin.ID)
	require.NoError(t, err)
	assert.Zero(t, n, "and its roles")
}

func TestRoleRepository_Admin(t *testing.T) {
	f := newFixture(t)
	repo := f.repos.Role
	admin, err := repo.FindByCode(ctx, model.RoleAdmin)
	require.NoError(t, err)
	viewer := &model.Role{Code: "acme-viewer"}
	require.NoError(t, repo.Create(ctx, viewer))
	bob := &model.User{Username: "bob"}
	require.NoError(t, f.repos.User.Create(ctx, bob))

	got, err := repo.FindByID(ctx, viewer.ID)
	require.NoError(t, err)
	assert.Equal(t, "acme-viewer", got.Code)

	description := "Reads acme"
	viewer.Code, viewer.Description = "acme-readers", &description
	require.NoError(t, repo.Update(ctx, viewer))
	got, err = repo.FindByID(ctx, viewer.ID)
	require.NoError(t, err)
	assert.Equal(t, "acme-readers", got.Code)
	assert.Equal(t, description, *got.Description)
	assert.ErrorIs(t, repo.Update(ctx, &model.Role{Base: model.Base{ID: viewer.ID}, Code: model.RoleAdmin}), gorm.ErrDuplicatedKey)

	require.NoError(t, repo.SetUserRoles(ctx, bob.ID, []uuid.UUID{admin.ID, viewer.ID}))
	require.NoError(t, repo.SetUserRoles(ctx, bob.ID, []uuid.UUID{viewer.ID}))
	held, err := repo.FindUserRoles(ctx)
	require.NoError(t, err)
	assert.Equal(t, []model.UserRole{{UserID: bob.ID, RoleID: viewer.ID}}, held, "the roles are replaced")
	n, err := repo.CountUsers(ctx, viewer.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	require.NoError(t, repo.SetPermissions(ctx, viewer.ID,
		[]model.NamespacePermission{{NamespaceID: &f.namespace.ID, Resource: model.ResourceInventory, Action: model.ActionRead}, {Resource: model.ResourceMetadata, Action: model.ActionRead}},
		[]model.GlobalPermission{{Resource: model.ResourceTokens, Action: model.ActionRead}}))
	require.NoError(t, repo.SetPermissions(ctx, viewer.ID,
		[]model.NamespacePermission{{NamespaceID: &f.namespace.ID, Resource: model.ResourceInventory, Action: model.ActionRead}}, nil))
	require.NoError(t, repo.SetOIDCGroups(ctx, viewer.ID, []string{"devs", "ops"}))
	require.NoError(t, repo.SetOIDCGroups(ctx, viewer.ID, []string{"ops"}))

	ns, global, groups, err := repo.FindGrants(ctx)
	require.NoError(t, err)
	var mine []model.NamespacePermission
	for _, p := range ns {
		if p.RoleID == viewer.ID {
			mine = append(mine, p)
		}
	}
	require.Len(t, mine, 1, "the permissions are replaced")
	assert.Equal(t, f.namespace.ID, *mine[0].NamespaceID)
	for _, p := range global {
		assert.Equal(t, admin.ID, p.RoleID, "only admin holds a global permission")
	}
	assert.Equal(t, []model.RoleOIDCGroup{{RoleID: viewer.ID, GroupName: "ops"}}, groups)

	require.NoError(t, repo.Delete(ctx, viewer.ID))
	_, err = repo.FindByID(ctx, viewer.ID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	held, err = repo.FindUserRoles(ctx)
	require.NoError(t, err)
	assert.Empty(t, held, "its holders lose it")
	_, _, groups, err = repo.FindGrants(ctx)
	require.NoError(t, err)
	assert.Empty(t, groups)
}

func TestAgentRepository_Admin(t *testing.T) {
	f := newFixture(t)
	label := "web"
	token := &model.EnrollmentToken{TokenHash: "hash-web", Label: &label}
	require.NoError(t, f.repos.EnrollmentToken.Create(ctx, token))
	waiting := &model.Agent{TokenHash: "agent-waiting", EnrollmentTokenID: token.ID}
	require.NoError(t, f.repos.Agent.Create(ctx, waiting))

	got, err := f.repos.EnrollmentToken.FindByID(ctx, token.ID)
	require.NoError(t, err)
	assert.False(t, got.CreatedAt.IsZero())

	rows, err := f.repos.Agent.FindAll(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, waiting.ID, rows[0].ID, "the last enrolled first")
	assert.Nil(t, rows[0].HostID, "no host before the first snapshot")
	assert.Equal(t, label, *rows[0].TokenLabel)
	assert.Equal(t, f.agent.ID, rows[1].ID)
	assert.Equal(t, "agent-hash", rows[1].TokenHash)
	assert.Equal(t, f.host.ID, *rows[1].HostID)
	assert.Equal(t, "acme-site-prod-1", *rows[1].Hostname)
	assert.Equal(t, "acme", *rows[1].Namespace)
	assert.Nil(t, rows[1].TokenLabel)

	counts, err := f.repos.Agent.CountByEnrollmentToken(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[uuid.UUID]int{f.token.ID: 1, token.ID: 1}, counts)
}

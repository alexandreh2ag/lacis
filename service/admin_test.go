package service

import (
	"errors"
	"testing"

	"github.com/alexandreh2ag/lacis/auth"
	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/hash"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gorm.io/gorm"
)

var (
	adminRole  = model.Role{Base: model.Base{ID: uuid.MustParse("019a2400-0000-7000-8000-000000000001")}, Code: model.RoleAdmin}
	viewerRole = model.Role{Base: model.Base{ID: uuid.New()}, Code: "acme-viewer"}
)

func TestUserService_List(t *testing.T) {
	m := newMocks(t)
	alice := model.User{Base: model.Base{ID: uuid.New()}, Username: "alice"}
	bob := model.User{Base: model.Base{ID: uuid.New()}, Username: "bob"}
	m.user.EXPECT().FindAll(ctx).Return([]model.User{alice, bob}, nil)
	m.role.EXPECT().FindAll(ctx).Return([]model.Role{viewerRole, adminRole}, nil)
	m.role.EXPECT().FindUserRoles(ctx).Return([]model.UserRole{{UserID: alice.ID, RoleID: viewerRole.ID}, {UserID: alice.ID, RoleID: adminRole.ID}}, nil)

	got, err := NewUserService(appContext.TestContext(nil), m.tx).List(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "alice", got[0].Username)
	assert.Equal(t, []model.Role{viewerRole, adminRole}, got[0].Roles, "by code")
	assert.Empty(t, got[1].Roles)
}

func TestUserService_Update(t *testing.T) {
	id := uuid.New()
	local := func() *model.User {
		return &model.User{Base: model.Base{ID: id}, Username: "bob", FirstName: ptr("Bob"), PasswordHash: ptr("old")}
	}
	tests := []struct {
		name    string
		actor   *auth.Principal
		in      UpdateUserInput
		user    *model.User
		setup   func(m *mocks)
		check   func(t *testing.T, saved *model.User)
		wantErr error
	}{
		{name: "names", in: UpdateUserInput{FirstName: ptr(" "), LastName: ptr(" Martin ")}, user: local(),
			check: func(t *testing.T, saved *model.User) {
				assert.Nil(t, saved.FirstName, "set empty, a name is cleared")
				assert.Equal(t, "Martin", *saved.LastName)
				assert.Equal(t, "old", *saved.PasswordHash)
			}},
		{name: "password of another user ends all its sessions", in: UpdateUserInput{Password: ptr("new password")}, user: local(),
			actor: &auth.Principal{User: model.User{Base: model.Base{ID: uuid.New()}}, SessionID: "admin-session"},
			setup: func(m *mocks) { m.session.EXPECT().DeleteByUser(ctx, id, "").Return(nil) },
			check: func(t *testing.T, saved *model.User) {
				ok, err := hash.VerifyPassword("new password", *saved.PasswordHash)
				require.NoError(t, err)
				assert.True(t, ok)
			}},
		{name: "own password keeps the session", in: UpdateUserInput{Password: ptr("new password")}, user: local(),
			actor: &auth.Principal{User: model.User{Base: model.Base{ID: id}}, SessionID: "mine"},
			setup: func(m *mocks) { m.session.EXPECT().DeleteByUser(ctx, id, "mine").Return(nil) }},
		{name: "short password", in: UpdateUserInput{Password: ptr("short")}, user: local(), wantErr: ErrInvalid},
		{name: "SSO user has no password", in: UpdateUserInput{Password: ptr("new password")},
			user: &model.User{Base: model.Base{ID: id}, Username: "sso", OIDCSubject: ptr("sub")}, wantErr: ErrInvalid},
		{name: "long name", in: UpdateUserInput{LastName: ptr(string(make([]byte, 101)) + "x")}, user: local(), wantErr: ErrInvalid},
		{name: "unknown", wantErr: ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMocks(t)
			if tt.user == nil {
				m.user.EXPECT().FindByID(ctx, id).Return(nil, gorm.ErrRecordNotFound)
			} else {
				m.user.EXPECT().FindByID(ctx, id).Return(tt.user, nil).AnyTimes()
			}
			if tt.setup != nil {
				tt.setup(m)
			}
			var saved *model.User
			if tt.wantErr == nil {
				m.user.EXPECT().Update(ctx, gomock.Any()).DoAndReturn(func(_ any, u *model.User) error {
					saved = u
					return nil
				})
				m.role.EXPECT().FindByUser(ctx, id).Return([]model.Role{viewerRole}, nil)
			}
			got, err := NewUserService(appContext.TestContext(nil), m.tx).Update(ctx, tt.actor, id, tt.in)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []model.Role{viewerRole}, got.Roles)
			if tt.check != nil {
				tt.check(t, saved)
			}
		})
	}
}

func TestUserService_SetRoles(t *testing.T) {
	id := uuid.New()
	user := &model.User{Base: model.Base{ID: id}, Username: "bob"}
	errDB := errors.New("db down")
	tests := []struct {
		name    string
		roles   []uuid.UUID
		setup   func(m *mocks)
		wantErr error
	}{
		{name: "gives the roles once", roles: []uuid.UUID{adminRole.ID, viewerRole.ID, adminRole.ID},
			setup: func(m *mocks) {
				m.role.EXPECT().FindByID(ctx, adminRole.ID).Return(&adminRole, nil)
				m.role.EXPECT().FindByID(ctx, viewerRole.ID).Return(&viewerRole, nil)
				m.role.EXPECT().SetUserRoles(ctx, id, []uuid.UUID{adminRole.ID, viewerRole.ID}).Return(nil)
			}},
		{name: "takes admin from one of two admins", roles: []uuid.UUID{viewerRole.ID},
			setup: func(m *mocks) {
				m.role.EXPECT().FindByID(ctx, viewerRole.ID).Return(&viewerRole, nil)
				m.role.EXPECT().FindByUser(ctx, id).Return([]model.Role{adminRole}, nil)
				m.role.EXPECT().CountUsers(ctx, adminRole.ID).Return(int64(2), nil)
				m.role.EXPECT().SetUserRoles(ctx, id, []uuid.UUID{viewerRole.ID}).Return(nil)
			}},
		{name: "a user without admin loses every role", roles: nil,
			setup: func(m *mocks) {
				m.role.EXPECT().FindByUser(ctx, id).Return([]model.Role{viewerRole}, nil)
				m.role.EXPECT().SetUserRoles(ctx, id, []uuid.UUID{}).Return(nil)
			}},
		{name: "the last admin keeps admin", roles: nil, wantErr: ErrLastAdmin,
			setup: func(m *mocks) {
				m.role.EXPECT().FindByUser(ctx, id).Return([]model.Role{adminRole}, nil)
				m.role.EXPECT().CountUsers(ctx, adminRole.ID).Return(int64(1), nil)
			}},
		{name: "unknown role", roles: []uuid.UUID{viewerRole.ID}, wantErr: ErrNotFound,
			setup: func(m *mocks) { m.role.EXPECT().FindByID(ctx, viewerRole.ID).Return(nil, gorm.ErrRecordNotFound) }},
		{name: "db error", roles: nil, wantErr: errDB,
			setup: func(m *mocks) { m.role.EXPECT().FindByUser(ctx, id).Return(nil, errDB) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMocks(t)
			m.user.EXPECT().FindByID(ctx, id).Return(user, nil).AnyTimes()
			tt.setup(m)
			if tt.wantErr == nil {
				m.role.EXPECT().FindByUser(ctx, id).Return(nil, nil)
			}
			_, err := NewUserService(appContext.TestContext(nil), m.tx).SetRoles(ctx, id, tt.roles)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestUserService_Delete(t *testing.T) {
	id := uuid.New()
	other := &auth.Principal{User: model.User{Base: model.Base{ID: uuid.New()}}}

	m := newMocks(t)
	svc := NewUserService(appContext.TestContext(nil), m.tx)
	assert.ErrorIs(t, svc.Delete(ctx, &auth.Principal{User: model.User{Base: model.Base{ID: id}}}, id), ErrInvalid, "nobody deletes themselves")

	m.user.EXPECT().FindByID(ctx, id).Return(nil, gorm.ErrRecordNotFound)
	assert.ErrorIs(t, svc.Delete(ctx, other, id), ErrNotFound)

	m.user.EXPECT().FindByID(ctx, id).Return(&model.User{Base: model.Base{ID: id}}, nil).Times(2)
	m.role.EXPECT().FindByUser(ctx, id).Return([]model.Role{adminRole}, nil).Times(2)
	m.role.EXPECT().CountUsers(ctx, adminRole.ID).Return(int64(1), nil)
	assert.ErrorIs(t, svc.Delete(ctx, other, id), ErrLastAdmin)

	m.role.EXPECT().CountUsers(ctx, adminRole.ID).Return(int64(3), nil)
	m.user.EXPECT().Delete(ctx, id).Return(nil)
	assert.NoError(t, svc.Delete(ctx, other, id))
}

func TestUserService_Create_RoleIDs(t *testing.T) {
	m := newMocks(t)
	m.role.EXPECT().FindByID(ctx, viewerRole.ID).Return(&viewerRole, nil)
	m.user.EXPECT().Create(ctx, gomock.Any()).Return(nil)
	m.role.EXPECT().AssignToUser(ctx, gomock.Any(), viewerRole.ID).Return(nil)
	_, err := NewUserService(appContext.TestContext(nil), m.tx).Create(ctx, CreateUserInput{Username: "bob", Password: "12345678", RoleIDs: []uuid.UUID{viewerRole.ID}})
	require.NoError(t, err)

	m.role.EXPECT().FindByID(ctx, adminRole.ID).Return(nil, gorm.ErrRecordNotFound)
	_, err = NewUserService(appContext.TestContext(nil), m.tx).Create(ctx, CreateUserInput{Username: "bob", Password: "12345678", RoleIDs: []uuid.UUID{adminRole.ID}})
	assert.ErrorIs(t, err, ErrNotFound)
}

// grants expects the reads of listRoles: viewer reads acme, alice holds it.
func grants(m *mocks, acme model.Namespace, roles ...model.Role) {
	m.role.EXPECT().FindAll(ctx).Return(roles, nil)
	m.role.EXPECT().FindGrants(ctx).Return(
		[]model.NamespacePermission{{RoleID: adminRole.ID, Resource: model.ResourceAll, Action: model.ActionAll},
			{RoleID: viewerRole.ID, NamespaceID: &acme.ID, Resource: model.ResourceInventory, Action: model.ActionRead}},
		[]model.GlobalPermission{{RoleID: adminRole.ID, Resource: model.ResourceAll, Action: model.ActionAll}},
		[]model.RoleOIDCGroup{{RoleID: viewerRole.ID, GroupName: "devs"}}, nil)
	m.role.EXPECT().FindUserRoles(ctx).Return([]model.UserRole{{UserID: uuid.New(), RoleID: viewerRole.ID}}, nil)
	m.namespace.EXPECT().FindAll(ctx).Return([]model.Namespace{acme}, nil)
}

func TestRoleService_List(t *testing.T) {
	m := newMocks(t)
	acme := model.Namespace{Base: model.Base{ID: uuid.New()}, Slug: "acme"}
	grants(m, acme, viewerRole, adminRole)

	got, err := NewRoleService(appContext.TestContext(nil), m.tx).List(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2)
	viewer, admin := got[0], got[1]
	assert.False(t, viewer.Builtin())
	assert.True(t, admin.Builtin())
	assert.Equal(t, 1, viewer.Users)
	assert.Zero(t, admin.Users)
	assert.Equal(t, []string{"devs"}, viewer.OIDCGroups)
	assert.Equal(t, []string{}, admin.OIDCGroups)
	require.Len(t, viewer.Namespace, 1)
	assert.Equal(t, "acme", viewer.Namespace[0].Namespace.Slug)
	assert.Empty(t, viewer.Global)
	require.Len(t, admin.Namespace, 1)
	assert.Nil(t, admin.Namespace[0].Namespace, "every namespace")
	assert.Len(t, admin.Global, 1)
}

func TestRoleService_Create(t *testing.T) {
	acme := model.Namespace{Base: model.Base{ID: uuid.New()}, Slug: "acme"}
	m := newMocks(t)
	m.namespace.EXPECT().FindByID(ctx, acme.ID).Return(&acme, nil)
	m.role.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ any, r *model.Role) error {
		assert.Equal(t, "acme-viewer", r.Code)
		assert.Equal(t, "Reads acme", *r.Description)
		r.ID = viewerRole.ID
		return nil
	})
	m.role.EXPECT().SetPermissions(ctx, viewerRole.ID,
		[]model.NamespacePermission{{NamespaceID: &acme.ID, Resource: model.ResourceInventory, Action: model.ActionRead}},
		[]model.GlobalPermission{{Resource: model.ResourceTokens, Action: model.ActionAll}}).Return(nil)
	m.role.EXPECT().SetOIDCGroups(ctx, viewerRole.ID, []string{"devs", "ops"}).Return(nil)
	grants(m, acme, viewerRole, adminRole)

	got, err := NewRoleService(appContext.TestContext(nil), m.tx).Create(ctx, RoleInput{Code: " acme-viewer ", Description: " Reads acme ",
		OIDCGroups: []string{" ops ", "devs", "ops"},
		Permissions: &RolePermissions{
			Namespace: []model.NamespacePermission{{Base: model.Base{ID: uuid.New()}, NamespaceID: &acme.ID, Resource: model.ResourceInventory, Action: model.ActionRead},
				{NamespaceID: ptr(acme.ID), Resource: model.ResourceInventory, Action: model.ActionRead}},
			Global: []model.GlobalPermission{{Resource: model.ResourceTokens, Action: model.ActionAll}, {Resource: model.ResourceTokens, Action: model.ActionAll}},
		}})
	require.NoError(t, err)
	assert.Equal(t, viewerRole.ID, got.ID)
}

func TestRoleService_Errors(t *testing.T) {
	errDB := errors.New("db down")
	acmeID := uuid.New()
	nsPerm := func(p model.NamespacePermission) *RolePermissions {
		return &RolePermissions{Namespace: []model.NamespacePermission{p}}
	}
	tests := []struct {
		name    string
		update  *model.Role
		in      RoleInput
		setup   func(m *mocks)
		wantErr error
		wantMsg string
	}{
		{name: "bad code", in: RoleInput{Code: "Acme Viewer"}, wantErr: ErrInvalid, wantMsg: "code"},
		{name: "empty group", in: RoleInput{Code: "x", OIDCGroups: []string{" "}}, wantErr: ErrInvalid, wantMsg: "OIDC group"},
		{name: "global resource on a namespace", in: RoleInput{Code: "x", Permissions: nsPerm(model.NamespacePermission{Resource: model.ResourceUsers, Action: model.ActionRead})},
			wantErr: ErrInvalid, wantMsg: "resource of a namespace"},
		{name: "bad action", in: RoleInput{Code: "x", Permissions: nsPerm(model.NamespacePermission{Resource: model.ResourceInventory, Action: "delete"})},
			wantErr: ErrInvalid, wantMsg: "no action"},
		{name: "namespace resource out of namespaces", in: RoleInput{Code: "x", Permissions: &RolePermissions{Global: []model.GlobalPermission{{Resource: model.ResourceMetadata, Action: model.ActionRead}}}},
			wantErr: ErrInvalid, wantMsg: "global resource"},
		{name: "bad global action", in: RoleInput{Code: "x", Permissions: &RolePermissions{Global: []model.GlobalPermission{{Resource: model.ResourceUsers, Action: "x"}}}},
			wantErr: ErrInvalid, wantMsg: "no action"},
		{name: "unknown namespace", in: RoleInput{Code: "x", Permissions: nsPerm(model.NamespacePermission{NamespaceID: &acmeID, Resource: model.ResourceAll, Action: model.ActionAll})},
			setup:   func(m *mocks) { m.namespace.EXPECT().FindByID(ctx, acmeID).Return(nil, gorm.ErrRecordNotFound) },
			wantErr: ErrNotFound},
		{name: "namespace db error", in: RoleInput{Code: "x", Permissions: nsPerm(model.NamespacePermission{NamespaceID: &acmeID, Resource: model.ResourceAll, Action: model.ActionAll})},
			setup:   func(m *mocks) { m.namespace.EXPECT().FindByID(ctx, acmeID).Return(nil, errDB) },
			wantErr: errDB},
		{name: "taken code", in: RoleInput{Code: "admin"},
			setup:   func(m *mocks) { m.role.EXPECT().Create(ctx, gomock.Any()).Return(gorm.ErrDuplicatedKey) },
			wantErr: ErrAlreadyExists},
		{name: "admin keeps its code", update: &adminRole, in: RoleInput{Code: "root"}, wantErr: ErrInvalid, wantMsg: "keeps its code"},
		{name: "admin keeps its permissions", update: &adminRole, in: RoleInput{Code: "admin", Permissions: &RolePermissions{}},
			wantErr: ErrInvalid, wantMsg: "every permission"},
		{name: "taken code on update", update: &viewerRole, in: RoleInput{Code: "admin"},
			setup:   func(m *mocks) { m.role.EXPECT().Update(ctx, gomock.Any()).Return(gorm.ErrDuplicatedKey) },
			wantErr: ErrAlreadyExists},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMocks(t)
			if tt.setup != nil {
				tt.setup(m)
			}
			svc := NewRoleService(appContext.TestContext(nil), m.tx)
			var err error
			if tt.update != nil {
				role := *tt.update
				m.role.EXPECT().FindByID(ctx, role.ID).Return(&role, nil)
				_, err = svc.Update(ctx, role.ID, tt.in)
			} else {
				_, err = svc.Create(ctx, tt.in)
			}
			assert.ErrorIs(t, err, tt.wantErr)
			if tt.wantMsg != "" {
				assert.ErrorContains(t, err, tt.wantMsg)
			}
		})
	}
}

func TestRoleService_Update(t *testing.T) {
	acme := model.Namespace{Base: model.Base{ID: uuid.New()}, Slug: "acme"}

	m := newMocks(t)
	admin := adminRole
	m.role.EXPECT().FindByID(ctx, admin.ID).Return(&admin, nil)
	m.role.EXPECT().Update(ctx, gomock.Any()).Return(nil)
	m.role.EXPECT().SetOIDCGroups(ctx, admin.ID, []string{"lacis-admins"}).Return(nil)
	grants(m, acme, viewerRole, adminRole)
	_, err := NewRoleService(appContext.TestContext(nil), m.tx).Update(ctx, admin.ID, RoleInput{Code: "admin", OIDCGroups: []string{"lacis-admins"}})
	require.NoError(t, err, "the admin role takes OIDC groups and a description")
	assert.Nil(t, admin.Description)

	m = newMocks(t)
	viewer := viewerRole
	m.role.EXPECT().FindByID(ctx, viewer.ID).Return(&viewer, nil)
	m.role.EXPECT().Update(ctx, gomock.Any()).Return(nil)
	m.role.EXPECT().SetPermissions(ctx, viewer.ID, []model.NamespacePermission{}, []model.GlobalPermission{}).Return(nil)
	m.role.EXPECT().SetOIDCGroups(ctx, viewer.ID, []string{}).Return(nil)
	grants(m, acme, viewerRole, adminRole)
	got, err := NewRoleService(appContext.TestContext(nil), m.tx).Update(ctx, viewer.ID, RoleInput{Code: "acme-readers", Permissions: &RolePermissions{}})
	require.NoError(t, err)
	assert.Equal(t, viewerRole.ID, got.ID)
	assert.Equal(t, "acme-readers", viewer.Code)

	m = newMocks(t)
	m.role.EXPECT().FindByID(ctx, viewer.ID).Return(nil, gorm.ErrRecordNotFound)
	_, err = NewRoleService(appContext.TestContext(nil), m.tx).Update(ctx, viewer.ID, RoleInput{Code: "x"})
	assert.ErrorIs(t, err, ErrNotFound)

	m = newMocks(t)
	m.role.EXPECT().FindAll(ctx).Return(nil, nil)
	m.role.EXPECT().FindGrants(ctx).Return(nil, nil, nil, nil)
	m.role.EXPECT().FindUserRoles(ctx).Return(nil, nil)
	m.namespace.EXPECT().FindAll(ctx).Return(nil, nil)
	_, err = NewRoleService(appContext.TestContext(nil), m.tx).Get(ctx, viewer.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRoleService_Delete(t *testing.T) {
	m := newMocks(t)
	svc := NewRoleService(appContext.TestContext(nil), m.tx)
	m.role.EXPECT().FindByID(ctx, adminRole.ID).Return(&adminRole, nil)
	assert.ErrorIs(t, svc.Delete(ctx, adminRole.ID), ErrInvalid)

	m.role.EXPECT().FindByID(ctx, viewerRole.ID).Return(&viewerRole, nil)
	m.role.EXPECT().Delete(ctx, viewerRole.ID).Return(nil)
	assert.NoError(t, svc.Delete(ctx, viewerRole.ID))

	m.role.EXPECT().FindByID(ctx, viewerRole.ID).Return(nil, gorm.ErrRecordNotFound)
	assert.ErrorIs(t, svc.Delete(ctx, viewerRole.ID), ErrNotFound)
}

func TestEnrollmentTokenService_ListRevoke(t *testing.T) {
	m := newMocks(t)
	a, b := model.EnrollmentToken{Base: model.Base{ID: uuid.New()}}, model.EnrollmentToken{Base: model.Base{ID: uuid.New()}}
	m.token.EXPECT().FindAll(ctx).Return([]model.EnrollmentToken{a, b}, nil)
	m.agent.EXPECT().CountByEnrollmentToken(ctx).Return(map[uuid.UUID]int{a.ID: 3}, nil).Times(2)
	svc := NewEnrollmentTokenService(testContext(t), m.token, m.agent)

	got, err := svc.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, []EnrollmentTokenDetail{{EnrollmentToken: a, Agents: 3}, {EnrollmentToken: b}}, got)

	m.token.EXPECT().Revoke(ctx, a.ID, now).Return(nil)
	m.token.EXPECT().FindByID(ctx, a.ID).Return(&a, nil)
	revoked, err := svc.Revoke(ctx, a.ID)
	require.NoError(t, err)
	assert.Equal(t, 3, revoked.Agents)

	m.token.EXPECT().Revoke(ctx, b.ID, now).Return(gorm.ErrRecordNotFound)
	_, err = svc.Revoke(ctx, b.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestAgentService_ListRevoke(t *testing.T) {
	m := newMocks(t)
	agent := repository.AgentRow{Agent: model.Agent{Base: model.Base{ID: uuid.New()}}}
	m.agent.EXPECT().FindAll(ctx).Return([]repository.AgentRow{agent}, nil).Times(2)
	svc := NewAgentService(testContext(t), m.token, m.agent)

	got, err := svc.List(ctx)
	require.NoError(t, err)
	assert.Len(t, got, 1)

	m.agent.EXPECT().Revoke(ctx, agent.ID, now).Return(nil)
	revoked, err := svc.Revoke(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, agent.ID, revoked.ID)

	other := uuid.New()
	m.agent.EXPECT().Revoke(ctx, other, now).Return(gorm.ErrRecordNotFound)
	_, err = svc.Revoke(ctx, other)
	assert.ErrorIs(t, err, ErrNotFound)
}

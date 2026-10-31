package resolver_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/auth"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

// tokenReader may read the tokens and the agents only.
func tokenReader() *auth.Principal {
	p := reader()
	p.Global = []model.GlobalPermission{{Resource: model.ResourceTokens, Action: model.ActionRead}}
	return p
}

func TestAccess(t *testing.T) {
	f := newFixture(t, tokenReader())
	assert.JSONEq(t, `{"access":{"users":{"read":false,"write":false},"roles":{"read":false,"write":false},"tokens":{"read":true,"write":false}}}`,
		f.post(t, `{ access { users { read write } roles { read write } tokens { read write } } }`))

	f = newFixture(t, admin())
	assert.JSONEq(t, `{"access":{"users":{"read":true,"write":true},"roles":{"read":true,"write":true},"tokens":{"read":true,"write":true}}}`,
		f.post(t, `{ access { users { read write } roles { read write } tokens { read write } } }`))
}

func TestAdmin_Forbidden(t *testing.T) {
	f := newFixture(t, tokenReader())
	for _, q := range []string{
		`{ users { id } }`,
		`{ roles { id } }`,
		`{ roleChoices { id } }`,
		`{ namespaceChoices { id } }`,
		`mutation { deleteUser(id:"` + id(1).String() + `") }`,
		`mutation { deleteRole(id:"` + id(1).String() + `") }`,
		`mutation { revokeAgent(id:"` + id(1).String() + `") { id } }`,
		`mutation { createEnrollmentToken(ttl:0) { secret } }`,
	} {
		assert.Contains(t, f.errors(t, q), `"code":"FORBIDDEN"`, q)
	}
}

func TestUsers(t *testing.T) {
	f := newFixture(t, admin())
	created := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	f.users.EXPECT().List(gomock.Any()).Return([]service.Account{
		{User: model.User{Base: model.Base{ID: id(1)}, Username: "alice", FirstName: ptr("Alice"), PasswordHash: ptr("h"), CreatedAt: created},
			Roles: []model.Role{{Base: model.Base{ID: id(2)}, Code: "admin"}}},
		{User: model.User{Base: model.Base{ID: id(3)}, Username: "sso", OIDCSubject: ptr("sub"), CreatedAt: created}, Roles: []model.Role{}},
	}, nil)
	assert.JSONEq(t, `{"users":[
		{"username":"alice","firstName":"Alice","local":true,"oidc":false,"createdAt":"2026-10-01T08:00:00Z","roles":[{"code":"admin"}]},
		{"username":"sso","firstName":null,"local":false,"oidc":true,"createdAt":"2026-10-01T08:00:00Z","roles":[]}]}`,
		f.post(t, `{ users { username firstName local oidc createdAt roles { code } } }`))
}

func TestUserMutations(t *testing.T) {
	p := admin()
	f := newFixture(t, p)
	account := &service.Account{User: model.User{Base: model.Base{ID: id(1)}, Username: "bob"}}

	f.users.EXPECT().Create(gomock.Any(), service.CreateUserInput{Username: "bob", Password: "12345678", FirstName: "Bob", RoleIDs: []uuid.UUID{id(2)}}).
		Return(&account.User, nil)
	f.users.EXPECT().Get(gomock.Any(), id(1)).Return(account, nil)
	assert.JSONEq(t, `{"createUser":{"username":"bob"}}`,
		f.post(t, `mutation { createUser(input:{username:"bob", password:"12345678", firstName:"Bob", roles:["`+id(2).String()+`"]}) { username } }`))

	f.users.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil, service.ErrAlreadyExists)
	assert.Contains(t, f.errors(t, `mutation { createUser(input:{username:"bob", password:"12345678"}) { username } }`), `"code":"CONFLICT"`)

	f.users.EXPECT().Update(gomock.Any(), gomock.Any(), id(1), service.UpdateUserInput{Password: ptr("new password")}).
		DoAndReturn(func(_ any, actor *auth.Principal, _ uuid.UUID, _ service.UpdateUserInput) (*service.Account, error) {
			assert.Equal(t, p.User.ID, actor.User.ID, "the actor is the user of the session")
			return account, nil
		})
	assert.JSONEq(t, `{"updateUser":{"username":"bob"}}`, f.post(t, `mutation { updateUser(id:"`+id(1).String()+`", input:{password:"new password"}) { username } }`))

	f.users.EXPECT().Update(gomock.Any(), gomock.Any(), id(1), gomock.Any()).Return(nil, service.ErrInvalid)
	assert.Contains(t, f.errors(t, `mutation { updateUser(id:"`+id(1).String()+`", input:{password:"x"}) { username } }`), `"code":"BAD_USER_INPUT"`)

	f.users.EXPECT().SetRoles(gomock.Any(), id(1), []uuid.UUID{}).Return(nil, service.ErrLastAdmin)
	assert.Contains(t, f.errors(t, `mutation { setUserRoles(id:"`+id(1).String()+`", roles:[]) { username } }`), `"code":"CONFLICT"`)
	f.users.EXPECT().SetRoles(gomock.Any(), id(1), []uuid.UUID{id(2)}).Return(account, nil)
	assert.JSONEq(t, `{"setUserRoles":{"username":"bob"}}`, f.post(t, `mutation { setUserRoles(id:"`+id(1).String()+`", roles:["`+id(2).String()+`"]) { username } }`))

	f.users.EXPECT().Delete(gomock.Any(), gomock.Any(), id(1)).Return(nil)
	assert.JSONEq(t, `{"deleteUser":true}`, f.post(t, `mutation { deleteUser(id:"`+id(1).String()+`") }`))
	f.users.EXPECT().Delete(gomock.Any(), gomock.Any(), id(1)).Return(service.ErrNotFound)
	assert.Contains(t, f.errors(t, `mutation { deleteUser(id:"`+id(1).String()+`") }`), `"code":"NOT_FOUND"`)
}

func TestRoles(t *testing.T) {
	f := newFixture(t, admin())
	viewer := service.RoleDetail{Role: model.Role{Base: model.Base{ID: id(2)}, Code: "acme-viewer", Description: ptr("Reads acme")}, Users: 2,
		OIDCGroups: []string{"devs"},
		Namespace: []service.NamespaceGrant{{NamespacePermission: model.NamespacePermission{NamespaceID: &acmeID, Resource: model.ResourceInventory, Action: model.ActionRead}, Namespace: acme},
			{NamespacePermission: model.NamespacePermission{Resource: model.ResourceAll, Action: model.ActionAll}}},
		Global: []model.GlobalPermission{{Resource: model.ResourceTokens, Action: model.ActionWrite}}}
	adminRole := service.RoleDetail{Role: model.Role{Base: model.Base{ID: id(1)}, Code: "admin"}}
	f.roles.EXPECT().List(gomock.Any()).Return([]service.RoleDetail{viewer, adminRole}, nil).Times(2)
	assert.JSONEq(t, `{"roles":[
		{"code":"acme-viewer","description":"Reads acme","builtin":false,"users":2,"oidcGroups":["devs"],
		 "permissions":[{"namespace":{"slug":"acme"},"resource":"INVENTORY","action":"READ"},{"namespace":null,"resource":"ALL","action":"ALL"}],
		 "globalPermissions":[{"resource":"TOKENS","action":"WRITE"}]},
		{"code":"admin","description":null,"builtin":true,"users":0,"oidcGroups":[],"permissions":[],"globalPermissions":[]}]}`,
		f.post(t, `{ roles { code description builtin users oidcGroups permissions { namespace { slug } resource action } globalPermissions { resource action } } }`))
	assert.JSONEq(t, `{"roleChoices":[{"code":"acme-viewer"},{"code":"admin"}]}`, f.post(t, `{ roleChoices { code } }`))

	f.namespaces.EXPECT().List(gomock.Any()).Return([]model.Namespace{*acme}, nil)
	assert.JSONEq(t, `{"namespaceChoices":[{"slug":"acme","name":"Acme"}]}`, f.post(t, `{ namespaceChoices { slug name } }`))
}

func TestRoleMutations(t *testing.T) {
	f := newFixture(t, admin())
	role := &service.RoleDetail{Role: model.Role{Base: model.Base{ID: id(2)}, Code: "acme-viewer"}}

	f.roles.EXPECT().Create(gomock.Any(), service.RoleInput{Code: "acme-viewer", OIDCGroups: []string{"devs"}, Permissions: &service.RolePermissions{
		Namespace: []model.NamespacePermission{{NamespaceID: &acmeID, Resource: model.ResourceInventory, Action: model.ActionRead}},
	}}).Return(role, nil)
	assert.JSONEq(t, `{"createRole":{"code":"acme-viewer"}}`, f.post(t, `mutation { createRole(input:{code:"acme-viewer", oidcGroups:["devs"],
		permissions:[{namespace:"`+acmeID.String()+`", resource:INVENTORY, action:READ}]}) { code } }`))

	f.roles.EXPECT().Update(gomock.Any(), id(1), service.RoleInput{Code: "admin", Description: "Everything"}).Return(role, nil)
	assert.JSONEq(t, `{"updateRole":{"code":"acme-viewer"}}`, f.post(t, `mutation { updateRole(id:"`+id(1).String()+`", input:{code:"admin", description:"Everything"}) { code } }`),
		"no permission list keeps the permissions")

	f.roles.EXPECT().Update(gomock.Any(), id(2), service.RoleInput{Code: "x", Permissions: &service.RolePermissions{
		Global: []model.GlobalPermission{{Resource: model.ResourceAll, Action: model.ActionRead}}}}).Return(nil, service.ErrInvalid)
	assert.Contains(t, f.errors(t, `mutation { updateRole(id:"`+id(2).String()+`", input:{code:"x", globalPermissions:[{resource:ALL, action:READ}]}) { code } }`),
		`"code":"BAD_USER_INPUT"`)

	f.roles.EXPECT().Delete(gomock.Any(), id(2)).Return(nil)
	assert.JSONEq(t, `{"deleteRole":true}`, f.post(t, `mutation { deleteRole(id:"`+id(2).String()+`") }`))
	f.roles.EXPECT().Delete(gomock.Any(), id(1)).Return(service.ErrInvalid)
	assert.Contains(t, f.errors(t, `mutation { deleteRole(id:"`+id(1).String()+`") }`), `"code":"BAD_USER_INPUT"`)
	f.roles.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil, service.ErrAlreadyExists)
	assert.Contains(t, f.errors(t, `mutation { createRole(input:{code:"admin"}) { code } }`), `"code":"CONFLICT"`)
}

func TestTokensAndAgents(t *testing.T) {
	f := newFixture(t, tokenReader())
	created := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	f.tokens.EXPECT().List(gomock.Any()).Return([]service.EnrollmentTokenDetail{
		{EnrollmentToken: model.EnrollmentToken{Base: model.Base{ID: id(1)}, Label: ptr("web"), CreatedAt: created, ExpiresAt: ptr(created.Add(time.Hour))}, Agents: 2},
	}, nil)
	assert.JSONEq(t, `{"enrollmentTokens":[{"label":"web","createdAt":"2026-10-01T08:00:00Z","expiresAt":"2026-10-01T09:00:00Z","revokedAt":null,"agents":2}]}`,
		f.post(t, `{ enrollmentTokens { label createdAt expiresAt revokedAt agents } }`))

	f.agents.EXPECT().List(gomock.Any()).Return([]repository.AgentRow{
		{Agent: model.Agent{Base: model.Base{ID: id(5)}, CreatedAt: created}, TokenLabel: ptr("web"), HostID: ptr(id(10)), Hostname: ptr("web-1"), Namespace: ptr("acme")},
		{Agent: model.Agent{Base: model.Base{ID: id(6)}, CreatedAt: created}},
	}, nil)
	assert.JSONEq(t, `{"agents":[{"tokenLabel":"web","host":{"hostname":"web-1","namespace":"acme"}},{"tokenLabel":null,"host":null}]}`,
		f.post(t, `{ agents { tokenLabel host { hostname namespace } } }`))
}

func TestTokenMutations(t *testing.T) {
	f := newFixture(t, admin())
	token := &model.EnrollmentToken{Base: model.Base{ID: id(1)}, Label: ptr("web")}
	f.tokens.EXPECT().Create(gomock.Any(), service.CreateEnrollmentTokenInput{Label: "web", TTL: 7 * 24 * time.Hour}).
		Return(&service.CreatedEnrollmentToken{Token: token, Secret: "lacis_et_secret"}, nil)
	assert.JSONEq(t, `{"createEnrollmentToken":{"secret":"lacis_et_secret","token":{"label":"web","agents":0}}}`,
		f.post(t, `mutation { createEnrollmentToken(label:"web", ttl:604800) { secret token { label agents } } }`))

	f.tokens.EXPECT().Create(gomock.Any(), service.CreateEnrollmentTokenInput{TTL: -time.Second}).Return(nil, service.ErrInvalid)
	assert.Contains(t, f.errors(t, `mutation { createEnrollmentToken(ttl:-1) { secret } }`), `"code":"BAD_USER_INPUT"`)

	revokedAt := now
	f.tokens.EXPECT().Revoke(gomock.Any(), id(1)).Return(&service.EnrollmentTokenDetail{EnrollmentToken: model.EnrollmentToken{RevokedAt: &revokedAt}}, nil)
	assert.JSONEq(t, `{"revokeEnrollmentToken":{"revokedAt":"2026-10-28T12:00:00Z"}}`,
		f.post(t, `mutation { revokeEnrollmentToken(id:"`+id(1).String()+`") { revokedAt } }`))
	f.tokens.EXPECT().Revoke(gomock.Any(), id(2)).Return(nil, service.ErrNotFound)
	assert.Contains(t, f.errors(t, `mutation { revokeEnrollmentToken(id:"`+id(2).String()+`") { revokedAt } }`), `"code":"NOT_FOUND"`)

	f.agents.EXPECT().Revoke(gomock.Any(), id(5)).Return(&repository.AgentRow{Agent: model.Agent{RevokedAt: &revokedAt}}, nil)
	assert.JSONEq(t, `{"revokeAgent":{"revokedAt":"2026-10-28T12:00:00Z"}}`, f.post(t, `mutation { revokeAgent(id:"`+id(5).String()+`") { revokedAt } }`))
	f.agents.EXPECT().Revoke(gomock.Any(), id(6)).Return(nil, service.ErrNotFound)
	assert.Contains(t, f.errors(t, `mutation { revokeAgent(id:"`+id(6).String()+`") { revokedAt } }`), `"code":"NOT_FOUND"`)
}

func TestTechIcons(t *testing.T) {
	at := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	f := newFixture(t, reader())
	f.techIcons.EXPECT().List(gomock.Any()).Return([]service.TechIconInfo{
		{TechIcon: model.TechIcon{Tech: "traefik", UpdatedAt: at}, UpdatedByName: ptr("admin")},
	}, nil)
	assert.JSONEq(t, `{"techIcons":[{"tech":"traefik","url":"icons/traefik?v=1793350800000","updatedAt":"2026-10-30T09:00:00Z","updatedBy":"admin"}]}`,
		f.post(t, `{ techIcons { tech url updatedAt updatedBy } }`), "every user reads the logos")
	assert.Contains(t, f.errors(t, `{ technologies }`), `"code":"FORBIDDEN"`)
	assert.JSONEq(t, `{"access":{"icons":{"read":false,"write":false}}}`, f.post(t, `{ access { icons { read write } } }`))

	f = newFixture(t, admin())
	f.techIcons.EXPECT().Technologies(gomock.Any()).Return([]string{"nginx", "traefik"}, nil)
	assert.JSONEq(t, `{"technologies":["nginx","traefik"],"access":{"icons":{"read":true,"write":true}}}`,
		f.post(t, `{ technologies access { icons { read write } } }`))
}

func TestTasks(t *testing.T) {
	at := time.Date(2026, 10, 30, 3, 15, 0, 0, time.UTC)
	f := newFixture(t, tokenReader())
	assert.Contains(t, f.errors(t, `{ tasks { kind } }`), `"code":"FORBIDDEN"`)
	assert.Contains(t, f.errors(t, `mutation { runTask(kind:"purge_snapshots") { id } }`), `"code":"FORBIDDEN"`)

	f = newFixture(t, admin())
	f.tasks.EXPECT().List(gomock.Any()).Return([]service.Task{
		{Kind: "purge_snapshots", Schedule: ptr("15 3 * * *"), NextRunAt: &at, Runs: []service.TaskRun{
			{ID: 7, Kind: "purge_snapshots", State: "completed", Attempt: 1, MaxAttempts: 5, CreatedAt: at, FinalizedAt: &at,
				Errors: []string{}, Output: json.RawMessage(`{"snapshots":12}`)},
		}},
		{Kind: "purge_inactive_hosts", Runs: []service.TaskRun{}},
	}, nil)
	assert.JSONEq(t, `{"tasks":[
		{"kind":"purge_snapshots","schedule":"15 3 * * *","nextRunAt":"2026-10-30T03:15:00Z",
		 "runs":[{"id":"7","state":"completed","attempt":1,"maxAttempts":5,"finalizedAt":"2026-10-30T03:15:00Z","errors":[],"output":{"snapshots":12}}]},
		{"kind":"purge_inactive_hosts","schedule":null,"nextRunAt":null,"runs":[]}],
		"access":{"tasks":{"read":true,"write":true}}}`,
		f.post(t, `{ tasks { kind schedule nextRunAt runs { id state attempt maxAttempts finalizedAt errors output } } access { tasks { read write } } }`))

	f.tasks.EXPECT().Failures(gomock.Any(), 5).Return([]service.TaskRun{{ID: 3, Kind: "normalize_snapshot", State: "discarded", Errors: []string{"boom"}}}, nil)
	assert.JSONEq(t, `{"taskFailures":[{"id":"3","kind":"normalize_snapshot","errors":["boom"]}]}`,
		f.post(t, `{ taskFailures(limit:5) { id kind errors } }`))
	f.tasks.EXPECT().Failures(gomock.Any(), 20).Return(nil, nil)
	f.post(t, `{ taskFailures { id } }`)

	f.tasks.EXPECT().Run(gomock.Any(), "purge_snapshots").Return(&service.TaskRun{ID: 8, State: "available", Errors: []string{}}, nil)
	assert.JSONEq(t, `{"runTask":{"id":"8","state":"available"}}`, f.post(t, `mutation { runTask(kind:"purge_snapshots") { id state } }`))
	f.tasks.EXPECT().Run(gomock.Any(), "nope").Return(nil, service.ErrNotFound)
	assert.Contains(t, f.errors(t, `mutation { runTask(kind:"nope") { id } }`), `"code":"NOT_FOUND"`)

	f.tasks.EXPECT().Retry(gomock.Any(), int64(3)).Return(&service.TaskRun{ID: 3, State: "available", Errors: []string{}}, nil)
	assert.JSONEq(t, `{"retryTaskRun":{"state":"available"}}`, f.post(t, `mutation { retryTaskRun(id:"3") { state } }`))
	assert.Contains(t, f.errors(t, `mutation { retryTaskRun(id:"x") { state } }`), `"code":"NOT_FOUND"`)
}

func TestAgents_Conflict(t *testing.T) {
	at := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	f := newFixture(t, tokenReader())
	f.agents.EXPECT().List(gomock.Any()).Return([]repository.AgentRow{
		{Agent: model.Agent{Base: model.Base{ID: id(5)}, ConflictAt: &at, ConflictHostnames: model.StringArray{"web-a", "web-b"}}},
		{Agent: model.Agent{Base: model.Base{ID: id(6)}}},
	}, nil)
	assert.JSONEq(t, `{"agents":[{"conflictAt":"2026-10-30T09:00:00Z","conflictHostnames":["web-a","web-b"]},{"conflictAt":null,"conflictHostnames":null}]}`,
		f.post(t, `{ agents { conflictAt conflictHostnames } }`))
}

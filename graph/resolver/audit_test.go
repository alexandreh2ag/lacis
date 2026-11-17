package resolver_test

import (
	"encoding/json"
	"testing"
	"time"

	mockService "github.com/alexandreh2ag/lacis/mocks/service"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

// withAudit gives the fixture an audit service.
func withAudit(t *testing.T, f *fixture) *mockService.MockAuditService {
	audit := mockService.NewMockAuditService(gomock.NewController(t))
	f.resolver.AuditService = audit
	return audit
}

func TestAuditEvents(t *testing.T) {
	f := newFixture(t, admin())
	audit := withAudit(t, f)
	from := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	audit.EXPECT().List(gomock.Any(), repository.AuditFilter{From: from, To: from.Add(24 * time.Hour), Username: "bob", Action: "host",
		Sources: []model.AuditSource{model.AuditMCP}, Search: "web", Sort: repository.AuditSortUsername, Offset: 20, Limit: 10}).
		Return([]model.AuditEvent{{ID: 7, At: from.Add(time.Hour), Username: "bob", Source: model.AuditMCP, Action: "get_host",
			Namespace: ptr("acme"), Target: ptr("web-1"), Details: json.RawMessage(`{"host":"web-1"}`)}}, int64(21), nil)
	assert.JSONEq(t, `{"auditEvents":{"total":21,"items":[{"id":7,"at":"2026-11-17T01:00:00Z","username":"bob","source":"MCP",
		"action":"get_host","namespace":"acme","project":null,"target":"web-1","details":{"host":"web-1"}}]}}`,
		f.post(t, `{ auditEvents(filter:{from:"2026-11-17T00:00:00Z", to:"2026-11-18T00:00:00Z", user:" bob ", action:" host ", sources:[MCP], search:"web"}, sort:{by:USER},
			page:{offset:20, limit:10}) { total items { id at username source action namespace project target details } } }`))

	audit.EXPECT().List(gomock.Any(), repository.AuditFilter{Sort: repository.AuditSortAt, Desc: true, Limit: 50}).Return(nil, int64(0), nil)
	assert.JSONEq(t, `{"auditEvents":{"total":0,"items":[]}}`, f.post(t, `{ auditEvents { total items { id } } }`), "newest first by default")
	audit.EXPECT().List(gomock.Any(), repository.AuditFilter{Sort: repository.AuditSortAction, Desc: true, Limit: 50}).Return(nil, int64(0), nil)
	f.post(t, `{ auditEvents(sort:{by:ACTION, desc:true}) { total } }`)

	audit.EXPECT().Usernames(gomock.Any(), "bo").Return([]string{"bob"}, nil)
	assert.JSONEq(t, `{"auditUsers":["bob"]}`, f.post(t, `{ auditUsers(search:"bo") }`))
	audit.EXPECT().Actions(gomock.Any(), "host").Return([]string{"get_host"}, nil)
	assert.JSONEq(t, `{"auditActions":["get_host"]}`, f.post(t, `{ auditActions(search:"host") }`))

	r := newFixture(t, reader())
	withAudit(t, r)
	for _, q := range []string{`{ auditEvents { total } }`, `{ auditUsers }`, `{ auditActions }`} {
		assert.Contains(t, r.errors(t, q), `"code":"FORBIDDEN"`, q)
	}
	assert.JSONEq(t, `{"access":{"audit":{"read":false,"write":false}}}`, r.post(t, `{ access { audit { read write } } }`))
	assert.JSONEq(t, `{"access":{"audit":{"read":true,"write":true}}}`, f.post(t, `{ access { audit { read write } } }`))
}

func TestAuditMutations(t *testing.T) {
	f := newFixture(t, admin())
	audit := withAudit(t, f)
	f.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil).AnyTimes()
	user := admin().User

	// A deletion names what it deleted.
	f.metadata.EXPECT().DeleteHost(gomock.Any(), "acme", id(12)).Return(&model.Host{Hostname: "web-1"}, nil)
	audit.EXPECT().Record(gomock.Any(), user, model.AuditUI, "deleteHost", service.AuditAbout{Namespace: "acme", Target: "web-1"},
		map[string]any{"namespace": "acme", "id": id(12)})
	f.post(t, `mutation { deleteHost(namespace:"acme", id:"`+id(12).String()+`") }`)

	// A user deleted is named before it goes.
	f.users.EXPECT().Get(gomock.Any(), id(1)).Return(&service.Account{User: model.User{Username: "bob"}}, nil)
	f.users.EXPECT().Delete(gomock.Any(), gomock.Any(), id(1)).Return(nil)
	audit.EXPECT().Record(gomock.Any(), user, model.AuditUI, "deleteUser", service.AuditAbout{Target: "bob"}, gomock.Any())
	f.post(t, `mutation { deleteUser(id:"`+id(1).String()+`") }`)

	// The project of its slug, the task of its kind.
	f.metadata.EXPECT().DeleteProject(gomock.Any(), "acme", "shop").Return(nil)
	audit.EXPECT().Record(gomock.Any(), user, model.AuditUI, "deleteProject", service.AuditAbout{Namespace: "acme", Project: "shop"}, gomock.Any())
	f.post(t, `mutation { deleteProject(namespace:"acme", slug:"shop") }`)
	f.tasks.EXPECT().Run(gomock.Any(), "purge_snapshots").Return(&service.TaskRun{ID: 1, Kind: "purge_snapshots"}, nil)
	audit.EXPECT().Record(gomock.Any(), user, model.AuditUI, "runTask", service.AuditAbout{Target: "purge_snapshots"}, gomock.Any())
	f.post(t, `mutation { runTask(kind:"purge_snapshots") { id } }`)

	// A refusal is not recorded: no Record expected.
	f.metadata.EXPECT().DeleteHost(gomock.Any(), "acme", id(10)).Return(nil, service.ErrNotFound)
	f.errors(t, `mutation { deleteHost(namespace:"acme", id:"`+id(10).String()+`") }`)
	r := newFixture(t, reader())
	withAudit(t, r)
	r.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil).AnyTimes()
	r.errors(t, `mutation { deleteHost(namespace:"acme", id:"`+id(10).String()+`") }`)

	// Nor a query.
	f.post(t, `{ access { audit { read } } }`)
}

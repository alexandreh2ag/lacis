package resolver_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/99designs/gqlgen/client"
	"github.com/alexandreh2ag/lacis/auth"
	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/graph/resolver"
	"github.com/alexandreh2ag/lacis/http/route/graphql"
	"github.com/alexandreh2ag/lacis/linker"
	mockClock "github.com/alexandreh2ag/lacis/mocks/common/clock"
	mockService "github.com/alexandreh2ag/lacis/mocks/service"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var (
	now     = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	acmeID  = uuid.MustParse("019a2400-0000-7000-8000-000000000a00")
	otherID = uuid.MustParse("019a2400-0000-7000-8000-000000000b00")
)

func id(n int) uuid.UUID {
	return uuid.MustParse("019a2400-0000-7000-8000-" + padded(n))
}

func padded(n int) string {
	s := "000000000000" + string(rune('0'+n/10)) + string(rune('0'+n%10))
	return s[len(s)-12:]
}

func ptr[T any](v T) *T { return &v }

// inventory: web (prod, shop) runs the app api behind nginx, using pg on
// db (prod, infra) and S3; old (staging) was last seen 10 days ago.
func inventory(t *testing.T) *model.Inventory {
	shop, infra, prod, staging := id(1), id(2), id(3), id(4)
	web, db, old := id(10), id(11), id(12)
	app, pg, nginx, network := id(20), id(21), id(22), id(30)
	detail := func(d linker.Detail) json.RawMessage {
		b, err := json.Marshal(d)
		require.NoError(t, err)
		return b
	}
	ns := model.Namespace{Base: model.Base{ID: acmeID}, Slug: "acme", Name: "Acme",
		StaleAfter: model.Interval(24 * time.Hour), InactiveAfter: model.Interval(7 * 24 * time.Hour)}
	return &model.Inventory{
		Namespace:    ns,
		Networks:     []model.Network{{Base: model.Base{ID: network}, NamespaceID: acmeID, Slug: "10-0-1-0-24", Name: "10.0.1.0/24", Kind: "private"}},
		CIDRs:        []model.NetworkCIDR{{NetworkID: network, NamespaceID: acmeID, CIDR: model.CIDR{Prefix: netip.MustParsePrefix("10.0.1.0/24")}}},
		Projects:     []model.Project{{Base: model.Base{ID: shop}, Slug: "shop", Name: "Shop"}, {Base: model.Base{ID: infra}, Slug: "infra", Name: "Infra"}},
		Environments: []model.Environment{{Base: model.Base{ID: prod}, Slug: "production", Name: "Production"}, {Base: model.Base{ID: staging}, Slug: "staging", Name: "Staging", SortOrder: 1}},
		Hosts: []model.Host{
			{Base: model.Base{ID: db}, NamespaceID: acmeID, Hostname: "db-1", ProjectID: &infra, EnvironmentID: &prod, LastSeenAt: now.Add(-time.Hour)},
			{Base: model.Base{ID: old}, NamespaceID: acmeID, Hostname: "old-1", EnvironmentID: &staging, LastSeenAt: now.Add(-10 * 24 * time.Hour)},
			{Base: model.Base{ID: web}, NamespaceID: acmeID, Hostname: "web-1", ProjectID: &shop, EnvironmentID: &prod, LastSeenAt: now.Add(-2 * 24 * time.Hour),
				OS: json.RawMessage(`{"id":"debian","version":"12","pretty":"Debian 12"}`)},
		},
		IPs: []model.HostIP{
			{HostID: web, IP: model.IP{Addr: netip.MustParseAddr("10.0.1.10")}, PrefixLen: 24, DefaultRoute: true},
			{HostID: web, IP: model.IP{Addr: netip.MustParseAddr("172.17.0.1")}, PrefixLen: 16},
		},
		Runtimes:   []model.Runtime{{Base: model.Base{ID: id(40)}, NamespaceID: acmeID, HostID: web, Type: "php", Version: "8.3.14", Path: "/usr/bin/php8.3"}},
		Services:   []model.Service{{Base: model.Base{ID: pg}, NamespaceID: acmeID, HostID: db, Type: "postgresql", Instance: "16/main", Version: ptr("16.4")}},
		Webservers: []model.Webserver{{Base: model.Base{ID: nginx}, NamespaceID: acmeID, HostID: web, Type: "nginx", Version: ptr("1.22.1")}},
		Apps: []model.App{{Base: model.Base{ID: app}, NamespaceID: acmeID, HostID: web, Path: "/srv/api", Name: "api", Role: "web",
			ProjectID: &shop, EnvironmentID: &prod, Framework: ptr("symfony"), FrameworkVersion: ptr("7.2"),
			Runtime: json.RawMessage(`{"type":"php","version":"8.3.14","source":"fpm_pool"}`),
			Code:    json.RawMessage(`{"source":"git","version":"v2.4.0","commit":"3f2a9c1","deployed_at":"2026-10-20T09:12:00Z"}`),
			Vhosts:  model.StringArray{"api.example"}}},
		Dependencies: []model.AppDependency{{AppID: app, Key: "DATABASE_URL", Scheme: ptr("postgresql"), Port: ptr(5432)}},
		Endpoints:    []model.Endpoint{{Kind: model.EndpointKindDomain, Address: "api.example", OwnerType: model.EndpointOwnerWebserver, OwnerID: nginx}},
		Links: []model.Link{
			{Base: model.Base{ID: id(50)}, NamespaceID: acmeID, FromType: model.LinkNodeApp, FromID: app, ToType: model.LinkNodeService, ToID: &pg,
				Source: model.LinkSourceEnv, Confidence: model.LinkConfidenceExact, Detail: detail(linker.Detail{Rule: linker.RulePrivateIP, Key: "DATABASE_URL"}), ComputedAt: now},
			{Base: model.Base{ID: id(51)}, NamespaceID: acmeID, FromType: model.LinkNodeApp, FromID: app, ToType: model.LinkNodeExternal, ExternalTarget: ptr("s3.example"),
				Source: model.LinkSourceEnv, Confidence: model.LinkConfidenceExact, Detail: detail(linker.Detail{Rule: linker.RuleExternal, Reason: linker.ReasonPublic, Key: "S3_URL"}), ComputedAt: now},
			{Base: model.Base{ID: id(52)}, NamespaceID: acmeID, FromType: model.LinkNodeWebserver, FromID: nginx, ToType: model.LinkNodeApp, ToID: &app,
				Source: model.LinkSourceWebserver, Confidence: model.LinkConfidenceInferred, Detail: detail(linker.Detail{Rule: linker.RuleVhost}), ComputedAt: now},
			{Base: model.Base{ID: id(53)}, NamespaceID: acmeID, FromType: model.LinkNodeApp, FromID: app, ToType: model.LinkNodeHost, ToID: &db,
				Source: model.LinkSourceEnv, Confidence: model.LinkConfidenceInferred, Detail: detail(linker.Detail{Rule: linker.RuleHostname, Key: "CACHE_URL"}), ComputedAt: now},
		},
	}
}

type fixture struct {
	namespaces *mockService.MockNamespaceService
	history    *mockService.MockHistoryService
	metadata   *mockService.MockMetadataService
	users      *mockService.MockUserService
	roles      *mockService.MockRoleService
	tokens     *mockService.MockEnrollmentTokenService
	agents     *mockService.MockAgentService
	techIcons  *mockService.MockTechIconService
	tasks      *mockService.MockTaskService
	client     *client.Client
}

// newFixture serves the API for principal; the inventory of acme is
// loaded once per request at most.
func newFixture(t *testing.T, p *auth.Principal) *fixture {
	ctrl := gomock.NewController(t)
	clock := mockClock.NewMockClock(ctrl)
	clock.EXPECT().Now().Return(now).AnyTimes()
	f := &fixture{namespaces: mockService.NewMockNamespaceService(ctrl), history: mockService.NewMockHistoryService(ctrl),
		metadata: mockService.NewMockMetadataService(ctrl), users: mockService.NewMockUserService(ctrl), roles: mockService.NewMockRoleService(ctrl),
		tokens: mockService.NewMockEnrollmentTokenService(ctrl), agents: mockService.NewMockAgentService(ctrl),
		techIcons: mockService.NewMockTechIconService(ctrl), tasks: mockService.NewMockTaskService(ctrl)}
	srv := graphql.NewServer(config.GraphQLConfig{Introspection: true, MaxDepth: 10, MaxComplexity: 1000},
		&resolver.Resolver{Clock: clock, NamespaceService: f.namespaces, HistoryService: f.history, MetadataService: f.metadata,
			UserService: f.users, RoleService: f.roles, EnrollmentTokenService: f.tokens, AgentService: f.agents,
			TechIconService: f.techIcons, TaskService: f.tasks})
	f.client = client.New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p != nil {
			r = r.WithContext(auth.WithPrincipal(r.Context(), p))
		}
		srv.ServeHTTP(w, r)
	}))
	return f
}

var acme = &model.Namespace{Base: model.Base{ID: acmeID}, Slug: "acme", Name: "Acme",
	StaleAfter: model.Interval(24 * time.Hour), InactiveAfter: model.Interval(7 * 24 * time.Hour)}

// reader may read acme only; admin may do everything.
func reader() *auth.Principal {
	return &auth.Principal{User: model.User{Base: model.Base{ID: id(90)}, Username: "bob"},
		Namespace: []model.NamespacePermission{{NamespaceID: &acmeID, Resource: model.ResourceInventory, Action: model.ActionRead}}}
}

func admin() *auth.Principal {
	return &auth.Principal{User: model.User{Base: model.Base{ID: id(91)}, Username: "admin", FirstName: ptr("Ada"), LastName: ptr("Lovelace")},
		Namespace: []model.NamespacePermission{{Resource: model.ResourceAll, Action: model.ActionAll}},
		Global:    []model.GlobalPermission{{Resource: model.ResourceAll, Action: model.ActionAll}}}
}

// post runs the query and decodes its data as JSON text.
func (f *fixture) post(t *testing.T, query string, opts ...client.Option) string {
	t.Helper()
	resp, err := f.client.RawPost(query, opts...)
	require.NoError(t, err)
	require.Empty(t, resp.Errors, string(resp.Errors))
	b, err := json.Marshal(resp.Data)
	require.NoError(t, err)
	return string(b)
}

func TestMeAndNamespaces(t *testing.T) {
	f := newFixture(t, reader())
	f.namespaces.EXPECT().List(gomock.Any()).Return([]model.Namespace{*acme, {Base: model.Base{ID: otherID}, Slug: "other"}}, nil)
	assert.JSONEq(t, `{"me":{"username":"bob","firstName":null,"lastName":null,"isAdmin":false},"namespaces":[{"slug":"acme"}]}`,
		f.post(t, `{ me { username firstName lastName isAdmin } namespaces { slug } }`))

	f = newFixture(t, admin())
	f.namespaces.EXPECT().List(gomock.Any()).Return([]model.Namespace{*acme, {Base: model.Base{ID: otherID}, Slug: "other"}}, nil)
	assert.JSONEq(t, `{"me":{"isAdmin":true},"namespaces":[{"slug":"acme"},{"slug":"other"}]}`, f.post(t, `{ me { isAdmin } namespaces { slug } }`))
}

func TestNamespace_Access(t *testing.T) {
	f := newFixture(t, reader())
	f.namespaces.EXPECT().BySlug(gomock.Any(), "other").Return(&model.Namespace{Base: model.Base{ID: otherID}, Slug: "other"}, nil)
	f.namespaces.EXPECT().BySlug(gomock.Any(), "nope").Return(nil, service.ErrNamespaceNotFound)
	assert.JSONEq(t, `{"a":null,"b":null}`, f.post(t, `{ a: namespace(slug:"other") { slug } b: namespace(slug:"nope") { slug } }`),
		"an unreadable namespace reads as unknown")

	f.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil)
	assert.JSONEq(t, `{"namespace":{"slug":"acme","canEdit":false}}`, f.post(t, `{ namespace(slug:"acme") { slug canEdit } }`))
}

func TestNamespace_Content(t *testing.T) {
	f := newFixture(t, admin())
	f.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil)
	f.namespaces.EXPECT().Inventory(gomock.Any(), acmeID).Return(inventory(t), nil).Times(1)

	got := f.post(t, `{ namespace(slug:"acme") {
		canEdit staleAfter inactiveAfter
		counts { projects environments hosts staleHosts inactiveHosts apps webservers services links externalLinks }
		projects { slug } environments { slug sortOrder }
		networks { slug cidrs }
		prod: hosts(filter:{envs:["production"]}) { total items { hostname status } }
		stale: hosts(filter:{status:[STALE, INACTIVE]}) { total items { hostname status } }
		search: hosts(filter:{search:"WEB"}, page:{limit:1}) { total items { hostname } }
		paged: hosts(page:{offset:2, limit:9999}) { total items { hostname } }
		far: hosts(page:{offset:9}) { total items { hostname } }
		apps(filter:{projects:["shop"]}) { total items { name } }
		noApps: apps(filter:{search:"zzz"}) { total }
		services(filter:{types:["postgresql"], projects:["infra"], search:"16"}) { total items { type instance host { hostname } } }
		noServices: services(filter:{envs:["staging"]}) { total }
	} }`)
	assert.JSONEq(t, `{"namespace":{
		"canEdit":true,"staleAfter":86400,"inactiveAfter":604800,
		"counts":{"projects":2,"environments":2,"hosts":3,"staleHosts":1,"inactiveHosts":1,"apps":1,"webservers":1,"services":1,"links":4,"externalLinks":1},
		"projects":[{"slug":"shop"},{"slug":"infra"}],"environments":[{"slug":"production","sortOrder":0},{"slug":"staging","sortOrder":1}],
		"networks":[{"slug":"10-0-1-0-24","cidrs":["10.0.1.0/24"]}],
		"prod":{"total":2,"items":[{"hostname":"db-1","status":"ACTIVE"},{"hostname":"web-1","status":"STALE"}]},
		"stale":{"total":2,"items":[{"hostname":"old-1","status":"INACTIVE"},{"hostname":"web-1","status":"STALE"}]},
		"search":{"total":1,"items":[{"hostname":"web-1"}]},
		"paged":{"total":3,"items":[{"hostname":"web-1"}]},
		"far":{"total":3,"items":[]},
		"apps":{"total":1,"items":[{"name":"api"}]},
		"noApps":{"total":0},
		"services":{"total":1,"items":[{"type":"postgresql","instance":"16/main","host":{"hostname":"db-1"}}]},
		"noServices":{"total":0}
	}}`, got)
}

func TestNamespace_Entities(t *testing.T) {
	f := newFixture(t, reader())
	f.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil)
	f.namespaces.EXPECT().Inventory(gomock.Any(), acmeID).Return(inventory(t), nil)

	got := f.post(t, `{ namespace(slug:"acme") {
		host(id:"`+id(10).String()+`") {
			hostname project { slug } environment { slug } os { id version pretty }
			ips { address prefixLength defaultRoute network { slug } }
			runtimes { type version } services { type } webservers { type domains links { to { __typename } } }
			apps { name }
		}
		noOS: host(id:"`+id(11).String()+`") { os { id } project { slug } services { type dependents { from { ... on App { name } } } } }
		noProject: host(id:"`+id(12).String()+`") { project { slug } }
		unknown: host(id:"`+id(99).String()+`") { hostname }
		app(id:"`+id(20).String()+`") {
			name project { slug } environment { slug } framework frameworkVersion vhosts
			runtime { type version path source } code { source version commit deployedAt branch }
			host { hostname }
			dependencies { key scheme port }
			links { key rule reason confidence to {
				__typename
				... on Service { type host { hostname } }
				... on External { target reason }
				... on Host { hostname }
			} }
			dependents { source confidence from { __typename ... on Webserver { type version host { hostname } } } }
		}
		service(id:"`+id(21).String()+`") { type version }
		webserver(id:"`+id(22).String()+`") { type host { hostname } dependents { id } }
	} }`)
	assert.JSONEq(t, `{"namespace":{
		"host":{"hostname":"web-1","project":{"slug":"shop"},"environment":{"slug":"production"},
			"os":{"id":"debian","version":"12","pretty":"Debian 12"},
			"ips":[{"address":"10.0.1.10","prefixLength":24,"defaultRoute":true,"network":{"slug":"10-0-1-0-24"}},
				{"address":"172.17.0.1","prefixLength":16,"defaultRoute":false,"network":null}],
			"runtimes":[{"type":"php","version":"8.3.14"}],"services":[],"webservers":[{"type":"nginx","domains":["api.example"],"links":[{"to":{"__typename":"App"}}]}],
			"apps":[{"name":"api"}]},
		"noOS":{"os":null,"project":{"slug":"infra"},"services":[{"type":"postgresql","dependents":[{"from":{"name":"api"}}]}]},
		"noProject":{"project":null},
		"unknown":null,
		"app":{"name":"api","project":{"slug":"shop"},"environment":{"slug":"production"},"framework":"symfony","frameworkVersion":"7.2",
			"vhosts":["api.example"],
			"runtime":{"type":"php","version":"8.3.14","path":null,"source":"fpm_pool"},
			"code":{"source":"git","version":"v2.4.0","commit":"3f2a9c1","deployedAt":"2026-10-20T09:12:00Z","branch":null},
			"host":{"hostname":"web-1"},
			"dependencies":[{"key":"DATABASE_URL","scheme":"postgresql","port":5432}],
			"links":[
				{"key":"DATABASE_URL","rule":"private_ip","reason":null,"confidence":"EXACT","to":{"__typename":"Service","type":"postgresql","host":{"hostname":"db-1"}}},
				{"key":"S3_URL","rule":"external","reason":"public_ip","confidence":"EXACT","to":{"__typename":"External","target":"s3.example","reason":"public_ip"}},
				{"key":"CACHE_URL","rule":"hostname","reason":null,"confidence":"INFERRED","to":{"__typename":"Host","hostname":"db-1"}}
			],
			"dependents":[{"source":"webserver","confidence":"INFERRED","from":{"__typename":"Webserver","type":"nginx","version":"1.22.1","host":{"hostname":"web-1"}}}]},
		"service":{"type":"postgresql","version":"16.4"},
		"webserver":{"type":"nginx","host":{"hostname":"web-1"},"dependents":[]}
	}}`, got)
}

func TestNamespace_EmptyFields(t *testing.T) {
	f := newFixture(t, reader())
	f.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil)
	inv := inventory(t)
	inv.Apps[0].Runtime, inv.Apps[0].Code, inv.Apps[0].Vhosts, inv.Apps[0].ProjectID, inv.Apps[0].EnvironmentID = nil, nil, nil, nil, nil
	inv.Endpoints = nil
	f.namespaces.EXPECT().Inventory(gomock.Any(), acmeID).Return(inv, nil)
	assert.JSONEq(t, `{"namespace":{"app":{"runtime":null,"code":null,"vhosts":[],"project":null,"environment":null},"webserver":{"domains":[]}}}`,
		f.post(t, `{ namespace(slug:"acme") { app(id:"`+id(20).String()+`") { runtime { type } code { source } vhosts project { slug } environment { slug } }
			webserver(id:"`+id(22).String()+`") { domains } } }`))
}

func TestHistory(t *testing.T) {
	f := newFixture(t, reader())
	f.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil)
	f.namespaces.EXPECT().Inventory(gomock.Any(), acmeID).Return(inventory(t), nil)
	since := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	entity := id(21)
	row := func(n int64) repository.ChangeEventRow {
		return repository.ChangeEventRow{ChangeEvent: model.ChangeEvent{ID: n, EntityType: model.ChangeEntityService, EntityID: &entity,
			Kind: model.ChangeChanged, Before: json.RawMessage(`{"version":"16.3"}`), After: json.RawMessage(`{"version":"16.4"}`), OccurredAt: now},
			Hostname: ptr("db-1")}
	}
	f.history.EXPECT().List(gomock.Any(), "acme", service.HistoryFilter{Hostname: "db-1", EntityType: "service", Since: since, OldestFirst: true, Offset: 1, Limit: 3}).
		Return([]repository.ChangeEventRow{row(3), row(2), row(1)}, nil)
	f.history.EXPECT().List(gomock.Any(), "acme", service.HistoryFilter{Hostname: "web-1", Limit: resolver.DefaultLimit + 1}).
		Return([]repository.ChangeEventRow{}, nil)

	got := f.post(t, `{ namespace(slug:"acme") {
		history(filter:{hostname:"db-1", entityType:"service", since:"2026-10-01T00:00:00Z", oldestFirst:true}, page:{offset:1, limit:2}) {
			hasMore items { id occurredAt hostname entityType entityId kind before after }
		}
		host(id:"`+id(10).String()+`") { history { hasMore items { id } } }
	} }`)
	assert.JSONEq(t, `{"namespace":{
		"history":{"hasMore":true,"items":[
			{"id":3,"occurredAt":"2026-10-28T12:00:00Z","hostname":"db-1","entityType":"service","entityId":"`+entity.String()+`","kind":"changed","before":{"version":"16.3"},"after":{"version":"16.4"}},
			{"id":2,"occurredAt":"2026-10-28T12:00:00Z","hostname":"db-1","entityType":"service","entityId":"`+entity.String()+`","kind":"changed","before":{"version":"16.3"},"after":{"version":"16.4"}}
		]},
		"host":{"history":{"hasMore":false,"items":[]}}
	}}`, got)
}

func TestErrors(t *testing.T) {
	f := newFixture(t, nil)
	resp, err := f.client.RawPost(`{ me { username } }`)
	require.NoError(t, err)
	assert.Contains(t, string(resp.Errors), "unauthenticated")

	f = newFixture(t, reader())
	f.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil)
	f.namespaces.EXPECT().Inventory(gomock.Any(), acmeID).Return(nil, errors.New("db down"))
	resp, err = f.client.RawPost(`{ namespace(slug:"acme") { counts { hosts } } }`)
	require.NoError(t, err)
	assert.Contains(t, string(resp.Errors), "db down")

	f = newFixture(t, reader())
	f.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(nil, errors.New("db down"))
	resp, err = f.client.RawPost(`{ namespace(slug:"acme") { slug } }`)
	require.NoError(t, err)
	assert.Contains(t, string(resp.Errors), "db down")

	f = newFixture(t, reader())
	resp, err = f.client.RawPost(`{ namespace(slug:"acme") { apps { items { host { apps { host { apps { host { apps { host { apps { name } } } } } } } } } } } }`)
	require.NoError(t, err)
	assert.Contains(t, string(resp.Errors), "operation has depth 12, which exceeds the limit of 10")
}

func TestNamespace_Graph(t *testing.T) {
	f := newFixture(t, reader())
	f.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil)
	f.namespaces.EXPECT().Inventory(gomock.Any(), acmeID).Return(inventory(t), nil).Times(1)

	got := f.post(t, `{ namespace(slug:"acme") {
		projects: graph(level: PROJECT) {
			level
			nodes { id kind label role techs count apps members selected status hosts { hostname } entity { __typename } }
			edges { id from to source confidence labels count crossEnv }
			groups { id }
		}
		apps: graph(level: APP, filter: {projects: ["shop"]}) {
			nodes { id kind label hostname version techs selected status entity { ... on App { name } ... on Service { type } ... on Host { hostname } } }
			edges { from to confidence labels }
			groups { label nodes }
		}
		all: graph(level: HOST, filter: {status: [ACTIVE, STALE, INACTIVE], externals: false, confidences: [EXACT]}) {
			nodes { id label status selected }
			edges { from to labels }
		}
	} }`)
	assert.JSONEq(t, `{"namespace":{
		"projects":{
			"edges":[
				{"confidence":"INFERRED","count":2,"crossEnv":false,"from":"project:shop@production","id":"project:shop@production→project:infra@production","labels":["DATABASE_URL","CACHE_URL"],"source":"env","to":"project:infra@production"},
				{"confidence":"EXACT","count":1,"crossEnv":false,"from":"project:shop@production","id":"project:shop@production→externals:production","labels":["S3_URL"],"source":"env","to":"externals:production"}],
			"groups":[],
			"level":"PROJECT",
			"nodes":[
				{"apps":0,"count":1,"entity":null,"hosts":[{"hostname":"db-1"}],"id":"project:infra@production","kind":"PROJECT","label":"infra","members":["db-1"],"role":"service","selected":true,"status":"ACTIVE","techs":["postgresql"]},
				{"apps":1,"count":1,"entity":null,"hosts":[{"hostname":"web-1"}],"id":"project:shop@production","kind":"PROJECT","label":"shop","members":["web-1"],"role":"app","selected":true,"status":"STALE","techs":["symfony","nginx","php"]},
				{"apps":0,"count":1,"entity":null,"hosts":[],"id":"externals:production","kind":"EXTERNAL","label":"externals","members":["s3.example"],"role":null,"selected":true,"status":null,"techs":[]}]},
		"apps":{
			"edges":[
				{"confidence":"EXACT","from":"app:019a2400-0000-7000-8000-000000000020","labels":["S3_URL"],"to":"external:s3.example"},
				{"confidence":"INFERRED","from":"app:019a2400-0000-7000-8000-000000000020","labels":["CACHE_URL"],"to":"host:019a2400-0000-7000-8000-000000000011"},
				{"confidence":"EXACT","from":"app:019a2400-0000-7000-8000-000000000020","labels":["DATABASE_URL"],"to":"service:019a2400-0000-7000-8000-000000000021"}],
			"groups":[
				{"label":"db-1","nodes":["service:019a2400-0000-7000-8000-000000000021"]},
				{"label":"web-1","nodes":["app:019a2400-0000-7000-8000-000000000020"]}],
			"nodes":[
				{"entity":null,"hostname":null,"id":"external:s3.example","kind":"EXTERNAL","label":"s3.example","selected":false,"status":null,"techs":[],"version":null},
				{"entity":{"hostname":"db-1"},"hostname":"db-1","id":"host:019a2400-0000-7000-8000-000000000011","kind":"HOST","label":"db-1","selected":false,"status":"ACTIVE","techs":["postgresql"],"version":null},
				{"entity":{"type":"postgresql"},"hostname":"db-1","id":"service:019a2400-0000-7000-8000-000000000021","kind":"SERVICE","label":"postgresql 16/main","selected":false,"status":"ACTIVE","techs":["postgresql"],"version":null},
				{"entity":{"name":"api"},"hostname":"web-1","id":"app:019a2400-0000-7000-8000-000000000020","kind":"APP","label":"api","selected":true,"status":"STALE","techs":["symfony","php","nginx"],"version":"v2.4.0"}]},
		"all":{
			"edges":[
				{"from":"host:019a2400-0000-7000-8000-000000000010","labels":["DATABASE_URL"],"to":"host:019a2400-0000-7000-8000-000000000011"}],
			"nodes":[
				{"id":"host:019a2400-0000-7000-8000-000000000011","label":"db-1","selected":true,"status":"ACTIVE"},
				{"id":"host:019a2400-0000-7000-8000-000000000012","label":"old-1","selected":true,"status":"INACTIVE"},
				{"id":"host:019a2400-0000-7000-8000-000000000010","label":"web-1","selected":true,"status":"STALE"}]}}}`, got, "inactive old-1 is left out but on demand; nginx is folded into api; the db host is reached by CACHE_URL")
}

// errors runs the query and returns its errors as JSON text.
func (f *fixture) errors(t *testing.T, query string) string {
	t.Helper()
	resp, err := f.client.RawPost(query)
	require.NoError(t, err)
	require.NotEmpty(t, resp.Errors)
	return string(resp.Errors)
}

// editor may read acme and change its metadata, not its inventory.
func editor() *auth.Principal {
	p := reader()
	p.Namespace = append(p.Namespace, model.NamespacePermission{NamespaceID: &acmeID, Resource: model.ResourceMetadata, Action: model.ActionWrite})
	return p
}

func TestMutations_Access(t *testing.T) {
	f := newFixture(t, reader())
	f.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil)
	got := f.errors(t, `mutation { updateNamespace(namespace:"acme", input:{name:"x"}) { name } }`)
	assert.Contains(t, got, `"code":"FORBIDDEN"`)
	assert.Contains(t, got, "metadata write is needed")

	f.namespaces.EXPECT().BySlug(gomock.Any(), "other").Return(&model.Namespace{Base: model.Base{ID: otherID}, Slug: "other"}, nil)
	assert.Contains(t, f.errors(t, `mutation { updateNamespace(namespace:"other", input:{}) { name } }`), `"code":"NOT_FOUND"`,
		"a namespace the user cannot read reads as unknown")
	f.namespaces.EXPECT().BySlug(gomock.Any(), "nope").Return(nil, service.ErrNamespaceNotFound)
	assert.Contains(t, f.errors(t, `mutation { updateProject(namespace:"nope", slug:"shop", input:{}) { name } }`), `"code":"NOT_FOUND"`)

	f = newFixture(t, editor())
	f.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil)
	assert.Contains(t, f.errors(t, `mutation { deleteHost(namespace:"acme", id:"`+id(12).String()+`") }`), "inventory write is needed",
		"deleting a host changes the inventory")
}

func TestMutations_Metadata(t *testing.T) {
	f := newFixture(t, editor())
	f.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil).AnyTimes()

	stale, inactive := 2*time.Hour, 48*time.Hour
	updated := *acme
	updated.Name, updated.StaleAfter, updated.InactiveAfter = "Acme Corp", model.Interval(stale), model.Interval(inactive)
	f.metadata.EXPECT().UpdateNamespace(gomock.Any(), "acme", service.NamespaceInput{Name: ptr("Acme Corp"), Description: ptr(""),
		StaleAfter: &stale, InactiveAfter: &inactive}).Return(&updated, nil)
	assert.JSONEq(t, `{"updateNamespace":{"name":"Acme Corp","staleAfter":7200,"inactiveAfter":172800,"snapshotRetention":0}}`,
		f.post(t, `mutation { updateNamespace(namespace:"acme", input:{name:"Acme Corp", description:"", staleAfter:7200, inactiveAfter:172800}) {
			name staleAfter inactiveAfter snapshotRetention } }`))

	f.metadata.EXPECT().UpdateProject(gomock.Any(), "acme", "shop", service.ProjectInput{Name: ptr(" ")}).
		Return(nil, fmt.Errorf("%w: name must hold 1 to 100 characters", service.ErrInvalid))
	got := f.errors(t, `mutation { updateProject(namespace:"acme", slug:"shop", input:{name:" "}) { name } }`)
	assert.Contains(t, got, `"code":"BAD_USER_INPUT"`)
	assert.Contains(t, got, "name must hold 1 to 100 characters")

	f.metadata.EXPECT().UpdateEnvironment(gomock.Any(), "acme", "staging", service.EnvironmentInput{Name: ptr("Staging")}).
		Return(&model.Environment{NamespaceID: acmeID, Slug: "staging", Name: "Staging"}, nil)
	f.metadata.EXPECT().ReorderEnvironments(gomock.Any(), "acme", []string{"staging", "production"}).
		Return([]model.Environment{{NamespaceID: acmeID, Slug: "staging"}, {NamespaceID: acmeID, Slug: "production", SortOrder: 1}}, nil)
	f.metadata.EXPECT().UpdateNetwork(gomock.Any(), "acme", "lan", service.NetworkInput{Kind: ptr("public")}).
		Return(&model.Network{NamespaceID: acmeID, Slug: "lan", Name: "LAN", Kind: "public"}, nil)
	f.metadata.EXPECT().UpdateProject(gomock.Any(), "acme", "nope", service.ProjectInput{}).Return(nil, service.ErrNotFound)
	assert.JSONEq(t, `{"e":{"name":"Staging"},"o":[{"slug":"staging","sortOrder":0},{"slug":"production","sortOrder":1}],"n":{"kind":"public"}}`,
		f.post(t, `mutation {
			e: updateEnvironment(namespace:"acme", slug:"staging", input:{name:"Staging"}) { name }
			o: reorderEnvironments(namespace:"acme", slugs:["staging","production"]) { slug sortOrder }
			n: updateNetwork(namespace:"acme", slug:"lan", input:{kind:"public"}) { kind }
		}`))
	assert.Contains(t, f.errors(t, `mutation { updateProject(namespace:"acme", slug:"nope", input:{}) { name } }`), `"code":"NOT_FOUND"`)
}

func TestMutations_DeleteHost(t *testing.T) {
	f := newFixture(t, admin())
	f.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil).AnyTimes()
	f.metadata.EXPECT().DeleteHost(gomock.Any(), "acme", id(12)).Return(nil)
	assert.JSONEq(t, `{"deleteHost":true}`, f.post(t, `mutation { deleteHost(namespace:"acme", id:"`+id(12).String()+`") }`))

	f.metadata.EXPECT().DeleteHost(gomock.Any(), "acme", id(10)).Return(service.ErrHostActive)
	got := f.errors(t, `mutation { deleteHost(namespace:"acme", id:"`+id(10).String()+`") }`)
	assert.Contains(t, got, `"code":"CONFLICT"`)
	assert.Contains(t, got, "its next snapshot would bring it back")
}

func TestNamespace_Sort(t *testing.T) {
	f := newFixture(t, reader())
	f.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil)
	f.namespaces.EXPECT().Inventory(gomock.Any(), acmeID).Return(inventory(t), nil)
	assert.JSONEq(t, `{"namespace":{
		"byName":{"items":[{"hostname":"db-1"},{"hostname":"old-1"},{"hostname":"web-1"}]},
		"byNameDesc":{"items":[{"hostname":"web-1"},{"hostname":"old-1"},{"hostname":"db-1"}]},
		"byStatus":{"items":[{"hostname":"old-1"},{"hostname":"web-1"},{"hostname":"db-1"}]},
		"byApps":{"items":[{"hostname":"web-1"},{"hostname":"db-1"},{"hostname":"old-1"}]},
		"byIP":{"items":[{"hostname":"db-1"},{"hostname":"old-1"},{"hostname":"web-1"}]},
		"byVersion":{"items":[{"name":"api"}]},
		"searchIP":{"items":[{"hostname":"web-1"}]},
		"searchVersion":{"items":[{"hostname":"db-1"}]},
		"services":{"items":[{"type":"postgresql"}]}
	}}`, f.post(t, `{ namespace(slug:"acme") {
		byName: hosts { items { hostname } }
		byNameDesc: hosts(sort:{by:HOSTNAME, desc:true}) { items { hostname } }
		byStatus: hosts(sort:{by:STATUS, desc:true}) { items { hostname } }
		byApps: hosts(sort:{by:APPS, desc:true}) { items { hostname } }
		byIP: hosts(sort:{by:IP}) { items { hostname } }
		byVersion: apps(sort:{by:VERSION}) { items { name } }
		searchIP: hosts(filter:{search:"10.0.1.10"}) { items { hostname } }
		searchVersion: hosts(filter:{search:"16.4"}) { items { hostname } }
		services(sort:{by:HOSTNAME, desc:true}) { items { type } }
	} }`))
}

func TestNatural(t *testing.T) {
	assert.Negative(t, resolver.Natural("web-2", "web-10"))
	assert.Negative(t, resolver.Natural("8.9", "8.10"))
	assert.Negative(t, resolver.Natural("Alpha", "beta"))
	assert.Positive(t, resolver.Natural("v2.5.0", "v2.4.0"))
	assert.Zero(t, resolver.Natural("a01", "a1"))
	assert.Negative(t, resolver.Natural("a", "a1"))
}

func TestNamespace_GraphExpand(t *testing.T) {
	f := newFixture(t, reader())
	f.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil)
	f.namespaces.EXPECT().Inventory(gomock.Any(), acmeID).Return(inventory(t), nil).Times(1)

	got := f.post(t, `{ namespace(slug:"acme") {
		graph(level: PROJECT, filter: {envs: ["production"], expand: ["project:shop@production"]}) {
			nodes { id kind expanded count hosts { hostname } entity { __typename } }
			edges { from to labels }
			groups { id nodes }
		}
	} }`)
	zone := "item:project:shop@production|"
	assert.JSONEq(t, `{"namespace":{"graph":{
		"edges":[
			{"from":"`+zone+`webserver|nginx","labels":[],"to":"`+zone+`app|api"},
			{"from":"`+zone+`app|api","labels":["DATABASE_URL","CACHE_URL"],"to":"project:infra@production"},
			{"from":"`+zone+`app|api","labels":["S3_URL"],"to":"externals:production"}],
		"groups":[{"id":"project:shop@production","nodes":["`+zone+`webserver|nginx","`+zone+`app|api","`+zone+`runtime|php 8.3.14"]}],
		"nodes":[
			{"count":1,"entity":null,"expanded":false,"hosts":[{"hostname":"db-1"}],"id":"project:infra@production","kind":"PROJECT"},
			{"count":1,"entity":null,"expanded":true,"hosts":[{"hostname":"web-1"}],"id":"project:shop@production","kind":"PROJECT"},
			{"count":1,"entity":null,"expanded":false,"hosts":[],"id":"externals:production","kind":"EXTERNAL"},
			{"count":1,"entity":{"__typename":"Webserver"},"expanded":false,"hosts":[{"hostname":"web-1"}],"id":"`+zone+`webserver|nginx","kind":"WEBSERVER"},
			{"count":1,"entity":{"__typename":"App"},"expanded":false,"hosts":[{"hostname":"web-1"}],"id":"`+zone+`app|api","kind":"APP"},
			{"count":1,"entity":null,"expanded":false,"hosts":[{"hostname":"web-1"}],"id":"`+zone+`runtime|php 8.3.14","kind":"RUNTIME"}]}}}`,
		got, "the links of the opened project leave from its content")
}

func TestNamespace_Impact(t *testing.T) {
	f := newFixture(t, reader())
	f.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil)
	f.namespaces.EXPECT().Inventory(gomock.Any(), acmeID).Return(inventory(t), nil).Times(1)

	got := f.post(t, `{ namespace(slug:"acme") {
		down: impact(level: PROJECT, node: "project:infra@production", direction: DEPENDENTS) { nodes { id level } edges }
		needs: impact(level: PROJECT, node: "project:shop@production", direction: NEEDS) { nodes { id level } edges }
	} }`)
	assert.JSONEq(t, `{"namespace":{
		"down":{"edges":["project:shop@production→project:infra@production"],"nodes":[{"id":"project:shop@production","level":1}]},
		"needs":{"edges":["project:shop@production→externals:production","project:shop@production→project:infra@production"],
			"nodes":[{"id":"externals:production","level":1},{"id":"project:infra@production","level":1}]}}}`, got)
}

func TestNamespace_AppFilters(t *testing.T) {
	f := newFixture(t, reader())
	f.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil)
	f.namespaces.EXPECT().Inventory(gomock.Any(), acmeID).Return(inventory(t), nil).Times(1)

	got := f.post(t, `{ namespace(slug:"acme") {
		facets { frameworks { name versions } runtimes { name versions } serviceTypes }
		recent: apps(filter: {runtime: {name: "php", op: GTE, version: "8.3"}}) { total }
		old: apps(filter: {runtime: {name: "php", op: LT, version: "8"}}) { total }
		symfony7: apps(filter: {framework: {name: "symfony", op: EQ, version: "7"}}) { total }
		symfony6: apps(filter: {framework: {name: "symfony", op: LT, version: "7"}}) { total }
		laravel: apps(filter: {framework: {name: "laravel"}}) { total }
		searched: apps(filter: {search: "Symfony 7.2"}) { total }
	} }`)
	assert.JSONEq(t, `{"namespace":{
		"facets":{"frameworks":[{"name":"symfony","versions":["7.2"]}],"runtimes":[{"name":"php","versions":["8.3"]}],"serviceTypes":["postgresql"]},
		"recent":{"total":1},"old":{"total":0},"symfony7":{"total":1},"symfony6":{"total":0},"laravel":{"total":0},"searched":{"total":1}}}`, got)
}

func TestNamespace_SplitNetworks(t *testing.T) {
	f := newFixture(t, reader())
	inv := inventory(t)
	// db-1 shares the range of web-1, moved into a network split from it.
	lan, split := id(30), id(31)
	inv.Networks = append(inv.Networks, model.Network{Base: model.Base{ID: split}, NamespaceID: acmeID, Slug: "lan-b", Name: "LAN B", Kind: "private", SplitFromID: &lan})
	inv.IPs = append(inv.IPs, model.HostIP{HostID: id(11), IP: model.IP{Addr: netip.MustParseAddr("10.0.1.20")}, PrefixLen: 24})
	inv.HostNetworks = []model.HostNetwork{{HostID: id(11), FromNetworkID: lan, NetworkID: split}}
	f.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil)
	f.namespaces.EXPECT().Inventory(gomock.Any(), acmeID).Return(inv, nil)

	assert.JSONEq(t, `{"namespace":{
		"networks":[
			{"slug":"10-0-1-0-24","cidrs":["10.0.1.0/24"],"splitFrom":null,"hosts":[{"hostname":"web-1"}]},
			{"slug":"lan-b","cidrs":["10.0.1.0/24"],"splitFrom":{"slug":"10-0-1-0-24"},"hosts":[{"hostname":"db-1"}]}
		],
		"host":{"ips":[{"network":{"slug":"lan-b"}}]}
	}}`, f.post(t, `{ namespace(slug:"acme") {
		networks { slug cidrs splitFrom { slug } hosts { hostname } }
		host(id:"`+id(11).String()+`") { ips { network { slug } } }
	} }`))
}

func TestMutations_SplitNetwork(t *testing.T) {
	f := newFixture(t, admin())
	f.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil).AnyTimes()
	lan := id(30)
	f.metadata.EXPECT().SplitNetwork(gomock.Any(), "acme", "lan", "LAN B", []uuid.UUID{id(10), id(11)}).
		Return(&model.Network{Base: model.Base{ID: id(31)}, NamespaceID: acmeID, Slug: "lan-b", Name: "LAN B", SplitFromID: &lan}, nil)
	f.metadata.EXPECT().MoveHostsToNetwork(gomock.Any(), "acme", "lan", []uuid.UUID{id(10)}).
		Return(&model.Network{Base: model.Base{ID: lan}, NamespaceID: acmeID, Slug: "lan"}, nil)
	f.metadata.EXPECT().DeleteNetwork(gomock.Any(), "acme", "lan-b").Return(nil)
	assert.JSONEq(t, `{"s":{"slug":"lan-b","name":"LAN B"},"m":{"slug":"lan"},"d":true}`, f.post(t, `mutation {
		s: splitNetwork(namespace:"acme", slug:"lan", name:"LAN B", hosts:["`+id(10).String()+`","`+id(11).String()+`"]) { slug name }
		m: moveHostsToNetwork(namespace:"acme", slug:"lan", hosts:["`+id(10).String()+`"]) { slug }
		d: deleteNetwork(namespace:"acme", slug:"lan-b")
	}`))

	f.metadata.EXPECT().DeleteNetwork(gomock.Any(), "acme", "lan").Return(fmt.Errorf("%w: only a network split by hand is deleted", service.ErrInvalid))
	assert.Contains(t, f.errors(t, `mutation { deleteNetwork(namespace:"acme", slug:"lan") }`), `"code":"BAD_USER_INPUT"`)

	r := newFixture(t, reader())
	r.namespaces.EXPECT().BySlug(gomock.Any(), "acme").Return(acme, nil).AnyTimes()
	assert.Contains(t, r.errors(t, `mutation { deleteNetwork(namespace:"acme", slug:"lan-b") }`), `"code":"FORBIDDEN"`)
}

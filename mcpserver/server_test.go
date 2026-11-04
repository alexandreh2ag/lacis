package mcpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/auth"
	"github.com/alexandreh2ag/lacis/common/labels"
	"github.com/alexandreh2ag/lacis/config"
	appContext "github.com/alexandreh2ag/lacis/context"
	mcpRoute "github.com/alexandreh2ag/lacis/http/route/mcp"
	"github.com/alexandreh2ag/lacis/mcpserver"
	mockClock "github.com/alexandreh2ag/lacis/mocks/common/clock"
	mockService "github.com/alexandreh2ag/lacis/mocks/service"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/alexandreh2ag/lacis/topology"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var (
	now = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

	acme    = model.Namespace{Base: model.Base{ID: uuid.MustParse("00000000-0000-0000-0000-00000000000a")}, Slug: "acme", Name: "Acme"}
	other   = model.Namespace{Base: model.Base{ID: uuid.MustParse("00000000-0000-0000-0000-00000000000b")}, Slug: "other", Name: "Other"}
	hidden  = model.Namespace{Base: model.Base{ID: uuid.MustParse("00000000-0000-0000-0000-00000000000c")}, Slug: "hidden", Name: "Hidden"}
	shop    = model.Project{Base: model.Base{ID: uuid.New()}, Slug: "shop", Name: "Shop"}
	prod    = model.Environment{Base: model.Base{ID: uuid.New()}, Slug: "production", SortOrder: 2}
	staging = model.Environment{Base: model.Base{ID: uuid.New()}, Slug: "staging", SortOrder: 1}

	web1  = model.Host{Base: model.Base{ID: uuid.New()}, Hostname: "web-1", ProjectID: &shop.ID, EnvironmentID: &prod.ID, Labels: labels.Labels{Role: "front"}, OS: json.RawMessage(`{"name":"debian","version":"12"}`)}
	web2  = model.Host{Base: model.Base{ID: uuid.New()}, Hostname: "web-2", ProjectID: &shop.ID, EnvironmentID: &staging.ID}
	db1   = model.Host{Base: model.Base{ID: uuid.New()}, Hostname: "db-1", ProjectID: &shop.ID, EnvironmentID: &prod.ID}
	front = model.App{Base: model.Base{ID: uuid.New()}, HostID: web1.ID, Name: "shop-front", Path: "/srv/shop", Role: "web",
		Framework: new("symfony"), FrameworkVersion: new("6.4.12"), Runtime: json.RawMessage(`{"type":"php","version":"8.1.27"}`), Vhosts: model.StringArray{"shop.acme.example"}}
	legacy = model.App{Base: model.Base{ID: uuid.New()}, HostID: web2.ID, Name: "legacy", Path: "/srv/legacy",
		Framework: new("laravel"), FrameworkVersion: new("8.83.1"), Runtime: json.RawMessage(`{"type":"php","version":"7.4.33"}`)}
	mysql = model.Service{Base: model.Base{ID: uuid.New()}, HostID: db1.ID, Type: "mysql", Instance: "default", Version: new("8.0.36")}
	nginx = model.Webserver{Base: model.Base{ID: uuid.New()}, HostID: web1.ID, Type: "nginx", Version: new("1.24.0")}
)

func inventory() *model.Inventory {
	return &model.Inventory{
		Projects: []model.Project{shop}, Environments: []model.Environment{prod, staging},
		Hosts: []model.Host{web1, web2, db1},
		IPs:   []model.HostIP{{HostID: db1.ID, IP: model.IP{Addr: netip.MustParseAddr("10.0.2.10")}}},
		Runtimes: []model.Runtime{
			{HostID: web1.ID, Type: "php", Version: "8.1.2-1ubuntu2.18", Path: "/usr/bin/php8.1"},
			{HostID: web2.ID, Type: "php", Version: "7.4.33", Path: "/usr/bin/php7.4"},
		},
		Services:   []model.Service{mysql},
		Webservers: []model.Webserver{nginx},
		Apps:       []model.App{front, legacy},
		Containers: []model.Container{{HostID: web2.ID, Name: "cache", Image: "docker.io/library/redis:7.2", Version: new("7.2.4"), State: "running"}},
		Dependencies: []model.AppDependency{{AppID: front.ID, Key: "DATABASE_URL", Scheme: new("mysql"), Port: new(3306),
			Target: json.RawMessage(`{"host":"db-1"}`), Source: json.RawMessage(`{"file":".env"}`)}},
		Endpoints: []model.Endpoint{{OwnerType: model.EndpointOwnerWebserver, OwnerID: nginx.ID, Kind: model.EndpointKindDomain, Address: "www.acme.example"}},
		Links: []model.Link{
			{FromType: model.LinkNodeApp, FromID: front.ID, ToType: model.LinkNodeService, ToID: &mysql.ID, Source: model.LinkSourceEnv, Confidence: model.LinkConfidenceExact},
			{FromType: model.LinkNodeWebserver, FromID: nginx.ID, ToType: model.LinkNodeApp, ToID: &front.ID, Source: model.LinkSourceWebserver, Confidence: model.LinkConfidenceExact},
			{FromType: model.LinkNodeApp, FromID: legacy.ID, ToType: model.LinkNodeExternal, ExternalTarget: new("api.stripe.example"), Source: model.LinkSourceEnv, Confidence: model.LinkConfidenceInferred},
		},
	}
}

type fixture struct {
	ns      *mockService.MockNamespaceService
	graph   *mockService.MockGraphService
	history *mockService.MockHistoryService
	oauth   *mockService.MockOAuthService
	session *mcp.ClientSession
	logs    *bytes.Buffer
	logger  *slog.Logger
	// principal is the user of the access token.
	principal *auth.Principal
}

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

// newFixture serves the MCP endpoint and connects a client with an access
// token of a user reading acme and other.
func newFixture(t *testing.T) *fixture {
	ctrl := gomock.NewController(t)
	f := &fixture{ns: mockService.NewMockNamespaceService(ctrl), graph: mockService.NewMockGraphService(ctrl),
		history: mockService.NewMockHistoryService(ctrl), oauth: mockService.NewMockOAuthService(ctrl),
		principal: &auth.Principal{User: model.User{Username: "alice"}, Namespace: []model.NamespacePermission{
			{NamespaceID: &acme.ID, Resource: model.ResourceInventory, Action: model.ActionRead},
			{NamespaceID: &other.ID, Resource: model.ResourceInventory, Action: model.ActionRead},
		}}}
	f.logs = &bytes.Buffer{}
	f.logger = slog.New(slog.NewTextHandler(f.logs, nil))
	clk := mockClock.NewMockClock(ctrl)
	clk.EXPECT().Now().Return(now).AnyTimes()
	f.oauth.EXPECT().AuthenticateAccess(gomock.Any(), "lacis_at_good").Return(f.principal, time.Now().Add(time.Hour), nil).AnyTimes()
	f.ns.EXPECT().List(gomock.Any()).Return([]model.Namespace{other, hidden, acme}, nil).AnyTimes()
	cfg := config.DefaultConfig()
	cfg.Graph.MaxNodes = 3
	cfg.Listen.PublicURL = "https://lacis.acme.example/lacis/"
	server := mcpserver.New(mcpserver.Services{Namespace: f.ns, Graph: f.graph, History: f.history}, clk, f.logger, cfg, "1.2.3")
	listen := config.ListenConfig{BasePath: "/", PublicURL: "https://lacis.acme.example"}
	srv := httptest.NewServer(mcpRoute.Handler(appContext.TestContext(nil).Logger, listen, f.oauth, server))
	t.Cleanup(srv.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: srv.URL,
		HTTPClient: &http.Client{Transport: bearer{token: "lacis_at_good"}}, DisableStandaloneSSE: true}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	f.session = session
	return f
}

func (f *fixture) inventories() {
	f.ns.EXPECT().Inventory(gomock.Any(), acme.ID).Return(inventory(), nil).AnyTimes()
	f.ns.EXPECT().Inventory(gomock.Any(), other.ID).Return(&model.Inventory{}, nil).AnyTimes()
}

// call calls a tool and decodes its structured output into out; it
// returns the text of a tool error.
func (f *fixture) call(t *testing.T, name string, args map[string]any, out any) string {
	t.Helper()
	res, err := f.session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	if res.IsError {
		require.NotEmpty(t, res.Content)
		return res.Content[0].(*mcp.TextContent).Text
	}
	b, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, out))
	return ""
}

func TestServer_Unauthorized(t *testing.T) {
	f := newFixture(t)
	f.oauth.EXPECT().AuthenticateAccess(gomock.Any(), "lacis_at_bad").Return(nil, time.Time{}, service.ErrUnauthorized)
	srv := httptest.NewServer(mcpRoute.Handler(appContext.TestContext(nil).Logger, config.ListenConfig{PublicURL: "https://lacis.acme.example/lacis"}, f.oauth, mcp.NewServer(&mcp.Implementation{Name: "x"}, nil)))
	defer srv.Close()
	for _, auth := range []string{"", "Bearer lacis_at_bad"} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = res.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
		assert.Contains(t, res.Header.Get("WWW-Authenticate"), `resource_metadata="https://lacis.acme.example/lacis/.well-known/oauth-protected-resource"`)
	}
}

func TestServer_Tools(t *testing.T) {
	f := newFixture(t)
	res, err := f.session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		assert.True(t, tool.Annotations.ReadOnlyHint, tool.Name)
	}
	assert.Equal(t, []string{"find_by_version", "get_app", "get_host", "graph", "history", "links", "list_namespaces", "list_versions", "search"}, names)
	assert.Contains(t, f.session.InitializeResult().Instructions, "find_by_version")
}

func TestServer_Icons(t *testing.T) {
	f := newFixture(t)
	info := f.session.InitializeResult().ServerInfo
	assert.Equal(t, "Lacis", info.Title)
	assert.Equal(t, []mcp.Icon{
		{Source: "https://lacis.acme.example/lacis/icon-512.png", MIMEType: "image/png", Sizes: []string{"512x512"}},
		{Source: "https://lacis.acme.example/lacis/favicon.svg", MIMEType: "image/svg+xml", Sizes: []string{"any"}},
	}, info.Icons, "served by the front, under the base path")
}

func TestServer_ListNamespaces(t *testing.T) {
	f := newFixture(t)
	f.inventories()
	var out mcpserver.ListNamespacesOutput
	require.Empty(t, f.call(t, "list_namespaces", nil, &out))
	require.Len(t, out.Namespaces, 2, "hidden is not readable")
	assert.Equal(t, mcpserver.NamespaceOut{Slug: "acme", Name: "Acme", Projects: []string{"shop"}, Envs: []string{"staging", "production"}, Hosts: 3, Apps: 2}, out.Namespaces[0])
	assert.Equal(t, "other", out.Namespaces[1].Slug)
}

func TestServer_Search(t *testing.T) {
	f := newFixture(t)
	f.inventories()
	tests := []struct {
		name  string
		args  map[string]any
		want  []string
		trunc bool
		err   string
	}{
		{name: "hostname", args: map[string]any{"query": "WEB-"}, want: []string{"host web-1", "host web-2"}},
		{name: "IP", args: map[string]any{"query": "10.0.2.10"}, want: []string{"host db-1"}},
		{name: "vhost of an app", args: map[string]any{"query": "shop.acme"}, want: []string{"app shop-front"}},
		{name: "domain of a webserver", args: map[string]any{"query": "www.acme"}, want: []string{"webserver nginx"}},
		{name: "service", args: map[string]any{"query": "mysql", "namespace": "acme"}, want: []string{"service mysql"}},
		{name: "image of a container", args: map[string]any{"query": "redis"}, want: []string{"container cache"}},
		{name: "limit", args: map[string]any{"query": "web-", "limit": 1}, want: []string{"host web-1"}, trunc: true},
		{name: "namespace not readable", args: map[string]any{"query": "web", "namespace": "hidden"}, err: `unknown namespace "hidden"; the namespaces are: acme, other`},
		{name: "no query", args: map[string]any{"query": " "}, err: "the query is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out mcpserver.SearchOutput
			msg := f.call(t, "search", tt.args, &out)
			if tt.err != "" {
				assert.Contains(t, msg, tt.err)
				return
			}
			require.Empty(t, msg)
			var got []string
			for _, h := range out.Results {
				got = append(got, h.Kind+" "+h.Name)
				assert.Equal(t, "acme", h.Namespace)
			}
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.trunc, out.Truncated)
		})
	}
}

func TestServer_GetHost(t *testing.T) {
	f := newFixture(t)
	f.inventories()
	var out mcpserver.HostOut
	assert.Contains(t, f.call(t, "get_host", map[string]any{"host": "web-1"}, &out), "give the namespace, one of: acme, other")
	require.Empty(t, f.call(t, "get_host", map[string]any{"host": "WEB-1", "namespace": "acme"}, &out))
	assert.Equal(t, "web-1", out.Hostname)
	assert.Equal(t, "shop", out.Project)
	assert.Equal(t, "production", out.Env)
	assert.Equal(t, "front", out.Role)
	assert.Equal(t, map[string]any{"name": "debian", "version": "12"}, out.OS)
	assert.Equal(t, []mcpserver.Runtime{{Type: "php", Version: "8.1.2-1ubuntu2.18", Path: "/usr/bin/php8.1"}}, out.Runtimes)
	assert.Equal(t, []mcpserver.WebserverOut{{ID: nginx.ID.String(), Type: "nginx", Version: "1.24.0", Endpoints: []mcpserver.Endpoint{{Kind: "domain", Address: "www.acme.example"}}}}, out.Webservers)
	require.Len(t, out.Apps, 1)
	assert.Equal(t, "8.1.27", out.Apps[0].RuntimeVersion)

	assert.Contains(t, f.call(t, "get_host", map[string]any{"host": "nope", "namespace": "acme"}, &out), `no host "nope"`)
}

func TestServer_GetApp(t *testing.T) {
	f := newFixture(t)
	f.inventories()
	var out mcpserver.AppOut
	require.Empty(t, f.call(t, "get_app", map[string]any{"app": "shop-front", "namespace": "acme"}, &out))
	assert.Equal(t, "web-1", out.Host)
	assert.Equal(t, "symfony", out.Framework)
	assert.Equal(t, "shop", out.Project, "the place of its host")
	require.Len(t, out.Dependencies, 1)
	assert.Equal(t, mcpserver.Dependency{Key: "DATABASE_URL", Scheme: "mysql", Port: 3306, Target: map[string]any{"host": "db-1"}, Source: map[string]any{"file": ".env"}}, out.Dependencies[0])
	require.Len(t, out.Uses, 1)
	assert.Equal(t, mcpserver.End{Kind: "service", ID: mysql.ID.String(), Name: "mysql", Host: "db-1", Project: "shop", Env: "production"}, out.Uses[0].To)
	require.Len(t, out.UsedBy, 1)
	assert.Equal(t, "nginx", out.UsedBy[0].From.Name)

	require.Empty(t, f.call(t, "get_app", map[string]any{"app": "/srv/legacy", "host": "web-2", "namespace": "acme"}, &out))
	assert.Equal(t, "legacy", out.Name)
	assert.Contains(t, f.call(t, "get_app", map[string]any{"app": "legacy", "host": "web-1", "namespace": "acme"}, &out), `no app "legacy"`)
	assert.Contains(t, f.call(t, "get_app", map[string]any{"app": "legacy", "host": "nope", "namespace": "acme"}, &out), `no host "nope"`)
}

func TestServer_Links(t *testing.T) {
	f := newFixture(t)
	f.inventories()
	describe := func(out mcpserver.LinksOutput) []string {
		var got []string
		for _, l := range out.Links {
			got = append(got, l.From.Name+" -> "+l.To.Name)
		}
		return got
	}
	tests := []struct {
		name string
		args map[string]any
		want []string
		err  string
	}{
		{name: "who uses a host", args: map[string]any{"target": "db-1"}, want: []string{"shop-front -> mysql"}},
		{name: "what a host uses", args: map[string]any{"target": "web-1", "direction": "dependencies"}, want: []string{"shop-front -> mysql", "nginx -> shop-front"}},
		{name: "both, an app", args: map[string]any{"target": "shop-front", "direction": "both"}, want: []string{"shop-front -> mysql", "nginx -> shop-front"}},
		{name: "a service by id", args: map[string]any{"target": mysql.ID.String()}, want: []string{"shop-front -> mysql"}},
		{name: "an external target", args: map[string]any{"target": "legacy", "direction": "dependencies"}, want: []string{"legacy -> api.stripe.example"}},
		{name: "nobody uses it", args: map[string]any{"target": "legacy"}},
		{name: "unknown", args: map[string]any{"target": "nope"}, err: `no host, app, service or webserver "nope"`},
		{name: "bad direction", args: map[string]any{"target": "db-1", "direction": "up"}, err: "direction must be"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.args["namespace"] = "acme"
			var out mcpserver.LinksOutput
			msg := f.call(t, "links", tt.args, &out)
			if tt.err != "" {
				assert.Contains(t, msg, tt.err)
				return
			}
			require.Empty(t, msg)
			assert.Equal(t, tt.want, describe(out))
		})
	}
}

func TestServer_FindByVersion(t *testing.T) {
	f := newFixture(t)
	f.inventories()
	describe := func(out mcpserver.FindByVersionOutput) []string {
		var got []string
		for _, u := range out.Usages {
			got = append(got, u.Kind+" "+u.Version+" "+u.Host+" "+u.App)
		}
		return got
	}
	tests := []struct {
		name string
		args map[string]any
		want []string
		err  string
	}{
		{name: "every php", args: map[string]any{"technology": "PHP"}, want: []string{"app_runtime 7.4.33 web-2 legacy", "app_runtime 8.1.27 web-1 shop-front",
			"runtime 7.4.33 web-2 ", "runtime 8.1.2-1ubuntu2.18 web-1 "}},
		{name: "php below 8.1", args: map[string]any{"technology": "php", "op": "lt", "version": "8.1"}, want: []string{"app_runtime 7.4.33 web-2 legacy", "runtime 7.4.33 web-2 "}},
		{name: "php 8.1 exactly", args: map[string]any{"technology": "php", "op": "eq", "version": "8.1"}, want: []string{"app_runtime 8.1.27 web-1 shop-front", "runtime 8.1.2-1ubuntu2.18 web-1 "}},
		{name: "framework", args: map[string]any{"technology": "symfony", "version": "6"}, want: []string{"framework 6.4.12 web-1 shop-front"}},
		{name: "service", args: map[string]any{"technology": "mysql", "op": "lte", "version": "8.0"}, want: []string{"service 8.0.36 db-1 "}},
		{name: "image of a container", args: map[string]any{"technology": "redis"}, want: []string{"container 7.2.4 web-2 cache"}},
		{name: "nobody", args: map[string]any{"technology": "ruby"}},
		{name: "bad operator", args: map[string]any{"technology": "php", "op": "<"}, err: "op must be one of"},
		{name: "no technology", args: map[string]any{"technology": ""}, err: "the technology is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out mcpserver.FindByVersionOutput
			msg := f.call(t, "find_by_version", tt.args, &out)
			if tt.err != "" {
				assert.Contains(t, msg, tt.err)
				return
			}
			require.Empty(t, msg)
			assert.Equal(t, tt.want, describe(out))
			assert.Equal(t, len(tt.want), out.Count)
		})
	}

	var out mcpserver.FindByVersionOutput
	require.Empty(t, f.call(t, "find_by_version", map[string]any{"technology": "php", "limit": 1}, &out))
	assert.Equal(t, 4, out.Count)
	assert.Len(t, out.Usages, 1)
	assert.True(t, out.Truncated)
}

func TestServer_ListVersions(t *testing.T) {
	f := newFixture(t)
	f.inventories()
	var out mcpserver.ListVersionsOutput
	require.Empty(t, f.call(t, "list_versions", map[string]any{"technology": "php", "namespace": "acme"}, &out))
	assert.Equal(t, []mcpserver.TechnologyVersions{
		{Technology: "php", Kind: "app_runtime", Versions: []mcpserver.VersionCount{{Version: "7.4", Count: 1}, {Version: "8.1", Count: 1}}},
		{Technology: "php", Kind: "runtime", Versions: []mcpserver.VersionCount{{Version: "7.4", Count: 1}, {Version: "8.1", Count: 1}}},
	}, out.Technologies)
	require.Empty(t, f.call(t, "list_versions", nil, &out))
	assert.Len(t, out.Technologies, 7)
}

func TestServer_Graph(t *testing.T) {
	f := newFixture(t)
	g := &topology.Graph{Nodes: []topology.Node{{ID: "a", Label: "shop"}, {ID: "b", Label: "db"}}, Edges: []topology.Edge{{From: "a", To: "b"}}}
	f.graph.EXPECT().Graph(gomock.Any(), "acme", topology.Filter{Level: topology.LevelHost, Envs: []string{"production"}}).Return(g, nil)
	var out mcpserver.GraphOutput
	require.Empty(t, f.call(t, "graph", map[string]any{"namespace": "acme", "level": "host", "envs": []string{"production"}}, &out))
	assert.Equal(t, 2, out.Nodes)
	assert.Equal(t, 1, out.Edges)
	assert.Contains(t, out.Mermaid, "flowchart")

	big := &topology.Graph{Nodes: make([]topology.Node, 4)}
	f.graph.EXPECT().Graph(gomock.Any(), "acme", topology.Filter{Level: topology.LevelProject}).Return(big, nil)
	assert.Contains(t, f.call(t, "graph", map[string]any{"namespace": "acme"}, &out), "more than 3: filter it")
	assert.Contains(t, f.call(t, "graph", map[string]any{"namespace": "acme", "level": "planet"}, &out), "level must be")
	f.graph.EXPECT().Graph(gomock.Any(), "acme", gomock.Any()).Return(nil, errors.New("db down"))
	msg := f.call(t, "graph", map[string]any{"namespace": "acme"}, &out)
	assert.Equal(t, "internal error of Lacis", msg, "the error itself stays in the logs")
	assert.Contains(t, f.logs.String(), `msg="MCP tool failed" tool=graph user=alice`)
	assert.Contains(t, f.logs.String(), "error=\"db down\"")
	assert.Contains(t, f.logs.String(), `msg="MCP tool call" tool=graph user=alice`)
	assert.Contains(t, f.logs.String(), `msg="MCP tool call refused" tool=graph user=alice`)
}

func TestServer_History(t *testing.T) {
	f := newFixture(t)
	id := uuid.New()
	rows := []repository.ChangeEventRow{
		{ChangeEvent: model.ChangeEvent{EntityType: "runtime", EntityID: &id, Kind: "changed", Before: json.RawMessage(`{"version":"8.1"}`), After: json.RawMessage(`{"version":"8.2"}`), OccurredAt: now}, Hostname: new("web-1")},
		{ChangeEvent: model.ChangeEvent{EntityType: "host", Kind: "added", OccurredAt: now.Add(-time.Hour)}},
	}
	f.history.EXPECT().List(gomock.Any(), "acme", service.HistoryFilter{Hostname: "web-1", Since: now.Add(-7 * 24 * time.Hour), Limit: 2}).Return(rows, nil)
	var out mcpserver.HistoryOutput
	require.Empty(t, f.call(t, "history", map[string]any{"namespace": "acme", "host": "web-1", "since": "7d", "limit": 1}, &out))
	assert.True(t, out.HasMore)
	require.Len(t, out.Changes, 1)
	assert.Equal(t, "web-1", out.Changes[0].Host)
	assert.Equal(t, map[string]any{"version": "8.2"}, out.Changes[0].After)

	f.history.EXPECT().List(gomock.Any(), "acme", service.HistoryFilter{Since: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Limit: 51}).Return(nil, nil)
	require.Empty(t, f.call(t, "history", map[string]any{"namespace": "acme", "since": "2026-10-01"}, &out))
	assert.Empty(t, out.Changes)
	assert.Contains(t, f.call(t, "history", map[string]any{"namespace": "acme", "since": "last week"}, &out), "a date (2026-10-01) or a duration")
}

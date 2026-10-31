package topology

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/alexandreh2ag/lacis/linker"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inventory: a prod app on web-prod using its database and S3, a staging
// app on web-staging using the prod database, and the proxy in front of
// both.
type ids struct {
	webProd, db, webStaging, rp       uuid.UUID
	appProd, appStaging, pg, ws, trfk uuid.UUID
}

func ptr[T any](v T) *T { return &v }

func inventory(t *testing.T) (*model.Inventory, ids) {
	i := ids{webProd: uuid.New(), db: uuid.New(), webStaging: uuid.New(), rp: uuid.New(),
		appProd: uuid.New(), appStaging: uuid.New(), pg: uuid.New(), ws: uuid.New(), trfk: uuid.New()}
	shop, infra := uuid.New(), uuid.New()
	prod, staging := uuid.New(), uuid.New()
	detail := func(d linker.Detail) json.RawMessage {
		b, err := json.Marshal(d)
		require.NoError(t, err)
		return b
	}
	link := func(fromType string, from uuid.UUID, toType string, to *uuid.UUID, external *string, source, confidence string, d linker.Detail) model.Link {
		return model.Link{FromType: fromType, FromID: from, ToType: toType, ToID: to, ExternalTarget: external,
			Source: source, Confidence: confidence, Detail: detail(d)}
	}
	inv := &model.Inventory{
		Namespace:    model.Namespace{Slug: "acme"},
		Projects:     []model.Project{{Base: model.Base{ID: shop}, Slug: "shop"}, {Base: model.Base{ID: infra}, Slug: "infra"}},
		Environments: []model.Environment{{Base: model.Base{ID: prod}, Slug: "production"}, {Base: model.Base{ID: staging}, Slug: "staging"}},
		Hosts: []model.Host{
			{Base: model.Base{ID: i.webProd}, Hostname: "web-prod", ProjectID: &shop, EnvironmentID: &prod},
			{Base: model.Base{ID: i.db}, Hostname: "db", ProjectID: &infra, EnvironmentID: &prod},
			{Base: model.Base{ID: i.webStaging}, Hostname: "web-staging", ProjectID: &shop, EnvironmentID: &staging},
			{Base: model.Base{ID: i.rp}, Hostname: "rp", ProjectID: &infra, EnvironmentID: &prod},
			{Base: model.Base{ID: uuid.New()}, Hostname: "idle", EnvironmentID: &staging},
		},
		Apps: []model.App{
			{Base: model.Base{ID: i.appProd}, HostID: i.webProd, Name: "api", Role: "web", Framework: ptr("symfony"), FrameworkVersion: ptr("7.2"),
				Runtime: json.RawMessage(`{"type": "php", "version": "8.3.14"}`), Code: json.RawMessage(`{"version": "v2.5.0", "commit": "a81c3e7"}`),
				Vhosts: model.StringArray{"api.example"}},
			// Its own env label wins over the one of its host.
			{Base: model.Base{ID: i.appStaging}, HostID: i.webStaging, Name: "api", Role: "worker", EnvironmentID: &staging},
		},
		Services:   []model.Service{{Base: model.Base{ID: i.pg}, HostID: i.db, Type: "postgresql", Instance: "16/main", Version: ptr("16.4")}},
		Webservers: []model.Webserver{{Base: model.Base{ID: i.ws}, HostID: i.webProd, Type: "nginx"}, {Base: model.Base{ID: i.trfk}, HostID: i.rp, Type: "traefik"}},
	}
	inv.Links = []model.Link{
		link(model.LinkNodeApp, i.appProd, model.LinkNodeService, &i.pg, nil, model.LinkSourceEnv, model.LinkConfidenceExact, linker.Detail{Rule: linker.RulePrivateIP, Key: "DATABASE_URL"}),
		link(model.LinkNodeApp, i.appProd, model.LinkNodeService, &i.pg, nil, model.LinkSourceEnv, model.LinkConfidenceInferred, linker.Detail{Rule: linker.RuleHostname, Key: "READ_URL"}),
		link(model.LinkNodeApp, i.appProd, model.LinkNodeExternal, nil, ptr("s3.example"), model.LinkSourceEnv, model.LinkConfidenceExact, linker.Detail{Rule: linker.RuleExternal, Reason: linker.ReasonPublic, Key: "S3_URL"}),
		link(model.LinkNodeApp, i.appStaging, model.LinkNodeService, &i.pg, nil, model.LinkSourceEnv, model.LinkConfidenceExact, linker.Detail{Rule: linker.RulePrivateIP, Key: "DATABASE_URL"}),
		link(model.LinkNodeWebserver, i.ws, model.LinkNodeApp, &i.appProd, nil, model.LinkSourceWebserver, model.LinkConfidenceExact, linker.Detail{Rule: linker.RuleVhost, Domains: []string{"api.example"}}),
		link(model.LinkNodeWebserver, i.trfk, model.LinkNodeWebserver, &i.ws, nil, model.LinkSourceTraefik, model.LinkConfidenceExact,
			linker.Detail{Rule: linker.RulePrivateIP, Domains: []string{"a.example", "b.example", "c.example", "d.example"}}),
		// A link to a node no longer in the inventory is left out.
		link(model.LinkNodeApp, i.appProd, model.LinkNodeService, ptr(uuid.New()), nil, model.LinkSourceEnv, model.LinkConfidenceExact, linker.Detail{}),
	}
	return inv, i
}

func nodeIDs(g *Graph) []string {
	var out []string
	for _, n := range g.Nodes {
		mark := ""
		if !n.Selected {
			mark = " (neighbor)"
		}
		out = append(out, n.Kind+" "+n.Host+" "+n.Label+mark)
	}
	return out
}

func TestBuild_Service(t *testing.T) {
	inv, i := inventory(t)
	g, err := Build(inv, Filter{})
	require.NoError(t, err)
	assert.Equal(t, "acme", g.Namespace)
	assert.Equal(t, LevelService, g.Level)
	assert.Equal(t, []string{
		"external  s3.example",
		"service db postgresql 16/main",
		"webserver rp traefik",
		"webserver web-prod nginx",
		"app web-prod api",
		"app web-staging api",
	}, nodeIDs(g), "sorted by host and kind; the idle host has no node")

	assert.Equal(t, Node{ID: "app:" + i.appProd.String(), Kind: model.LinkNodeApp, Label: "api", Detail: "symfony 7.2", Version: "v2.5.0",
		Techs: []string{"symfony", "php"}, Host: "web-prod", Project: "shop", Env: "production", Entity: &i.appProd,
		HostIDs: []uuid.UUID{i.webProd}, Selected: true}, g.Nodes[4])
	assert.Equal(t, "worker", g.Nodes[5].Detail)
	assert.Equal(t, "staging", g.Nodes[5].Env)
	assert.Equal(t, "public_ip", g.Nodes[0].Detail)

	pg := "service:" + i.pg.String()
	assert.Contains(t, g.Edges, Edge{From: "app:" + i.appProd.String(), To: pg, Source: model.LinkSourceEnv,
		Confidence: model.LinkConfidenceInferred, Rule: "mixed", Labels: []string{"DATABASE_URL", "READ_URL"}, Count: 2},
		"two links merge, the weakest confidence wins")
	assert.Len(t, g.Edges, 5)

	require.Len(t, g.Groups, 4, "the external target is in no group")
	assert.Equal(t, Group{ID: "host:" + i.webProd.String(), Label: "web-prod", Project: "shop", Env: "production",
		Nodes: []string{"webserver:" + i.ws.String(), "app:" + i.appProd.String()}}, g.Groups[2])
}

func TestBuild_Filter(t *testing.T) {
	inv, _ := inventory(t)
	g, err := Build(inv, Filter{Envs: []string{"staging"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"service db postgresql 16/main (neighbor)", "app web-staging api"}, nodeIDs(g),
		"the prod database of the staging app shows")
	assert.Len(t, g.Edges, 1)

	g, err = Build(inv, Filter{Projects: []string{"infra"}, Envs: []string{"production"}})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"service db postgresql 16/main",
		"webserver rp traefik",
		"webserver web-prod nginx (neighbor)",
		"app web-prod api (neighbor)",
		"app web-staging api (neighbor)",
	}, nodeIDs(g))

	g, err = Build(inv, Filter{Projects: []string{"none"}})
	require.NoError(t, err)
	assert.Empty(t, g.Nodes)
	assert.Empty(t, g.Edges)
}

func TestBuild_Host(t *testing.T) {
	inv, i := inventory(t)
	g, err := Build(inv, Filter{Level: LevelHost, Envs: []string{"staging"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"host db db (neighbor)", "host idle idle", "host web-staging web-staging"}, nodeIDs(g),
		"a selected host without content shows")
	assert.Empty(t, g.Groups)

	g, err = Build(inv, Filter{Level: LevelHost})
	require.NoError(t, err)
	webProd := "host:" + i.webProd.String()
	var fromWebProd []Edge
	for _, e := range g.Edges {
		if e.From == webProd {
			fromWebProd = append(fromWebProd, e)
		}
	}
	assert.Equal(t, []Edge{
		{From: webProd, To: "externals:production", Source: model.LinkSourceEnv, Confidence: model.LinkConfidenceExact,
			Rule: linker.RuleExternal, Labels: []string{"S3_URL"}, Count: 1},
		{From: webProd, To: "host:" + i.db.String(), Source: model.LinkSourceEnv, Confidence: model.LinkConfidenceInferred,
			Rule: "mixed", Labels: []string{"DATABASE_URL", "READ_URL"}, Count: 2},
	}, fromWebProd, "the nginx → api link inside the host is gone")

	_, err = Build(inv, Filter{Level: "rack"})
	assert.EqualError(t, err, `unknown level "rack"`)
}

func TestBuild_Project(t *testing.T) {
	inv, i := inventory(t)
	g, err := Build(inv, Filter{Level: LevelProject})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"project  ",
		"project  infra",
		"project  shop",
		"project  shop",
		"external  externals",
	}, nodeIDs(g), "the idle host without project shows too")
	byID := map[string]Node{}
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	infra := byID["project:infra@production"]
	assert.Equal(t, RoleProxy, infra.Role, "traefik routes to another host")
	assert.Equal(t, []string{"db", "rp"}, infra.Members)
	assert.Equal(t, 2, infra.Count)
	assert.Equal(t, []string{"traefik", "postgresql"}, infra.Techs)
	assert.ElementsMatch(t, []uuid.UUID{i.db, i.rp}, infra.HostIDs)
	shop := byID["project:shop@production"]
	assert.Equal(t, RoleApp, shop.Role)
	assert.Equal(t, 1, shop.Apps)
	assert.Equal(t, []string{"symfony", "nginx", "php"}, shop.Techs)
	assert.Equal(t, Node{ID: "externals:production", Kind: model.LinkNodeExternal, Label: "externals", Env: "production",
		Members: []string{"s3.example"}, Count: 1, Selected: true}, byID["externals:production"])

	edges := map[string]Edge{}
	for _, e := range g.Edges {
		edges[e.From+" → "+e.To] = e
	}
	assert.Len(t, edges, 4)
	staging := edges["project:shop@staging → project:infra@production"]
	assert.True(t, staging.CrossEnv, "the staging app uses the prod database")
	assert.Equal(t, []string{"DATABASE_URL"}, staging.Labels)
	prod := edges["project:shop@production → project:infra@production"]
	assert.False(t, prod.CrossEnv)
	assert.Equal(t, 2, prod.Count)
	assert.Equal(t, model.LinkConfidenceInferred, prod.Confidence)
	assert.Contains(t, edges, "project:infra@production → project:shop@production")
	assert.Contains(t, edges, "project:shop@production → externals:production")
}

// stackInventory adds web-prod-2, a copy of web-prod running an older api.
func stackInventory(t *testing.T) (*model.Inventory, ids, uuid.UUID) {
	inv, i := inventory(t)
	web2, api2, nginx2 := uuid.New(), uuid.New(), uuid.New()
	shop, prod := inv.Hosts[0].ProjectID, inv.Hosts[0].EnvironmentID
	inv.Hosts = append(inv.Hosts, model.Host{Base: model.Base{ID: web2}, Hostname: "web-prod-2", ProjectID: shop, EnvironmentID: prod})
	inv.Apps = append(inv.Apps, model.App{Base: model.Base{ID: api2}, HostID: web2, Name: "api", Role: "web", Framework: ptr("symfony"),
		Code: json.RawMessage(`{"version": "v2.4.0", "commit": "3f2a9c1"}`)})
	inv.Webservers = append(inv.Webservers, model.Webserver{Base: model.Base{ID: nginx2}, HostID: web2, Type: "nginx"})
	inv.Links = append(inv.Links,
		model.Link{FromType: model.LinkNodeApp, FromID: api2, ToType: model.LinkNodeService, ToID: &i.pg, Source: model.LinkSourceEnv,
			Confidence: model.LinkConfidenceExact, Detail: json.RawMessage(`{"rule": "private_ip", "key": "DATABASE_URL"}`)},
		model.Link{FromType: model.LinkNodeWebserver, FromID: nginx2, ToType: model.LinkNodeApp, ToID: &api2, Source: model.LinkSourceWebserver,
			Confidence: model.LinkConfidenceExact, Detail: json.RawMessage(`{"rule": "vhost"}`)})
	return inv, i, web2
}

func TestBuild_Stack(t *testing.T) {
	inv, i, web2 := stackInventory(t)
	g, err := Build(inv, Filter{Level: LevelHost, Envs: []string{"production"}})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"host  web-prod*",
		"external  externals (neighbor)",
		"host db db",
		"host rp rp",
		"host web-staging web-staging (neighbor)",
	}, nodeIDs(g), "a stack is on no single host")
	stack := g.Nodes[0]
	assert.Equal(t, "stack:"+i.webProd.String(), stack.ID)
	assert.Equal(t, []string{"web-prod", "web-prod-2"}, stack.Members)
	assert.Equal(t, []uuid.UUID{i.webProd, web2}, stack.HostIDs)
	assert.Equal(t, 2, stack.Apps)
	assert.Equal(t, []Divergence{{App: "api", Hosts: []Deployment{
		{Host: "web-prod", Version: "v2.5.0", Commit: "a81c3e7"},
		{Host: "web-prod-2", Version: "v2.4.0", Commit: "3f2a9c1"},
	}}}, stack.Divergences)
	for _, e := range g.Edges {
		if e.From == stack.ID && e.To == "host:"+i.db.String() {
			assert.Equal(t, 3, e.Count, "the links of both hosts merge")
		}
	}

	// A host named by the filter leaves its stack.
	g, err = Build(inv, Filter{Level: LevelHost, Hosts: []string{"web-prod"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"external  externals (neighbor)", "host db db (neighbor)", "host rp rp (neighbor)", "host web-prod web-prod"}, nodeIDs(g))
}

func TestBuild_App(t *testing.T) {
	inv, i := inventory(t)
	fpm := uuid.New()
	inv.Services = append(inv.Services, model.Service{Base: model.Base{ID: fpm}, HostID: i.webProd, Type: "php-fpm", Instance: "8.3"})
	inv.Links = append(inv.Links, model.Link{FromType: model.LinkNodeWebserver, FromID: i.ws, ToType: model.LinkNodeService, ToID: &fpm,
		Source: model.LinkSourceWebserver, Confidence: model.LinkConfidenceExact, Detail: json.RawMessage(`{"rule": "socket"}`)})
	// A node front served by the same nginx does not run php-fpm.
	front := uuid.New()
	inv.Apps = append(inv.Apps, model.App{Base: model.Base{ID: front}, HostID: i.webProd, Name: "front", Role: "web",
		Runtime: json.RawMessage(`{"type": "node"}`)})
	inv.Links = append(inv.Links, model.Link{FromType: model.LinkNodeWebserver, FromID: i.ws, ToType: model.LinkNodeApp, ToID: &front,
		Source: model.LinkSourceWebserver, Confidence: model.LinkConfidenceExact, Detail: json.RawMessage(`{"rule": "vhost"}`)})
	g, err := Build(inv, Filter{Level: LevelApp, Projects: []string{"shop"}, Envs: []string{"production"}})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"external  s3.example (neighbor)",
		"service db postgresql 16/main (neighbor)",
		"webserver rp traefik (neighbor)",
		"app web-prod api",
		"app web-prod front",
	}, nodeIDs(g), "nginx and php-fpm are folded into the apps")
	assert.Equal(t, []string{"symfony", "php", "nginx", "php-fpm"}, g.Nodes[3].Techs)
	assert.Equal(t, []string{"node", "nginx"}, g.Nodes[4].Techs)
	api := "app:" + i.appProd.String()
	var routed *Edge
	for k, e := range g.Edges {
		if e.To == api && e.From == "webserver:"+i.trfk.String() {
			routed = &g.Edges[k]
		}
	}
	require.NotNil(t, routed, "traefik → nginx is drawn to the app nginx serves")
	assert.Equal(t, []string{"a.example", "b.example", "c.example", "d.example"}, routed.Labels, "none of them is a vhost of the app")
	assert.Len(t, g.Edges, 4, "traefik → api and front, api → s3, api → the database; nginx → its apps is inside")
	require.Len(t, g.Groups, 3)
	assert.Equal(t, []string{api, "app:" + front.String()}, g.Groups[2].Nodes)
}

func TestRender(t *testing.T) {
	inv, _ := inventory(t)
	g, err := Build(inv, Filter{})
	require.NoError(t, err)

	var b bytes.Buffer
	require.NoError(t, Render(&b, g, FormatMermaid, Options{Labels: true}))
	out := b.String()
	assert.True(t, strings.HasPrefix(out, "flowchart LR\n"), "dagre needs no config")
	assert.Contains(t, out, `  subgraph g2["web-prod (shop · production)"]`+"\n")
	assert.Contains(t, out, `    n4("api<br/>symfony 7.2"):::app`+"\n")
	assert.NotContains(t, out, `subgraph g0`, "a host holding one node is no box")
	assert.Contains(t, out, `  n1[("postgresql 16/main<br/>16.4<br/>@ db")]:::service`+"\n", "its node names it")
	assert.Contains(t, out, `  n0>"s3.example<br/>public_ip"]:::external`+"\n")
	assert.Contains(t, out, `  n4 -.->|"DATABASE_URL, READ_URL"| n1`+"\n", "a link not exact is dashed")
	assert.Contains(t, out, `  n2 -->|"a.example, b.example, c.example +1"| n3`+"\n")
	assert.Contains(t, out, "  linkStyle 0 stroke:#2563eb,color:#2563eb\n")
	assert.Contains(t, out, "  linkStyle 2,3 stroke:#16a34a,color:#16a34a\n", "the edges leaving a node share its color")

	b.Reset()
	require.NoError(t, Render(&b, g, FormatMermaid, Options{Layout: LayoutELK}))
	out = b.String()
	assert.True(t, strings.HasPrefix(out, "---\nconfig:\n  layout: elk\n---\nflowchart LR\n"))
	assert.Contains(t, out, "  n4 -.-> n1\n", "no label by default")

	b.Reset()
	require.NoError(t, Render(&b, g, FormatDOT, Options{Labels: true}))
	out = b.String()
	assert.Contains(t, out, "digraph \"acme\" {\n")
	assert.Contains(t, out, "  subgraph cluster_g2 {\n    label=\"web-prod (shop · production)\";\n")
	assert.NotContains(t, out, "cluster_g0")
	assert.Contains(t, out, `  n0 [label="s3.example\npublic_ip", shape=cds`)
	assert.Contains(t, out, `  n1 [label="postgresql 16/main\n16.4\n@ db", shape=cylinder`)
	assert.Contains(t, out, `  n4 -> n1 [color="#16a34a", fontcolor="#16a34a", label="DATABASE_URL, READ_URL", style=dashed];`+"\n")

	b.Reset()
	require.NoError(t, Render(&b, g, FormatDOT, Options{}))
	assert.Contains(t, b.String(), `  n4 -> n1 [color="#16a34a", style=dashed];`+"\n")

	b.Reset()
	require.NoError(t, Render(&b, g, FormatJSON, Options{}))
	var back Graph
	require.NoError(t, json.Unmarshal(b.Bytes(), &back))
	assert.Equal(t, *g, back)

	assert.EqualError(t, Render(&b, g, "png", Options{}), `unknown format "png"`)
}

func TestRender_NoGroupNoLabel(t *testing.T) {
	g := &Graph{Namespace: "acme", Level: LevelHost,
		Nodes: []Node{{ID: "host:a", Kind: model.LinkNodeHost, Label: `a"b`}, {ID: "host:b", Kind: model.LinkNodeHost, Label: "b"}},
		Edges: []Edge{{From: "host:a", To: "host:b", Confidence: model.LinkConfidenceExact}}}
	var b bytes.Buffer
	require.NoError(t, Render(&b, g, FormatMermaid, Options{Labels: true}))
	assert.Contains(t, b.String(), `  n0["a#quot;b"]:::host`+"\n")
	assert.Contains(t, b.String(), "  n0 --> n1\n")
	b.Reset()
	require.NoError(t, Render(&b, g, FormatDOT, Options{Labels: true}))
	assert.Contains(t, b.String(), `  n0 [label="a\"b", shape=box`)
	assert.Contains(t, b.String(), "  n0 -> n1 [color=\"#2563eb\"];\n")
	assert.Equal(t, "plain", groupText(Group{Label: "plain"}))
}

func TestBuild_Expand(t *testing.T) {
	inv, i, web2 := stackInventory(t)
	inv.Runtimes = []model.Runtime{
		{Base: model.Base{ID: uuid.New()}, HostID: i.webProd, Type: "php", Version: "8.3.14", Path: "/usr/bin/php8.3"},
		{Base: model.Base{ID: uuid.New()}, HostID: web2, Type: "php", Version: "8.3.14", Path: "/usr/bin/php8.3"},
		{Base: model.Base{ID: uuid.New()}, HostID: i.db, Type: "python", Version: "3.11.2"},
	}
	zone := "project:shop@production"
	g, err := Build(inv, Filter{Level: LevelProject, Envs: []string{"production"}, Expand: []string{zone}})
	require.NoError(t, err)
	byID := map[string]Node{}
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	assert.True(t, byID[zone].Expanded)
	assert.False(t, byID["project:infra@production"].Expanded)
	assert.NotContains(t, byID, "item:project:infra@production|runtime|python 3.11.2", "the runtimes of a closed zone stay out")

	api := byID[zone+"|app|api"]
	require.Empty(t, api.ID, "an item is named item:<zone>|<kind>|<label>")
	api = byID["item:"+zone+"|app|api"]
	assert.Equal(t, model.LinkNodeApp, api.Kind)
	assert.Equal(t, 2, api.Count, "the api of both hosts is one item")
	assert.Equal(t, []string{"web-prod", "web-prod-2"}, api.Members)
	assert.Empty(t, api.Host, "on no single host")
	assert.Equal(t, &i.appProd, api.Entity, "it stands for its first deployment")
	assert.Equal(t, []Divergence{{App: "api", Hosts: []Deployment{
		{Host: "web-prod", Version: "v2.5.0", Commit: "a81c3e7"},
		{Host: "web-prod-2", Version: "v2.4.0", Commit: "3f2a9c1"},
	}}}, api.Divergences)
	assert.Equal(t, "v2.5.0 / v2.4.0", api.Version)
	php := byID["item:"+zone+"|runtime|php 8.3.14"]
	assert.Equal(t, KindRuntime, php.Kind)
	assert.Equal(t, 2, php.Count)
	assert.Equal(t, "/usr/bin/php8.3", php.Detail)

	require.Len(t, g.Groups, 1)
	assert.Equal(t, zone, g.Groups[0].ID)
	assert.ElementsMatch(t, []string{"item:" + zone + "|app|api", "item:" + zone + "|webserver|nginx", "item:" + zone + "|runtime|php 8.3.14"},
		g.Groups[0].Nodes)

	edges := map[string]Edge{}
	for _, e := range g.Edges {
		edges[e.From+" → "+e.To] = e
	}
	db := edges["item:"+zone+"|app|api → project:infra@production"]
	assert.Equal(t, 3, db.Count, "the links of both apis go out of the zone from the item")
	assert.Contains(t, edges, "project:infra@production → item:"+zone+"|webserver|nginx", "traefik reaches the nginx of the zone")
	assert.Equal(t, 2, edges["item:"+zone+"|webserver|nginx → item:"+zone+"|app|api"].Count, "inside the zone too")
	assert.Contains(t, edges, "item:"+zone+"|app|api → externals:production")
	for key := range edges {
		assert.NotContains(t, key, "→ "+zone, "nothing points at the opened zone itself")
	}

	// At the level of the hosts, a stack opens; a single host too.
	stack := "stack:" + i.webProd.String()
	g, err = Build(inv, Filter{Level: LevelHost, Envs: []string{"production"}, Expand: []string{stack, "host:" + i.db.String()}})
	require.NoError(t, err)
	var groups []string
	for _, gr := range g.Groups {
		groups = append(groups, gr.ID+": "+strings.Join(gr.Nodes, ", "))
	}
	assert.Equal(t, []string{
		stack + ": item:" + stack + "|webserver|nginx, item:" + stack + "|app|api, item:" + stack + "|runtime|php 8.3.14",
		"host:" + i.db.String() + ": item:host:" + i.db.String() + "|service|postgresql 16/main, item:host:" + i.db.String() + "|runtime|python 3.11.2",
	}, groups)

	// Other levels ignore it.
	g, err = Build(inv, Filter{Level: LevelApp, Expand: []string{zone}})
	require.NoError(t, err)
	for _, n := range g.Nodes {
		assert.NotEqual(t, KindRuntime, n.Kind)
		assert.False(t, strings.HasPrefix(n.ID, "item:"))
	}
}

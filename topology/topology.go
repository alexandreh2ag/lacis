// Package topology builds the graph of a namespace from its inventory and
// its links: a neutral model of nodes, edges and host groups, filtered by
// project, env and host, at one of four levels. The renderers draw it; the
// API serves it as is, so that no client recomposes the graph.
package topology

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/linker"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
)

// Level is the grain of the graph.
type Level string

const (
	// LevelService draws every app, service and webserver, grouped by host.
	LevelService Level = "service"
	// LevelHost draws hosts, the links of their content merged; the
	// similar hosts are stacked and the external targets of an env merged.
	LevelHost Level = "host"
	// LevelProject draws a node per project and env, the entry of the UI;
	// the external targets of an env are merged.
	LevelProject Level = "project"
	// LevelApp draws apps and services grouped by host, the webserver of a
	// host and the services it reaches locally folded into the apps it
	// serves; only the proxies routing to other hosts stay nodes.
	LevelApp Level = "app"
)

// Levels are the levels, the default first.
var Levels = []Level{LevelService, LevelHost, LevelProject, LevelApp}

// NodeKind is the kind of a node: an end of a link, a project or a
// runtime.
type NodeKind string

const (
	KindApp       NodeKind = model.LinkNodeApp
	KindService   NodeKind = model.LinkNodeService
	KindWebserver NodeKind = model.LinkNodeWebserver
	KindHost      NodeKind = model.LinkNodeHost
	KindExternal  NodeKind = model.LinkNodeExternal
	// KindProject is the node of a project in an env, at the level of the
	// projects.
	KindProject NodeKind = "project"
	// KindRuntime is a runtime of a host, drawn only in an expanded zone.
	KindRuntime NodeKind = "runtime"
)

// Confidence is how sure a link is.
type Confidence string

const (
	ConfidenceExact     Confidence = model.LinkConfidenceExact
	ConfidenceInferred  Confidence = model.LinkConfidenceInferred
	ConfidenceAmbiguous Confidence = model.LinkConfidenceAmbiguous
)

// Source is what a link was read from: env, traefik, webserver…
type Source string

// SourceMixed is an edge merging links of several sources.
const SourceMixed Source = "mixed"

// Roles tell what a project, a host or a stack mostly holds, and what a
// webserver does: route to backends or serve files and FastCGI apps.
const (
	RoleApp     = "app"
	RoleProxy   = "proxy"
	RoleService = "service"
	RoleWeb     = "web"
)

// proxyTypes are the webservers that only route.
var proxyTypes = map[string]bool{"traefik": true, "haproxy": true, "caddy": true}

// Filter selects the nodes of a graph by the slugs of their project and
// env and by hostname; empty selects all. The nodes linked to a selected
// node are kept, so that a dependency on another env or outside the
// inventory shows.
type Filter struct {
	Level    Level
	Projects []string
	Envs     []string
	// Hosts are hostnames; a host named is never stacked.
	Hosts []string
	// Confidences keeps the links of these confidences; empty keeps all.
	Confidences []Confidence
	// NoExternals leaves the external targets out.
	NoExternals bool
	// Isolate leaves out, when envs are selected, the links a webserver of
	// these envs has with a node of another env: a proxy shared by several
	// envs no longer brings the others. A direct link between envs stays.
	Isolate bool
	// Hidden leaves a host and its content out, such as an inactive host.
	Hidden func(*model.Host) bool `json:"-"`
	// Expand names project, host or stack nodes, at the levels of the
	// projects and the hosts, to draw with their content: the apps,
	// services, webservers and runtimes of their hosts, those of the same
	// kind and name merged.
	Expand []string
}

type Node struct {
	// ID is "<kind>:<uuid>", "external:<target>", "project:<slug>@<env>",
	// "stack:<uuid of its first host>", "externals:<env>", or
	// "item:<zone>|<kind>|<label>" for the content of an expanded zone.
	ID    string   `json:"id"`
	Kind  NodeKind `json:"kind"`
	Label string   `json:"label"`
	// Detail says more: the version of a service, the framework of an
	// app, the reason of an external target, the versions of a stack.
	Detail  string `json:"detail,omitempty"`
	Host    string `json:"host,omitempty"`
	Project string `json:"project,omitempty"`
	Env     string `json:"env,omitempty"`
	// Role is what a project, a host or a stack holds: app, proxy or
	// service; what a webserver does: proxy or web.
	Role string `json:"role,omitempty"`
	// Version is the code deployed of an app: a tag or a branch.
	Version string `json:"version,omitempty"`
	// Techs are the technologies of the node, the main one first: the
	// framework of an app then what it folds, the type of a service.
	Techs []string `json:"techs,omitempty"`
	// Count is the number of hosts of a project or a stack, of targets of
	// merged externals.
	Count int `json:"count,omitempty"`
	// Apps is the number of apps of a project, a host or a stack.
	Apps int `json:"apps,omitempty"`
	// Members are the hostnames of a project or a stack, the targets of
	// merged externals.
	Members []string `json:"members,omitempty"`
	// Entity is the app, service, webserver or host the node is; HostIDs
	// the hosts it stands for.
	Entity  *uuid.UUID  `json:"entity,omitempty"`
	HostIDs []uuid.UUID `json:"host_ids,omitempty"`
	// Divergences are the apps of a stack deployed in several versions.
	Divergences []Divergence `json:"divergences,omitempty"`
	// Selected is false for a node kept only for being linked to a
	// selected one.
	Selected bool `json:"selected"`
	// Expanded is a zone drawn with its content, the group of the same id.
	Expanded bool `json:"expanded,omitempty"`
}

// Divergence is an app of a stack whose hosts do not run the same code.
type Divergence struct {
	App   string       `json:"app"`
	Hosts []Deployment `json:"hosts"`
}

type Deployment struct {
	Host    string `json:"host"`
	Version string `json:"version,omitempty"`
	Commit  string `json:"commit,omitempty"`
}

type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Source is env, traefik, webserver…, or mixed when the edge merges
	// links of several sources.
	Source     Source     `json:"source"`
	Confidence Confidence `json:"confidence"`
	Rule       string     `json:"rule"`
	// Labels are the dependency keys of an app, or the domains a webserver
	// routes.
	Labels []string `json:"labels,omitempty"`
	// Count is the number of links the edge merges.
	Count int `json:"count"`
	// CrossEnv is an edge between two envs.
	CrossEnv bool `json:"cross_env,omitempty"`
}

// Group gathers the nodes of a host, at the levels of the services and of
// the apps, or the content of an expanded zone.
type Group struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Project string   `json:"project,omitempty"`
	Env     string   `json:"env,omitempty"`
	Nodes   []string `json:"nodes"`
}

type Graph struct {
	Namespace string  `json:"namespace"`
	Level     Level   `json:"level"`
	Nodes     []Node  `json:"nodes"`
	Edges     []Edge  `json:"edges"`
	Groups    []Group `json:"groups,omitempty"`
}

// Build draws the graph of the inventory.
func Build(inv *model.Inventory, f Filter) (*Graph, error) {
	b, err := prepare(inv, f)
	if err != nil {
		return nil, err
	}
	return b.draw(), nil
}

// prepare indexes the inventory for a filter: its nodes, those folded or
// stacked at the level, and the links the filter keeps.
func prepare(inv *model.Inventory, f Filter) (*builder, error) {
	f.Level = cmp.Or(f.Level, LevelService)
	if !slices.Contains(Levels, f.Level) {
		return nil, fmt.Errorf("unknown level %q", f.Level)
	}
	b := newBuilder(inv, f)
	b.nodes()
	b.fold()
	b.stack()
	b.visible = b.visibleLinks()
	return b, nil
}

// draw draws the graph of the level from the visible links.
func (b *builder) draw() *Graph {
	f := b.f
	// selected are the nodes the filter selects, hosts included.
	selected := map[string]bool{}
	for id, n := range b.all {
		if matches(f.Projects, n.Project) && matches(f.Envs, n.Env) && matches(f.Hosts, n.Host) {
			selected[id] = true
		}
	}
	g := &Graph{Namespace: b.inv.Namespace.Slug, Level: f.Level}
	members := map[string]map[string]bool{}
	keep := func(view, raw string) {
		if members[view] == nil {
			members[view] = map[string]bool{}
		}
		members[view][raw] = true
	}
	edges := map[string]*Edge{}
	var order []string
	for _, l := range b.visible {
		// Inside a host, a folded webserver reaches what it is drawn with.
		if (!selected[l.from] && !selected[l.to]) || b.internal(l) {
			continue
		}
		for _, vf := range b.view(l.from, l.to) {
			for _, vt := range b.view(l.to, l.from) {
				if vf == vt || b.inZone(vf, vt) || b.inZone(vt, vf) {
					continue
				}
				keep(vf, l.from)
				keep(vt, l.to)
				key := vf + "\x00" + vt
				if f.Level == LevelService {
					key += "\x00" + string(l.source)
				}
				e, ok := edges[key]
				if !ok {
					e = &Edge{From: vf, To: vt, Source: l.source, Confidence: l.confidence, Rule: l.detail.Rule}
					edges[key] = e
					order = append(order, key)
				}
				e.Count++
				e.Confidence = weakest(e.Confidence, l.confidence)
				if e.Rule != l.detail.Rule {
					e.Rule = "mixed"
				}
				if e.Source != l.source {
					e.Source = SourceMixed
				}
				for _, label := range b.labels(l.detail, vt) {
					if !slices.Contains(e.Labels, label) {
						e.Labels = append(e.Labels, label)
					}
				}
			}
		}
	}
	// A selected node without link shows too: its content at the levels of
	// the services and the apps, a host without content at the others.
	for id := range selected {
		if (b.all[id].Kind == KindHost) != (f.Level == LevelHost || f.Level == LevelProject) {
			continue
		}
		for _, v := range b.view(id, "") {
			keep(v, id)
		}
	}
	// An expanded zone shows all its content, linked or not.
	for _, zone := range f.Expand {
		for _, h := range b.zoneHosts(zone) {
			keep(zone, h)
			for _, child := range b.hostChildren[h] {
				for _, v := range b.view(child, "") {
					keep(v, child)
				}
			}
		}
	}
	for id, raw := range members {
		n := b.node(id, raw)
		for r := range raw {
			if selected[r] {
				n.Selected = true
			}
		}
		g.Nodes = append(g.Nodes, n)
	}
	slices.SortFunc(g.Nodes, func(a, b Node) int {
		return cmp.Or(cmp.Compare(a.Host, b.Host), cmp.Compare(kindOrder(a.Kind), kindOrder(b.Kind)),
			cmp.Compare(a.Label, b.Label), cmp.Compare(a.ID, b.ID))
	})
	env := make(map[string]string, len(g.Nodes))
	for _, n := range g.Nodes {
		if n.Kind != KindExternal {
			env[n.ID] = n.Env
		}
	}
	for _, key := range order {
		e := edges[key]
		e.CrossEnv = env[e.From] != "" && env[e.To] != "" && env[e.From] != env[e.To]
		g.Edges = append(g.Edges, *e)
	}
	// The edges follow the order of the nodes.
	rank := make(map[string]int, len(g.Nodes))
	for i, n := range g.Nodes {
		rank[n.ID] = i
	}
	slices.SortStableFunc(g.Edges, func(a, b Edge) int {
		return cmp.Or(cmp.Compare(rank[a.From], rank[b.From]), cmp.Compare(rank[a.To], rank[b.To]), cmp.Compare(a.Source, b.Source))
	})
	if f.Level == LevelService || f.Level == LevelApp {
		g.Groups = b.groups(g.Nodes)
	}
	g.Groups = append(g.Groups, b.zones(g.Nodes)...)
	return g
}

func matches(values []string, value string) bool {
	return len(values) == 0 || slices.Contains(values, value)
}

// weakest is the least sure of two confidences.
func weakest(a, b Confidence) Confidence {
	rank := map[Confidence]int{ConfidenceExact: 0, ConfidenceInferred: 1, ConfidenceAmbiguous: 2}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func kindOrder(kind NodeKind) int {
	return slices.Index([]NodeKind{KindProject, KindHost, KindWebserver, KindApp, KindService, KindRuntime, KindExternal}, kind)
}

// link is a link of the inventory between nodes of the builder, its
// detail decoded once.
type link struct {
	from, to         string
	fromKind, toKind NodeKind
	source           Source
	confidence       Confidence
	detail           linker.Detail
}

// elementKind is what a node of the level not in the inventory stands
// for.
type elementKind int

const (
	elementExternals elementKind = iota + 1
	elementProject
	elementStack
	elementItem
)

// element is a node of the level standing for nodes of the inventory: the
// externals of an env, a project in an env, a stack, or the content of an
// expanded zone of one kind and name.
type element struct {
	kind elementKind
	// project and env are those of a project, env that of externals.
	project, env string
	// zone, nodeKind and label are those of an item.
	zone     string
	nodeKind NodeKind
	label    string
}

// content is what a host holds.
type content struct {
	apps       []*model.App
	services   []*model.Service
	webservers []*model.Webserver
	// proxy is a host with a webserver routing to another host.
	proxy bool
}

type builder struct {
	inv          *model.Inventory
	f            Filter
	hosts        map[uuid.UUID]*model.Host
	apps         map[uuid.UUID]*model.App
	projects     map[uuid.UUID]string
	envs         map[uuid.UUID]string
	all          map[string]Node
	host         map[string]string // node → host node
	hostChildren map[string][]string
	content      map[string]*content // host node → its content
	// links are the links of the inventory, decoded; visible those between
	// nodes of the builder the filter keeps.
	links   []link
	visible []*link
	// elements are the nodes of the level standing for others, by id.
	elements map[string]element
	// folded are the webservers and services of the level of the apps
	// drawn inside the apps: a webserver gives the apps it serves, a
	// service nothing.
	folded map[string][]string
	// foldedInto are the technologies folded into an app.
	foldedInto map[string][]string
	stackOf    map[string]string   // host node → stack node
	stacks     map[string][]string // stack node → host nodes
	expanded   map[string]bool     // zones drawn with their content
}

func newBuilder(inv *model.Inventory, f Filter) *builder {
	b := &builder{inv: inv, f: f, hosts: map[uuid.UUID]*model.Host{}, apps: map[uuid.UUID]*model.App{}, projects: map[uuid.UUID]string{},
		envs: map[uuid.UUID]string{}, all: map[string]Node{}, host: map[string]string{}, hostChildren: map[string][]string{},
		content: map[string]*content{}, elements: map[string]element{}, folded: map[string][]string{}, foldedInto: map[string][]string{},
		stackOf: map[string]string{}, stacks: map[string][]string{}, expanded: map[string]bool{}}
	if f.Level == LevelProject || f.Level == LevelHost {
		for _, zone := range f.Expand {
			b.expanded[zone] = true
		}
	} else {
		b.f.Expand = nil
	}
	for i := range inv.Hosts {
		if f.Hidden == nil || !f.Hidden(&inv.Hosts[i]) {
			b.hosts[inv.Hosts[i].ID] = &inv.Hosts[i]
		}
	}
	for i := range inv.Apps {
		b.apps[inv.Apps[i].ID] = &inv.Apps[i]
	}
	for _, p := range inv.Projects {
		b.projects[p.ID] = p.Slug
	}
	for _, e := range inv.Environments {
		b.envs[e.ID] = e.Slug
	}
	return b
}

func (b *builder) id(kind NodeKind, id uuid.UUID, external *string) string {
	if kind == KindExternal {
		return string(kind) + ":" + deref(external)
	}
	return string(kind) + ":" + id.String()
}

func (b *builder) slug(m map[uuid.UUID]string, id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return m[*id]
}

// nodes indexes every node a link may reach, external targets included,
// and decodes the links.
func (b *builder) nodes() {
	for _, h := range b.inv.Hosts {
		if b.hosts[h.ID] == nil {
			continue
		}
		id := b.id(KindHost, h.ID, nil)
		b.all[id] = Node{ID: id, Kind: KindHost, Label: h.Hostname, Host: h.Hostname, Project: b.slug(b.projects, h.ProjectID),
			Env: b.slug(b.envs, h.EnvironmentID), Entity: &h.ID, HostIDs: []uuid.UUID{h.ID}}
		b.content[id] = &content{}
	}
	add := func(kind NodeKind, id, hostID uuid.UUID, n Node, project, env *uuid.UUID) bool {
		h := b.hosts[hostID]
		if h == nil {
			return false
		}
		n.ID, n.Kind, n.Host = b.id(kind, id, nil), kind, h.Hostname
		n.Project = cmp.Or(b.slug(b.projects, project), b.slug(b.projects, h.ProjectID))
		n.Env = cmp.Or(b.slug(b.envs, env), b.slug(b.envs, h.EnvironmentID))
		n.Entity, n.HostIDs = &id, []uuid.UUID{hostID}
		b.all[n.ID] = n
		hostNode := b.id(KindHost, hostID, nil)
		b.host[n.ID] = hostNode
		b.hostChildren[hostNode] = append(b.hostChildren[hostNode], n.ID)
		return true
	}
	for i := range b.inv.Apps {
		a := &b.inv.Apps[i]
		n := Node{Label: a.Name, Detail: a.Role, Version: codeOf(a).Version}
		if a.Framework != nil {
			n.Detail = strings.TrimSpace(*a.Framework + " " + deref(a.FrameworkVersion))
			n.Techs = append(n.Techs, *a.Framework)
		}
		if rt := runtimeOf(a); rt != "" {
			n.Techs = append(n.Techs, rt)
		}
		if add(KindApp, a.ID, a.HostID, n, a.ProjectID, a.EnvironmentID) {
			c := b.content[b.id(KindHost, a.HostID, nil)]
			c.apps = append(c.apps, a)
		}
	}
	for i := range b.inv.Services {
		s := &b.inv.Services[i]
		label := s.Type
		if s.Instance != "" && s.Instance != "default" {
			label += " " + s.Instance
		}
		if add(KindService, s.ID, s.HostID, Node{Label: label, Detail: deref(s.Version), Techs: []string{s.Type}}, nil, nil) {
			c := b.content[b.id(KindHost, s.HostID, nil)]
			c.services = append(c.services, s)
		}
	}
	for i := range b.inv.Webservers {
		w := &b.inv.Webservers[i]
		n := Node{Label: w.Type, Detail: deref(w.Version), Techs: []string{w.Type}, Role: RoleWeb}
		if proxyTypes[w.Type] {
			n.Role = RoleProxy
		}
		if add(KindWebserver, w.ID, w.HostID, n, nil, nil) {
			c := b.content[b.id(KindHost, w.HostID, nil)]
			c.webservers = append(c.webservers, w)
		}
	}
	// The runtimes only show in an expanded zone.
	if len(b.expanded) > 0 {
		for i := range b.inv.Runtimes {
			r := &b.inv.Runtimes[i]
			add(KindRuntime, r.ID, r.HostID, Node{Label: strings.TrimSpace(r.Type + " " + r.Version), Detail: r.Path, Techs: []string{r.Type}}, nil, nil)
		}
	}
	b.links = make([]link, 0, len(b.inv.Links))
	for _, l := range b.inv.Links {
		dl := link{from: b.id(NodeKind(l.FromType), l.FromID, nil), to: b.id(NodeKind(l.ToType), deref(l.ToID), l.ExternalTarget),
			fromKind: NodeKind(l.FromType), toKind: NodeKind(l.ToType), source: Source(l.Source), confidence: Confidence(l.Confidence)}
		_ = json.Unmarshal(l.Detail, &dl.detail)
		b.links = append(b.links, dl)
		if l.ExternalTarget != nil {
			id := b.id(KindExternal, uuid.Nil, l.ExternalTarget)
			b.all[id] = Node{ID: id, Kind: KindExternal, Label: *l.ExternalTarget, Detail: dl.detail.Reason}
		}
	}
	types := make(map[string]string, len(b.inv.Services))
	for _, s := range b.inv.Services {
		types[b.id(KindService, s.ID, nil)] = s.Type
	}
	for i := range b.links {
		l := &b.links[i]
		if l.fromKind != KindWebserver || b.host[l.from] == "" {
			continue
		}
		// A webserver routing to another host makes its host a proxy.
		if b.host[l.to] != "" && b.host[l.to] != b.host[l.from] {
			b.content[b.host[l.from]].proxy = true
		}
		// A webserver passing HTTP to a backend is a proxy.
		if httpLink(l, types[l.to]) {
			n := b.all[l.from]
			n.Role = RoleProxy
			b.all[l.from] = n
		}
	}
}

// httpLink tells a link of a webserver sending HTTP to a backend: a
// proxy_pass, an upstream or a backend, not a vhost serving an app nor a
// FastCGI pass; service is the type of the service it reaches.
func httpLink(l *link, service string) bool {
	if len(l.detail.Routes) == 0 {
		return false
	}
	return l.toKind != KindService || !strings.Contains(service, "fpm")
}

// present tells a link between nodes of the builder.
func (b *builder) present(l *link) bool {
	_, from := b.all[l.from]
	_, to := b.all[l.to]
	return from && to
}

// visibleLinks are the links between nodes of the builder the filter
// keeps.
func (b *builder) visibleLinks() []*link {
	var out []*link
	for i := range b.links {
		l := &b.links[i]
		if !b.present(l) || (b.f.NoExternals && l.toKind == KindExternal) ||
			(len(b.f.Confidences) > 0 && !slices.Contains(b.f.Confidences, l.confidence)) || b.isolated(l) {
			continue
		}
		out = append(out, l)
	}
	return out
}

// isolated tells a link the filter isolates: a webserver of a selected env
// and a node of another env, either way. A node without env, such as an
// external target, belongs to no other env.
func (b *builder) isolated(l *link) bool {
	if !b.f.Isolate || len(b.f.Envs) == 0 {
		return false
	}
	shared := func(ws, other string) bool {
		env := b.all[other].Env
		return b.all[ws].Kind == KindWebserver && slices.Contains(b.f.Envs, b.all[ws].Env) && env != "" && !slices.Contains(b.f.Envs, env)
	}
	return shared(l.from, l.to) || shared(l.to, l.from)
}

// internal tells a link a folded webserver makes inside its host: it is
// drawn inside the apps.
func (b *builder) internal(l *link) bool {
	_, ok := b.folded[l.from]
	return ok && b.host[l.to] == b.host[l.from]
}

// fold finds, at the level of the apps, the webservers serving only apps
// of their host, and the services only these webservers reach, such as
// php-fpm.
func (b *builder) fold() {
	if b.f.Level != LevelApp {
		return
	}
	out, in := map[string][]string{}, map[string][]string{}
	for i := range b.links {
		l := &b.links[i]
		if !b.present(l) {
			continue
		}
		out[l.from] = append(out[l.from], l.to)
		in[l.to] = append(in[l.to], l.from)
	}
	for id, n := range b.all {
		if n.Kind != KindWebserver || len(out[id]) == 0 {
			continue
		}
		var apps []string
		local := true
		for _, to := range out[id] {
			if b.host[to] != b.host[id] {
				local = false
				break
			}
			if b.all[to].Kind == KindApp && !slices.Contains(apps, to) {
				apps = append(apps, to)
			}
		}
		if local && len(apps) > 0 {
			slices.Sort(apps)
			b.folded[id] = apps
		}
	}
	for id, n := range b.all {
		if n.Kind != KindService || len(in[id]) == 0 {
			continue
		}
		only := true
		for _, from := range in[id] {
			if _, ok := b.folded[from]; !ok || b.host[from] != b.host[id] {
				only = false
				break
			}
		}
		if !only {
			continue
		}
		b.folded[id] = nil
		for _, from := range in[id] {
			for _, app := range b.runs(b.folded[from], n.Techs[0]) {
				b.addTech(app, n.Techs[0])
			}
		}
	}
	for id, apps := range b.folded {
		for _, app := range apps {
			b.addTech(app, b.all[id].Techs[0])
		}
	}
	for _, techs := range b.foldedInto {
		slices.Sort(techs)
	}
}

// runs keeps the apps whose runtime the service runs, such as the PHP apps
// for php-fpm; all of them when none does.
func (b *builder) runs(apps []string, service string) []string {
	var out []string
	for _, app := range apps {
		if rt := runtimeOf(b.apps[*b.all[app].Entity]); rt != "" && strings.HasPrefix(service, rt) {
			out = append(out, app)
		}
	}
	if len(out) == 0 {
		return apps
	}
	return out
}

func (b *builder) addTech(app, tech string) {
	if !slices.Contains(b.foldedInto[app], tech) {
		b.foldedInto[app] = append(b.foldedInto[app], tech)
	}
}

// stack gathers, at the level of the hosts, the hosts of a project and env
// holding the same apps, services and webservers.
func (b *builder) stack() {
	if b.f.Level != LevelHost {
		return
	}
	bySignature := map[string][]string{}
	var signatures []string
	for id, n := range b.all {
		if n.Kind != KindHost || slices.Contains(b.f.Hosts, n.Host) {
			continue
		}
		c := b.content[id]
		var apps, services, webservers []string
		for _, a := range c.apps {
			apps = append(apps, a.Name)
		}
		for _, s := range c.services {
			services = append(services, s.Type)
		}
		for _, w := range c.webservers {
			webservers = append(webservers, w.Type)
		}
		if len(apps)+len(services)+len(webservers) == 0 {
			continue
		}
		slices.Sort(apps)
		slices.Sort(services)
		slices.Sort(webservers)
		sig := strings.Join([]string{n.Project, n.Env, strings.Join(apps, ","), strings.Join(services, ","), strings.Join(webservers, ",")}, "\x00")
		if bySignature[sig] == nil {
			signatures = append(signatures, sig)
		}
		bySignature[sig] = append(bySignature[sig], id)
	}
	for _, sig := range signatures {
		hosts := bySignature[sig]
		if len(hosts) < 2 {
			continue
		}
		slices.SortFunc(hosts, func(x, y string) int { return cmp.Compare(b.all[x].Host, b.all[y].Host) })
		id := b.element("stack:"+b.all[hosts[0]].Entity.String(), element{kind: elementStack})
		b.stacks[id] = hosts
		for _, h := range hosts {
			b.stackOf[h] = id
		}
	}
}

// element records what a node of the level stands for, and gives its id.
func (b *builder) element(id string, e element) string {
	b.elements[id] = e
	return id
}

// hostOf gives the host node of a node; an external node stays itself.
func (b *builder) hostOf(id string) string {
	if h, ok := b.host[id]; ok {
		return h
	}
	return id
}

// view gives the nodes of the level a node is drawn as: none for a folded
// service, the apps of a folded webserver. An external target is merged
// per env of the other end of its link at the levels of the hosts and of
// the projects.
func (b *builder) view(id, other string) []string {
	n := b.all[id]
	if n.Kind == KindExternal && (b.f.Level == LevelHost || b.f.Level == LevelProject) {
		env := b.all[other].Env
		return []string{b.element("externals:"+env, element{kind: elementExternals, env: env})}
	}
	switch b.f.Level {
	case LevelHost, LevelProject:
		zone := b.zone(id)
		if b.expanded[zone] && n.Kind != KindHost {
			return []string{b.element(itemPrefix(zone)+string(n.Kind)+"|"+n.Label, element{kind: elementItem, zone: zone, nodeKind: n.Kind, label: n.Label})}
		}
		return []string{zone}
	case LevelApp:
		if apps, ok := b.folded[id]; ok {
			return apps
		}
	}
	return []string{id}
}

// zone is the node of the levels of the hosts and the projects a node is
// drawn in: its stack or host, or its project and env.
func (b *builder) zone(id string) string {
	h := b.hostOf(id)
	if b.f.Level == LevelProject {
		host := b.all[h]
		return b.element(string(KindProject)+":"+host.Project+"@"+host.Env, element{kind: elementProject, project: host.Project, env: host.Env})
	}
	if s, ok := b.stackOf[h]; ok {
		return s
	}
	return h
}

// zoneHosts are the host nodes of a zone.
func (b *builder) zoneHosts(zone string) []string {
	var hosts []string
	for id, n := range b.all {
		if n.Kind == KindHost && b.zone(id) == zone {
			hosts = append(hosts, id)
		}
	}
	slices.SortFunc(hosts, func(x, y string) int { return cmp.Compare(b.all[x].Host, b.all[y].Host) })
	return hosts
}

func itemPrefix(zone string) string {
	return "item:" + zone + "|"
}

// inZone tells a node of the level the content of an expanded zone.
func (b *builder) inZone(id, zone string) bool {
	e, ok := b.elements[id]
	return ok && e.kind == elementItem && e.zone == zone
}

// labels are the labels a link gives an edge: its key, or the domains it
// routes; to a folded webserver, the domains of the app reached.
func (b *builder) labels(d linker.Detail, to string) []string {
	if d.Key != "" {
		return []string{d.Key}
	}
	n := b.all[to]
	if n.Kind != KindApp || n.Entity == nil {
		return d.Domains
	}
	vhosts := b.apps[*n.Entity].Vhosts
	var mine []string
	for _, domain := range d.Domains {
		if slices.Contains(vhosts, domain) {
			mine = append(mine, domain)
		}
	}
	if len(mine) == 0 {
		return d.Domains
	}
	return mine
}

// node draws a node of the level from the nodes it stands for.
func (b *builder) node(id string, raw map[string]bool) Node {
	switch e := b.elements[id]; e.kind {
	case elementExternals:
		n := Node{ID: id, Kind: KindExternal, Label: "externals", Env: e.env}
		for r := range raw {
			n.Members = append(n.Members, b.all[r].Label)
		}
		slices.Sort(n.Members)
		n.Count = len(n.Members)
		return n
	case elementProject:
		var hosts []string
		for hid, n := range b.all {
			if n.Kind == KindHost && n.Project == e.project && n.Env == e.env {
				hosts = append(hosts, hid)
			}
		}
		n := b.aggregate(hosts)
		n.ID, n.Kind, n.Label, n.Project, n.Env = id, KindProject, e.project, e.project, e.env
		n.Expanded = b.expanded[id]
		return n
	case elementStack:
		hosts := b.stacks[id]
		n := b.aggregate(hosts)
		first := b.all[hosts[0]]
		n.ID, n.Kind, n.Label, n.Project, n.Env = id, KindHost, stackLabel(n.Members), first.Project, first.Env
		n.Divergences = b.divergences(hosts)
		n.Expanded = b.expanded[id]
		return n
	case elementItem:
		return b.item(id, e, raw)
	}
	n := b.all[id]
	switch n.Kind {
	case KindHost:
		h := b.aggregate([]string{id})
		n.Role, n.Techs, n.Apps, n.Detail = h.Role, h.Techs, h.Apps, h.Detail
		n.Expanded = b.expanded[id]
	case KindApp:
		n.Techs = append(slices.Clone(n.Techs), b.foldedInto[id]...)
		n.Techs = slices.Compact(n.Techs)
	}
	return n
}

// item draws the content of an expanded zone of one kind and name: an app
// deployed on several hosts of the zone is one item, its versions
// compared; a webserver proxying on one host is a proxy.
func (b *builder) item(id string, e element, raw map[string]bool) Node {
	kind, label := e.nodeKind, e.label
	ids := slices.Collect(maps.Keys(raw))
	slices.SortFunc(ids, func(x, y string) int { return cmp.Or(cmp.Compare(b.all[x].Host, b.all[y].Host), cmp.Compare(x, y)) })
	first := b.all[ids[0]]
	n := Node{ID: id, Kind: kind, Label: label, Project: first.Project, Env: first.Env, Techs: first.Techs, Count: len(ids), Role: first.Role}
	var details, versions []string
	var deployed []Deployment
	diverges := false
	for _, r := range ids {
		rn := b.all[r]
		n.Members = append(n.Members, rn.Host)
		n.HostIDs = append(n.HostIDs, rn.HostIDs...)
		if rn.Role == RoleProxy {
			n.Role = RoleProxy
		}
		if rn.Detail != "" && !slices.Contains(details, rn.Detail) {
			details = append(details, rn.Detail)
		}
		if rn.Version != "" && !slices.Contains(versions, rn.Version) {
			versions = append(versions, rn.Version)
		}
		if kind == KindApp {
			c := codeOf(b.apps[*rn.Entity])
			deployed = append(deployed, Deployment{Host: rn.Host, Version: c.Version, Commit: c.Commit})
			diverges = diverges || c.Version != deployed[0].Version || c.Commit != deployed[0].Commit
		}
	}
	n.Detail = strings.Join(details, " / ")
	n.Version = strings.Join(versions, " / ")
	if diverges {
		n.Divergences = []Divergence{{App: label, Hosts: deployed}}
	}
	// An item on several hosts stands for its first deployment.
	n.Entity = first.Entity
	if len(ids) == 1 {
		n.Host = first.Host
	}
	return n
}

// zones are the groups of the expanded zones, their content in the order
// of the nodes.
func (b *builder) zones(nodes []Node) []Group {
	var out []Group
	index := map[string]int{}
	for _, n := range nodes {
		e, ok := b.elements[n.ID]
		if !ok || e.kind != elementItem {
			continue
		}
		i, ok := index[e.zone]
		if !ok {
			i = len(out)
			index[e.zone] = i
			out = append(out, Group{ID: e.zone, Label: e.zone, Project: n.Project, Env: n.Env})
		}
		out[i].Nodes = append(out[i].Nodes, n.ID)
	}
	return out
}

// aggregate draws what hosts hold together: their role, technologies,
// apps and the versions of their services.
func (b *builder) aggregate(hosts []string) Node {
	n := Node{Count: len(hosts)}
	var frameworks, runtimes, services, versions, webservers, names []string
	proxy := false
	add := func(list *[]string, v string) {
		if v != "" && !slices.Contains(*list, v) {
			*list = append(*list, v)
		}
	}
	for _, h := range hosts {
		host := b.all[h]
		n.Members = append(n.Members, host.Host)
		n.HostIDs = append(n.HostIDs, *host.Entity)
		c := b.content[h]
		n.Apps += len(c.apps)
		proxy = proxy || c.proxy
		for _, a := range c.apps {
			add(&frameworks, deref(a.Framework))
			add(&runtimes, runtimeOf(a))
			add(&names, a.Name)
		}
		for _, s := range c.services {
			add(&services, s.Type)
			add(&versions, deref(s.Version))
		}
		for _, w := range c.webservers {
			add(&webservers, w.Type)
		}
	}
	slices.Sort(n.Members)
	slices.Sort(names)
	slices.Sort(versions)
	switch {
	case n.Apps > 0:
		n.Role = RoleApp
		n.Techs = slices.Concat(frameworks, webservers, runtimes)
		n.Detail = strings.Join(names, ", ")
	case proxy:
		n.Role = RoleProxy
		n.Techs = slices.Concat(webservers, services)
		n.Detail = strings.Join(webservers, " · ")
	case len(services) > 0:
		n.Role = RoleService
		n.Techs = slices.Concat(services, webservers)
		n.Detail = strings.Join(versions, " / ")
	default:
		n.Techs = webservers
	}
	return n
}

// divergences are the apps a stack runs in several versions: a branch
// deployed may hold several commits.
func (b *builder) divergences(hosts []string) []Divergence {
	byApp := map[string][]Deployment{}
	var apps []string
	for _, h := range hosts {
		for _, a := range b.content[h].apps {
			c := codeOf(a)
			if byApp[a.Name] == nil {
				apps = append(apps, a.Name)
			}
			byApp[a.Name] = append(byApp[a.Name], Deployment{Host: b.all[h].Host, Version: c.Version, Commit: c.Commit})
		}
	}
	slices.Sort(apps)
	var out []Divergence
	for _, app := range apps {
		deployed := byApp[app]
		for _, d := range deployed[1:] {
			if d.Version != deployed[0].Version || d.Commit != deployed[0].Commit {
				out = append(out, Divergence{App: app, Hosts: deployed})
				break
			}
		}
	}
	return out
}

// stackLabel names a stack by the words its hostnames start with, cut on
// "-": acme-shop-prod-1 and acme-shop-prod-2 give acme-shop-prod-*, web-prod
// and web-prod-2 give web-prod*; db1 and db12 share no word.
func stackLabel(hostnames []string) string {
	common := strings.Split(hostnames[0], "-")
	for _, h := range hostnames {
		words := strings.Split(h, "-")
		n := 0
		for n < len(common) && n < len(words) && common[n] == words[n] {
			n++
		}
		common = common[:n]
	}
	if len(common) == 0 {
		return hostnames[0] + " …"
	}
	prefix := strings.Join(common, "-")
	for _, h := range hostnames {
		if h == prefix {
			return prefix + "*"
		}
	}
	return prefix + "-*"
}

// code is what is read of the code of an app.
type code struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

func codeOf(a *model.App) code {
	var c code
	_ = json.Unmarshal(a.Code, &c)
	return c
}

func runtimeOf(a *model.App) string {
	var rt struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(a.Runtime, &rt)
	return rt.Type
}

// groups gathers the nodes of the graph by host.
func (b *builder) groups(nodes []Node) []Group {
	var out []Group
	index := map[string]int{}
	for _, n := range nodes {
		hostNode, ok := b.host[n.ID]
		if !ok {
			continue
		}
		i, ok := index[hostNode]
		if !ok {
			h := b.all[hostNode]
			i = len(out)
			index[hostNode] = i
			out = append(out, Group{ID: hostNode, Label: h.Label, Project: h.Project, Env: h.Env})
		}
		out[i].Nodes = append(out[i].Nodes, n.ID)
	}
	return out
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

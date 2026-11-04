package mcpserver

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/techversion"
	"github.com/google/uuid"
)

// index is the inventory of a namespace with its rows by id.
type index struct {
	inv        *model.Inventory
	hosts      map[uuid.UUID]*model.Host
	projects   map[uuid.UUID]*model.Project
	envs       map[uuid.UUID]*model.Environment
	apps       map[uuid.UUID]*model.App
	services   map[uuid.UUID]*model.Service
	webservers map[uuid.UUID]*model.Webserver
}

func newIndex(inv *model.Inventory) *index {
	x := &index{inv: inv, hosts: map[uuid.UUID]*model.Host{}, projects: map[uuid.UUID]*model.Project{}, envs: map[uuid.UUID]*model.Environment{},
		apps: map[uuid.UUID]*model.App{}, services: map[uuid.UUID]*model.Service{}, webservers: map[uuid.UUID]*model.Webserver{}}
	for i := range inv.Hosts {
		x.hosts[inv.Hosts[i].ID] = &inv.Hosts[i]
	}
	for i := range inv.Projects {
		x.projects[inv.Projects[i].ID] = &inv.Projects[i]
	}
	for i := range inv.Environments {
		x.envs[inv.Environments[i].ID] = &inv.Environments[i]
	}
	for i := range inv.Apps {
		x.apps[inv.Apps[i].ID] = &inv.Apps[i]
	}
	for i := range inv.Services {
		x.services[inv.Services[i].ID] = &inv.Services[i]
	}
	for i := range inv.Webservers {
		x.webservers[inv.Webservers[i].ID] = &inv.Webservers[i]
	}
	return x
}

func (x *index) hostname(id uuid.UUID) string {
	if h := x.hosts[id]; h != nil {
		return h.Hostname
	}
	return ""
}

func (x *index) project(id *uuid.UUID) string {
	if id != nil {
		if p := x.projects[*id]; p != nil {
			return p.Slug
		}
	}
	return ""
}

func (x *index) env(id *uuid.UUID) string {
	if id != nil {
		if e := x.envs[*id]; e != nil {
			return e.Slug
		}
	}
	return ""
}

// placeOf is the project and env of an entity of a host: its own for an
// app, else those of its host.
func (x *index) placeOf(hostID uuid.UUID, projectID, envID *uuid.UUID) (string, string) {
	if projectID == nil && envID == nil {
		if h := x.hosts[hostID]; h != nil {
			projectID, envID = h.ProjectID, h.EnvironmentID
		}
	}
	return x.project(projectID), x.env(envID)
}

// findHost finds a host by hostname, case-insensitive, or id.
func (x *index) findHost(ref string) *model.Host {
	for i := range x.inv.Hosts {
		h := &x.inv.Hosts[i]
		if strings.EqualFold(h.Hostname, ref) || h.ID.String() == ref {
			return h
		}
	}
	return nil
}

// findApps finds the apps of an id, or of a name or a path, on the host
// if given.
func (x *index) findApps(ref string, host *model.Host) []*model.App {
	var out []*model.App
	for i := range x.inv.Apps {
		a := &x.inv.Apps[i]
		if a.ID.String() == ref {
			return []*model.App{a}
		}
		if (host == nil || a.HostID == host.ID) && (strings.EqualFold(a.Name, ref) || a.Path == ref) {
			out = append(out, a)
		}
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

// appRuntime reads the runtime of an app: its type, version and package
// version; empty when none was collected.
func appRuntime(a *model.App) (typ, version, packageVersion string) {
	var rt struct {
		Type           string `json:"type"`
		Version        string `json:"version"`
		PackageVersion string `json:"package_version"`
	}
	_ = json.Unmarshal(a.Runtime, &rt)
	return rt.Type, rt.Version, rt.PackageVersion
}

// raw decodes a JSON value of the inventory, nil when empty: the schema
// of the output takes any JSON there.
func raw(v json.RawMessage) any {
	var out any
	if len(v) == 0 || json.Unmarshal(v, &out) != nil {
		return nil
	}
	return out
}

// Hit is an entity found by a search.
type Hit struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind" jsonschema:"host, app, service, webserver or container"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Host      string `json:"host,omitempty"`
	Project   string `json:"project,omitempty"`
	Env       string `json:"env,omitempty"`
	// Match is what matched the query.
	Match string `json:"match"`
}

// search finds the entities whose names, paths, domains, images or IPs
// hold the query, case-insensitive.
func (x *index) search(query string) []Hit {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	ns := x.inv.Namespace.Slug
	var hits []Hit
	match := func(values ...string) string {
		for _, v := range values {
			if v != "" && strings.Contains(strings.ToLower(v), q) {
				return v
			}
		}
		return ""
	}
	ipsOf := map[uuid.UUID][]string{}
	for _, ip := range x.inv.IPs {
		ipsOf[ip.HostID] = append(ipsOf[ip.HostID], ip.IP.String())
	}
	for _, h := range x.inv.Hosts {
		if m := match(append([]string{h.Hostname}, ipsOf[h.ID]...)...); m != "" {
			p, e := x.project(h.ProjectID), x.env(h.EnvironmentID)
			hits = append(hits, Hit{Namespace: ns, Kind: "host", ID: h.ID.String(), Name: h.Hostname, Project: p, Env: e, Match: m})
		}
	}
	domains := map[uuid.UUID][]string{}
	for _, ep := range x.inv.Endpoints {
		if ep.Kind == model.EndpointKindDomain {
			domains[ep.OwnerID] = append(domains[ep.OwnerID], ep.Address)
		}
	}
	for _, a := range x.inv.Apps {
		if m := match(append(append([]string{a.Name, a.Path}, a.Vhosts...), domains[a.ID]...)...); m != "" {
			p, e := x.placeOf(a.HostID, a.ProjectID, a.EnvironmentID)
			hits = append(hits, Hit{Namespace: ns, Kind: "app", ID: a.ID.String(), Name: a.Name, Host: x.hostname(a.HostID), Project: p, Env: e, Match: m})
		}
	}
	for _, s := range x.inv.Services {
		if m := match(s.Type, s.Instance); m != "" {
			p, e := x.placeOf(s.HostID, nil, nil)
			hits = append(hits, Hit{Namespace: ns, Kind: "service", ID: s.ID.String(), Name: serviceName(&s), Host: x.hostname(s.HostID), Project: p, Env: e, Match: m})
		}
	}
	for _, w := range x.inv.Webservers {
		if m := match(append([]string{w.Type}, domains[w.ID]...)...); m != "" {
			p, e := x.placeOf(w.HostID, nil, nil)
			hits = append(hits, Hit{Namespace: ns, Kind: "webserver", ID: w.ID.String(), Name: w.Type, Host: x.hostname(w.HostID), Project: p, Env: e, Match: m})
		}
	}
	for _, c := range x.inv.Containers {
		if m := match(c.Name, c.Image); m != "" {
			p, e := x.placeOf(c.HostID, nil, nil)
			hits = append(hits, Hit{Namespace: ns, Kind: "container", ID: c.ID.String(), Name: c.Name, Host: x.hostname(c.HostID), Project: p, Env: e, Match: m})
		}
	}
	return hits
}

func serviceName(s *model.Service) string {
	if s.Instance == "" || s.Instance == "default" {
		return s.Type
	}
	return s.Type + " (" + s.Instance + ")"
}

// Runtime is a runtime installed on a host.
type Runtime struct {
	Type    string `json:"type"`
	Version string `json:"version"`
	Path    string `json:"path,omitempty"`
	Manager string `json:"manager,omitempty"`
	// PackageVersion is the version of its system package, revision included.
	PackageVersion string `json:"package_version,omitempty" jsonschema:"version of the system package, packaging revision included (8.1.34-9+deb.sury.org+1): distributions patch an end-of-life version there and keep the version"`
}

// Endpoint is where a service or webserver listens, or a domain it serves.
type Endpoint struct {
	Kind    string `json:"kind"`
	Address string `json:"address"`
	Port    int    `json:"port,omitempty"`
}

type ServiceOut struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Instance string `json:"instance,omitempty"`
	Version  string `json:"version,omitempty"`
	// PackageVersion is the version of its system package, revision included.
	PackageVersion string     `json:"package_version,omitempty" jsonschema:"version of the system package, packaging revision included"`
	Details        any        `json:"details,omitempty"`
	Endpoints      []Endpoint `json:"endpoints,omitempty"`
}

type WebserverOut struct {
	ID        string     `json:"id"`
	Type      string     `json:"type"`
	Version   string     `json:"version,omitempty"`
	Endpoints []Endpoint `json:"endpoints,omitempty"`
}

// AppSummary is an app as a host lists it.
type AppSummary struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Role             string `json:"role,omitempty"`
	Path             string `json:"path"`
	Project          string `json:"project,omitempty"`
	Env              string `json:"env,omitempty"`
	Framework        string `json:"framework,omitempty"`
	FrameworkVersion string `json:"framework_version,omitempty"`
	Runtime          string `json:"runtime,omitempty"`
	RuntimeVersion   string `json:"runtime_version,omitempty"`
	// RuntimePackageVersion is the version of the system package of the
	// runtime or of its PHP-FPM, revision included.
	RuntimePackageVersion string   `json:"runtime_package_version,omitempty" jsonschema:"version of the system package of the runtime or of its PHP-FPM, packaging revision included"`
	Vhosts                []string `json:"vhosts,omitempty"`
}

type ContainerOut struct {
	Name    string   `json:"name"`
	Image   string   `json:"image"`
	Version string   `json:"version,omitempty"`
	State   string   `json:"state"`
	Ports   []string `json:"ports,omitempty"`
}

// HostOut is a host with its content.
type HostOut struct {
	Namespace      string         `json:"namespace"`
	ID             string         `json:"id"`
	Hostname       string         `json:"hostname"`
	Project        string         `json:"project,omitempty"`
	Env            string         `json:"env,omitempty"`
	Index          string         `json:"index,omitempty"`
	Role           string         `json:"role,omitempty"`
	OS             any            `json:"os,omitempty"`
	Kernel         string         `json:"kernel,omitempty"`
	Arch           string         `json:"arch,omitempty"`
	Virtualization string         `json:"virtualization,omitempty"`
	Resources      any            `json:"resources,omitempty"`
	AgentVersion   string         `json:"agent_version,omitempty"`
	FirstSeenAt    time.Time      `json:"first_seen_at"`
	LastSeenAt     time.Time      `json:"last_seen_at"`
	IPs            []string       `json:"ips,omitempty"`
	Runtimes       []Runtime      `json:"runtimes,omitempty"`
	Services       []ServiceOut   `json:"services,omitempty"`
	Webservers     []WebserverOut `json:"webservers,omitempty"`
	Apps           []AppSummary   `json:"apps,omitempty"`
	Containers     []ContainerOut `json:"containers,omitempty"`
}

func (x *index) endpointsOf(ownerID uuid.UUID) []Endpoint {
	var out []Endpoint
	for _, ep := range x.inv.Endpoints {
		if ep.OwnerID == ownerID {
			out = append(out, Endpoint{Kind: ep.Kind, Address: ep.Address, Port: deref(ep.Port)})
		}
	}
	return out
}

func (x *index) appSummary(a *model.App) AppSummary {
	p, e := x.placeOf(a.HostID, a.ProjectID, a.EnvironmentID)
	rt, rtv, rtp := appRuntime(a)
	return AppSummary{ID: a.ID.String(), Name: a.Name, Role: a.Role, Path: a.Path, Project: p, Env: e,
		Framework: deref(a.Framework), FrameworkVersion: deref(a.FrameworkVersion), Runtime: rt, RuntimeVersion: rtv, RuntimePackageVersion: rtp, Vhosts: a.Vhosts}
}

func (x *index) host(h *model.Host) HostOut {
	out := HostOut{Namespace: x.inv.Namespace.Slug, ID: h.ID.String(), Hostname: h.Hostname, Project: x.project(h.ProjectID), Env: x.env(h.EnvironmentID),
		OS: raw(h.OS), Kernel: deref(h.Kernel), Arch: deref(h.Arch), Virtualization: deref(h.Virtualization), Resources: raw(h.Resources),
		AgentVersion: deref(h.AgentVersion), FirstSeenAt: h.FirstSeenAt, LastSeenAt: h.LastSeenAt, Index: h.Labels.Index, Role: h.Labels.Role}
	for _, ip := range x.inv.IPs {
		if ip.HostID == h.ID {
			out.IPs = append(out.IPs, ip.IP.String())
		}
	}
	for _, r := range x.inv.Runtimes {
		if r.HostID == h.ID {
			out.Runtimes = append(out.Runtimes, Runtime{Type: r.Type, Version: r.Version, Path: r.Path, Manager: deref(r.Manager), PackageVersion: deref(r.PackageVersion)})
		}
	}
	for _, s := range x.inv.Services {
		if s.HostID == h.ID {
			out.Services = append(out.Services, ServiceOut{ID: s.ID.String(), Type: s.Type, Instance: s.Instance, Version: deref(s.Version),
				PackageVersion: deref(s.PackageVersion), Details: raw(s.Details), Endpoints: x.endpointsOf(s.ID)})
		}
	}
	for _, w := range x.inv.Webservers {
		if w.HostID == h.ID {
			out.Webservers = append(out.Webservers, WebserverOut{ID: w.ID.String(), Type: w.Type, Version: deref(w.Version), Endpoints: x.endpointsOf(w.ID)})
		}
	}
	for i := range x.inv.Apps {
		if a := &x.inv.Apps[i]; a.HostID == h.ID {
			out.Apps = append(out.Apps, x.appSummary(a))
		}
	}
	for _, c := range x.inv.Containers {
		if c.HostID == h.ID {
			out.Containers = append(out.Containers, ContainerOut{Name: c.Name, Image: c.Image, Version: deref(c.Version), State: c.State, Ports: c.Ports})
		}
	}
	return out
}

// Dependency is a dependency an app declares in its configuration.
type Dependency struct {
	Key      string `json:"key"`
	Scheme   string `json:"scheme,omitempty"`
	Port     int    `json:"port,omitempty"`
	Path     string `json:"path,omitempty"`
	Target   any    `json:"target,omitempty"`
	Source   any    `json:"source,omitempty"`
	Unparsed bool   `json:"unparsed,omitempty"`
}

// AppOut is an app with its dependencies and links.
type AppOut struct {
	AppSummary
	Namespace    string       `json:"namespace"`
	Host         string       `json:"host"`
	ResolvedPath string       `json:"resolved_path,omitempty"`
	Code         any          `json:"code,omitempty"`
	Launchers    any          `json:"launchers,omitempty"`
	Info         any          `json:"info,omitempty"`
	FirstSeenAt  time.Time    `json:"first_seen_at"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
	// Uses are the links from the app, UsedBy those to it.
	Uses   []LinkOut `json:"uses,omitempty"`
	UsedBy []LinkOut `json:"used_by,omitempty"`
}

func (x *index) app(a *model.App) AppOut {
	out := AppOut{AppSummary: x.appSummary(a), Namespace: x.inv.Namespace.Slug, Host: x.hostname(a.HostID), ResolvedPath: deref(a.ResolvedPath),
		Code: raw(a.Code), Launchers: raw(a.Launchers), Info: raw(a.Info), FirstSeenAt: a.FirstSeenAt}
	for _, d := range x.inv.Dependencies {
		if d.AppID == a.ID {
			out.Dependencies = append(out.Dependencies, Dependency{Key: d.Key, Scheme: deref(d.Scheme), Port: deref(d.Port), Path: deref(d.Path),
				Target: raw(d.Target), Source: raw(d.Source), Unparsed: d.Unparsed})
		}
	}
	ids := map[uuid.UUID]bool{a.ID: true}
	out.Uses, out.UsedBy = x.links(ids, DirectionDependencies), x.links(ids, DirectionDependents)
	return out
}

// End is an end of a link.
type End struct {
	Kind    string `json:"kind" jsonschema:"app, service, webserver, host or external"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name"`
	Host    string `json:"host,omitempty"`
	Project string `json:"project,omitempty"`
	Env     string `json:"env,omitempty"`
}

// LinkOut is a link of the graph: From uses To.
type LinkOut struct {
	From       End    `json:"from"`
	To         End    `json:"to"`
	Source     string `json:"source" jsonschema:"what proves the link: env, traefik, webserver, launcher or process"`
	Confidence string `json:"confidence" jsonschema:"exact, inferred or ambiguous"`
	Detail     any    `json:"detail,omitempty"`
}

func (x *index) end(kind string, id *uuid.UUID, external *string) End {
	if id == nil {
		return End{Kind: model.LinkNodeExternal, Name: deref(external)}
	}
	e := End{Kind: kind, ID: id.String()}
	switch kind {
	case model.LinkNodeApp:
		if a := x.apps[*id]; a != nil {
			e.Name, e.Host = a.Name, x.hostname(a.HostID)
			e.Project, e.Env = x.placeOf(a.HostID, a.ProjectID, a.EnvironmentID)
		}
	case model.LinkNodeService:
		if s := x.services[*id]; s != nil {
			e.Name, e.Host = serviceName(s), x.hostname(s.HostID)
			e.Project, e.Env = x.placeOf(s.HostID, nil, nil)
		}
	case model.LinkNodeWebserver:
		if w := x.webservers[*id]; w != nil {
			e.Name, e.Host = w.Type, x.hostname(w.HostID)
			e.Project, e.Env = x.placeOf(w.HostID, nil, nil)
		}
	case model.LinkNodeHost:
		if h := x.hosts[*id]; h != nil {
			e.Name, e.Host = h.Hostname, h.Hostname
			e.Project, e.Env = x.project(h.ProjectID), x.env(h.EnvironmentID)
		}
	}
	return e
}

// Directions of the links of an entity.
const (
	// DirectionDependents are the links to the entity: who uses it.
	DirectionDependents = "dependents"
	// DirectionDependencies are the links from the entity: what it uses.
	DirectionDependencies = "dependencies"
	DirectionBoth         = "both"
)

// links are the links from or to the entities of ids.
func (x *index) links(ids map[uuid.UUID]bool, direction string) []LinkOut {
	var out []LinkOut
	for _, l := range x.inv.Links {
		from := ids[l.FromID]
		to := l.ToID != nil && ids[*l.ToID]
		if (direction == DirectionDependents && !to) || (direction == DirectionDependencies && !from) || (direction == DirectionBoth && !from && !to) {
			continue
		}
		out = append(out, LinkOut{From: x.end(l.FromType, &l.FromID, nil), To: x.end(l.ToType, l.ToID, l.ExternalTarget),
			Source: l.Source, Confidence: l.Confidence, Detail: raw(l.Detail)})
	}
	return out
}

// entitiesOf are the ids of a host and of all it holds.
func (x *index) entitiesOf(h *model.Host) map[uuid.UUID]bool {
	ids := map[uuid.UUID]bool{h.ID: true}
	for _, a := range x.inv.Apps {
		if a.HostID == h.ID {
			ids[a.ID] = true
		}
	}
	for _, s := range x.inv.Services {
		if s.HostID == h.ID {
			ids[s.ID] = true
		}
	}
	for _, w := range x.inv.Webservers {
		if w.HostID == h.ID {
			ids[w.ID] = true
		}
	}
	return ids
}

// The kinds of the versions of a technology.
const (
	// KindRuntime is a runtime installed on a host, e.g. php 8.1 in
	// /usr/bin/php8.1.
	KindRuntime = "runtime"
	// KindAppRuntime is the runtime an app runs on.
	KindAppRuntime = "app_runtime"
	KindFramework  = "framework"
	KindService    = "service"
	KindWebserver  = "webserver"
	KindContainer  = "container"
)

// Usage is a use of a technology in a version.
type Usage struct {
	Namespace  string `json:"namespace"`
	Kind       string `json:"kind" jsonschema:"runtime (installed on the host), app_runtime (the runtime of an app), framework, service, webserver or container"`
	Technology string `json:"technology"`
	Version    string `json:"version,omitempty"`
	// PackageVersion is the version of the system package, revision
	// included, for a runtime, an app runtime or a service.
	PackageVersion string `json:"package_version,omitempty" jsonschema:"version of the system package, packaging revision included (8.1.34-9+deb.sury.org+1): tells a patched end-of-life version apart"`
	Host           string `json:"host"`
	App            string `json:"app,omitempty"`
	AppID          string `json:"app_id,omitempty"`
	Project        string `json:"project,omitempty"`
	Env            string `json:"env,omitempty"`
}

// imageName is the name of a Docker image without registry nor tag:
// registry.example/library/mysql:8.0 gives mysql.
func imageName(image string) string {
	if i := strings.LastIndex(image, "@"); i >= 0 {
		image = image[:i]
	}
	if i := strings.LastIndex(image, "/"); i >= 0 {
		image = image[i+1:]
	}
	name, _, _ := strings.Cut(image, ":")
	return name
}

// usages are the uses of every technology, or of technology only.
func (x *index) usages(technology string) []Usage {
	ns := x.inv.Namespace.Slug
	keep := func(name string) bool {
		return name != "" && (technology == "" || strings.EqualFold(name, technology))
	}
	var out []Usage
	for _, r := range x.inv.Runtimes {
		if keep(r.Type) {
			p, e := x.placeOf(r.HostID, nil, nil)
			out = append(out, Usage{Namespace: ns, Kind: KindRuntime, Technology: r.Type, Version: r.Version, PackageVersion: deref(r.PackageVersion), Host: x.hostname(r.HostID), Project: p, Env: e})
		}
	}
	for i := range x.inv.Apps {
		a := &x.inv.Apps[i]
		p, e := x.placeOf(a.HostID, a.ProjectID, a.EnvironmentID)
		u := Usage{Namespace: ns, Host: x.hostname(a.HostID), App: a.Name, AppID: a.ID.String(), Project: p, Env: e}
		if rt, v, pv := appRuntime(a); keep(rt) {
			u := u
			u.Kind, u.Technology, u.Version, u.PackageVersion = KindAppRuntime, rt, v, pv
			out = append(out, u)
		}
		if fw := deref(a.Framework); keep(fw) {
			u.Kind, u.Technology, u.Version = KindFramework, fw, deref(a.FrameworkVersion)
			out = append(out, u)
		}
	}
	for _, s := range x.inv.Services {
		if keep(s.Type) {
			p, e := x.placeOf(s.HostID, nil, nil)
			out = append(out, Usage{Namespace: ns, Kind: KindService, Technology: s.Type, Version: deref(s.Version), PackageVersion: deref(s.PackageVersion), Host: x.hostname(s.HostID), Project: p, Env: e})
		}
	}
	for _, w := range x.inv.Webservers {
		if keep(w.Type) {
			p, e := x.placeOf(w.HostID, nil, nil)
			out = append(out, Usage{Namespace: ns, Kind: KindWebserver, Technology: w.Type, Version: deref(w.Version), Host: x.hostname(w.HostID), Project: p, Env: e})
		}
	}
	for _, c := range x.inv.Containers {
		if name := imageName(c.Image); keep(name) {
			p, e := x.placeOf(c.HostID, nil, nil)
			out = append(out, Usage{Namespace: ns, Kind: KindContainer, Technology: name, Version: deref(c.Version), Host: x.hostname(c.HostID), App: c.Name, Project: p, Env: e})
		}
	}
	return out
}

// filterVersions keeps the usages whose version meets op version.
func filterVersions(usages []Usage, op techversion.Op, version string) []Usage {
	if version == "" {
		return usages
	}
	var out []Usage
	for _, u := range usages {
		if techversion.Satisfies(u.Version, op, version) {
			out = append(out, u)
		}
	}
	return out
}

// sortUsages orders by technology, version, namespace, host and app.
func sortUsages(usages []Usage) {
	slices.SortStableFunc(usages, func(a, b Usage) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Technology), strings.ToLower(b.Technology)), cmp.Compare(a.Kind, b.Kind),
			techversion.Order(a.Version, b.Version), cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Host, b.Host), cmp.Compare(a.App, b.App))
	})
}

// VersionCount is how many use a version, as major.minor.
type VersionCount struct {
	Version string `json:"version" jsonschema:"major.minor, or unknown"`
	Count   int    `json:"count"`
}

// TechnologyVersions are the versions of a technology of a kind.
type TechnologyVersions struct {
	Technology string         `json:"technology"`
	Kind       string         `json:"kind"`
	Versions   []VersionCount `json:"versions"`
}

// versions counts the usages by technology, kind and major.minor.
func versions(usages []Usage) []TechnologyVersions {
	type key struct{ tech, kind string }
	counts := map[key]map[string]int{}
	for _, u := range usages {
		k := key{strings.ToLower(u.Technology), u.Kind}
		if counts[k] == nil {
			counts[k] = map[string]int{}
		}
		v := techversion.Minor(u.Version)
		if v == "" {
			v = "unknown"
		}
		counts[k][v]++
	}
	var out []TechnologyVersions
	for k, byVersion := range counts {
		tv := TechnologyVersions{Technology: k.tech, Kind: k.kind}
		for v, n := range byVersion {
			tv.Versions = append(tv.Versions, VersionCount{Version: v, Count: n})
		}
		slices.SortFunc(tv.Versions, func(a, b VersionCount) int { return techversion.Order(a.Version, b.Version) })
		out = append(out, tv)
	}
	slices.SortFunc(out, func(a, b TechnologyVersions) int {
		return cmp.Or(cmp.Compare(a.Technology, b.Technology), cmp.Compare(a.Kind, b.Kind))
	})
	return out
}

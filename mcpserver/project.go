package mcpserver

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/alexandreh2ag/lacis/linkpath"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/placement"
	"github.com/google/uuid"
)

// ProjectOut is a project with its apps, its hosts, what it uses and who
// uses it, whatever the hosts its apps are deployed on.
type ProjectOut struct {
	Namespace   string   `json:"namespace"`
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Envs        []string `json:"envs,omitempty"`
	// Apps are its apps, one per name and env.
	Apps  []ProjectApp  `json:"apps,omitempty"`
	Hosts []ProjectHost `json:"hosts,omitempty"`
	// Dependencies are what its apps use, UsedBy the apps of the other
	// projects using its apps, services and webservers: through the
	// proxies, named in via.
	Dependencies []ProjectLink `json:"dependencies,omitempty"`
	UsedBy       []ProjectLink `json:"used_by,omitempty"`
}

// ProjectApp is an app of a project in an env, whatever the hosts it is
// deployed on.
type ProjectApp struct {
	Name             string `json:"name"`
	Env              string `json:"env,omitempty"`
	Framework        string `json:"framework,omitempty"`
	FrameworkVersion string `json:"framework_version,omitempty"`
	Runtime          string `json:"runtime,omitempty"`
	RuntimeVersion   string `json:"runtime_version,omitempty"`
	// Versions are the versions of the code deployed, several when the
	// deployments differ.
	Versions []string `json:"versions,omitempty" jsonschema:"versions of the code deployed; several when the deployments differ"`
	Domains  []string `json:"domains,omitempty"`
	Hosts    []string `json:"hosts"`
	// IDs are those of its deployments, for get_app.
	IDs []string `json:"ids" jsonschema:"ids of its deployments, one per host, for get_app"`
}

type ProjectHost struct {
	ID         string    `json:"id"`
	Hostname   string    `json:"hostname"`
	Env        string    `json:"env,omitempty"`
	OS         string    `json:"os,omitempty"`
	Status     string    `json:"status" jsonschema:"active, stale or inactive, by the last snapshot of its agent"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// ProjectLink is an effective link of a project, whatever the hosts: From
// uses To through the proxies of Via.
type ProjectLink struct {
	From       End      `json:"from"`
	To         End      `json:"to"`
	Key        string   `json:"key,omitempty" jsonschema:"the dependency of the configuration the link comes from"`
	Confidence string   `json:"confidence" jsonschema:"exact, inferred or ambiguous; the weakest of the links crossed"`
	Via        []string `json:"via,omitempty" jsonschema:"the webservers crossed, in order, as type@hostname"`
	DeadEnd    bool     `json:"dead_end,omitempty" jsonschema:"true when 'to' is a webserver whose routes lead nowhere known for the name called"`
}

// findProject finds a project by slug or name, case-insensitive, or id.
func (x *index) findProject(ref string) *model.Project {
	for i := range x.inv.Projects {
		p := &x.inv.Projects[i]
		if strings.EqualFold(p.Slug, ref) || strings.EqualFold(p.Name, ref) || p.ID.String() == ref {
			return p
		}
	}
	return nil
}

func (x *index) projectOut(p *model.Project, now time.Time) ProjectOut {
	out := ProjectOut{Namespace: x.inv.Namespace.Slug, Slug: p.Slug, Name: p.Name, Description: deref(p.Description)}
	envs := map[string]bool{}
	// The hosts of the project: by their labels, or because they run its
	// apps, once per env.
	for _, h := range x.inv.Hosts {
		for _, place := range x.places.Of(h.ID) {
			if place.Project != p.ID {
				continue
			}
			e := x.env(&place.Env)
			envs[e] = true
			out.Hosts = append(out.Hosts, ProjectHost{ID: h.ID.String(), Hostname: h.Hostname, Env: e, OS: osName(h.OS),
				Status: hostStatus(&x.inv.Namespace, &h, now), LastSeenAt: h.LastSeenAt})
		}
	}
	slices.SortFunc(out.Hosts, func(a, b ProjectHost) int {
		return cmp.Or(cmp.Compare(a.Env, b.Env), cmp.Compare(a.Hostname, b.Hostname))
	})

	// The apps of the project: their own project, else that of their host.
	mine := map[uuid.UUID]bool{}
	for _, a := range x.mergedApps(func(project, _ string) bool { return project == p.Slug }) {
		envs[a.Env] = true
		out.Apps = append(out.Apps, a.ProjectApp)
		for _, id := range a.IDs {
			mine[uuid.MustParse(id)] = true
		}
	}
	for e := range envs {
		if e != "" {
			out.Envs = append(out.Envs, e)
		}
	}
	slices.Sort(out.Envs)

	// What the apps use, and who of the other projects uses its apps and
	// the services and webservers of its hosts.
	deps := newLinkSet(x)
	for id := range mine {
		deps.add(x.paths.Uses(id), func(linkpath.Path) bool { return true })
	}
	used := newLinkSet(x)
	other := func(path linkpath.Path) bool {
		first := path.First()
		return x.end(first.FromType, &first.FromID, nil).Project != p.Slug
	}
	for id := range mine {
		used.add(x.paths.UsedBy(id), other)
	}
	for _, list := range [][]uuid.UUID{x.servicesOfProject(p.ID), x.webserversOfProject(p.ID)} {
		for _, id := range list {
			used.add(x.paths.UsedBy(id), other)
		}
	}
	out.Dependencies, out.UsedBy = deps.sorted(), used.sorted()
	return out
}

// AppItem is an app of a project and an env, whatever the hosts it is
// deployed on.
type AppItem struct {
	Namespace string `json:"namespace"`
	Project   string `json:"project,omitempty"`
	ProjectApp
}

// mergedApps are the apps kept by keep, one per name, project and env,
// by project, env and name. An app without project or env is its own.
func (x *index) mergedApps(keep func(project, env string) bool) []AppItem {
	apps := map[string]*AppItem{}
	var order []string
	for i := range x.inv.Apps {
		a := &x.inv.Apps[i]
		project, env := x.placeOf(a.HostID, a.ProjectID, a.EnvironmentID)
		if !keep(project, env) {
			continue
		}
		key := strings.Join([]string{project, env, a.Name}, "\x00")
		if project == "" || env == "" {
			key += "\x00" + a.ID.String()
		}
		item := apps[key]
		if item == nil {
			rt, rtv, _ := appRuntime(a)
			item = &AppItem{Namespace: x.inv.Namespace.Slug, Project: project, ProjectApp: ProjectApp{Name: a.Name, Env: env,
				Framework: deref(a.Framework), FrameworkVersion: deref(a.FrameworkVersion), Runtime: rt, RuntimeVersion: rtv}}
			apps[key] = item
			order = append(order, key)
		}
		var code struct {
			Version string `json:"version"`
		}
		_ = json.Unmarshal(a.Code, &code)
		item.Versions = appendNew(item.Versions, code.Version)
		item.Domains = appendNew(item.Domains, a.Vhosts...)
		item.Hosts = appendNew(item.Hosts, x.hostname(a.HostID))
		item.IDs = append(item.IDs, a.ID.String())
	}
	slices.Sort(order)
	out := make([]AppItem, 0, len(order))
	for _, k := range order {
		out = append(out, *apps[k])
	}
	return out
}

func (x *index) servicesOfProject(projectID uuid.UUID) []uuid.UUID {
	var out []uuid.UUID
	for _, s := range x.inv.Services {
		if x.places.Any(s.ID, func(p placement.Place) bool { return p.Project == projectID }) {
			out = append(out, s.ID)
		}
	}
	return out
}

func (x *index) webserversOfProject(projectID uuid.UUID) []uuid.UUID {
	var out []uuid.UUID
	for _, w := range x.inv.Webservers {
		if x.places.Any(w.ID, func(p placement.Place) bool { return p.Project == projectID }) {
			out = append(out, w.ID)
		}
	}
	return out
}

// linkSet gathers the effective links of a project once each, whatever
// the hosts of their ends.
type linkSet struct {
	x     *index
	seen  map[string]bool
	links []ProjectLink
}

func newLinkSet(x *index) *linkSet { return &linkSet{x: x, seen: map[string]bool{}} }

func (s *linkSet) add(paths []linkpath.Path, keep func(linkpath.Path) bool) {
	for _, p := range paths {
		if !keep(p) {
			continue
		}
		first, last := p.First(), p.Last()
		l := ProjectLink{From: placeless(s.x.end(first.FromType, &first.FromID, nil)), To: placeless(s.x.end(last.ToType, last.ToID, last.ExternalTarget)),
			Confidence: p.Confidence(), DeadEnd: p.DeadEnd}
		var d struct {
			Key string `json:"key"`
		}
		_ = json.Unmarshal(first.Detail, &d)
		l.Key = d.Key
		for _, v := range p.Links[1:] {
			e := s.x.end(v.FromType, &v.FromID, nil)
			l.Via = append(l.Via, e.Name+"@"+e.Host)
		}
		key := strings.Join([]string{l.From.Kind, l.From.Name, l.From.Project, l.From.Env, l.To.Kind, l.To.Name, l.To.Project, l.To.Env, l.Key}, "\x00")
		if !s.seen[key] {
			s.seen[key] = true
			s.links = append(s.links, l)
		}
	}
}

func (s *linkSet) sorted() []ProjectLink {
	slices.SortFunc(s.links, func(a, b ProjectLink) int {
		return cmp.Or(cmp.Compare(a.From.Env, b.From.Env), cmp.Compare(a.From.Name, b.From.Name), cmp.Compare(a.To.Name, b.To.Name), cmp.Compare(a.Key, b.Key))
	})
	return s.links
}

// placeless is an end of a link without its host nor its id: one for all
// the deployments of an app.
func placeless(e End) End {
	if e.Kind == model.LinkNodeApp || e.Kind == model.LinkNodeService || e.Kind == model.LinkNodeWebserver {
		e.ID, e.Host = "", ""
	}
	return e
}

func appendNew(list []string, values ...string) []string {
	for _, v := range values {
		if v != "" && !slices.Contains(list, v) {
			list = append(list, v)
		}
	}
	return list
}

// osName is the pretty name of the OS of a host.
func osName(os json.RawMessage) string {
	var o struct {
		Pretty string `json:"pretty"`
	}
	_ = json.Unmarshal(os, &o)
	return o.Pretty
}

// hostStatus is the freshness of a host, by the thresholds of its
// namespace.
func hostStatus(ns *model.Namespace, h *model.Host, now time.Time) string {
	age := now.Sub(h.LastSeenAt)
	switch {
	case age > ns.InactiveAfter.Duration():
		return "inactive"
	case age > ns.StaleAfter.Duration():
		return "stale"
	}
	return "active"
}

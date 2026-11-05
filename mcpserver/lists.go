package mcpserver

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/alexandreh2ag/lacis/linkpath"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ListInput filters a listing; every namespace readable when none is
// given.
type ListInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"slug of the namespace; every namespace readable when empty"`
	Project   string `json:"project,omitempty" jsonschema:"slug or name of a project"`
	Env       string `json:"env,omitempty" jsonschema:"slug of an environment"`
	Query     string `json:"query,omitempty" jsonschema:"text the name (or type, hostname, domain) must contain, case-insensitive"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum number of results, 100 by default"`
}

// keeps tells whether an entity of a project and an env passes the
// filter.
func (in ListInput) keeps(x *index, project, env string) bool {
	if in.Project != "" {
		p := x.findProject(in.Project)
		if p == nil || p.Slug != project {
			return false
		}
	}
	return in.Env == "" || strings.EqualFold(in.Env, env)
}

// contains tells whether one of the values contains the query.
func (in ListInput) contains(values ...string) bool {
	q := strings.ToLower(strings.TrimSpace(in.Query))
	if q == "" {
		return true
	}
	return slices.ContainsFunc(values, func(v string) bool { return strings.Contains(strings.ToLower(v), q) })
}

type ProjectItem struct {
	Namespace   string   `json:"namespace"`
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Envs        []string `json:"envs,omitempty"`
	// Apps counts its apps once per name and env, Hosts its hosts.
	Apps  int `json:"apps" jsonschema:"number of its apps, once per name and env"`
	Hosts int `json:"hosts"`
}

type ListProjectsOutput struct {
	Projects  []ProjectItem `json:"projects"`
	Truncated bool          `json:"truncated,omitempty"`
}

func (s *server) listProjects(ctx context.Context, req *mcp.CallToolRequest, in ListInput) (*mcp.CallToolResult, ListProjectsOutput, error) {
	out := ListProjectsOutput{Projects: []ProjectItem{}}
	indexes, err := s.indexes(ctx, req, in.Namespace)
	if err != nil {
		return nil, out, err
	}
	for _, x := range indexes {
		for i := range x.inv.Projects {
			p := &x.inv.Projects[i]
			if (in.Project != "" && x.findProject(in.Project) != p) || !in.contains(p.Slug, p.Name, deref(p.Description)) {
				continue
			}
			item := ProjectItem{Namespace: x.inv.Namespace.Slug, Slug: p.Slug, Name: p.Name, Description: deref(p.Description)}
			for _, a := range x.mergedApps(func(project, env string) bool { return project == p.Slug && in.keeps(x, project, env) }) {
				item.Apps++
				item.Envs = appendNew(item.Envs, a.Env)
			}
			for _, h := range x.inv.Hosts {
				if h.ProjectID != nil && *h.ProjectID == p.ID && in.keeps(x, p.Slug, x.env(h.EnvironmentID)) {
					item.Hosts++
					item.Envs = appendNew(item.Envs, x.env(h.EnvironmentID))
				}
			}
			slices.Sort(item.Envs)
			if in.Env != "" && item.Apps+item.Hosts == 0 {
				continue
			}
			out.Projects = append(out.Projects, item)
		}
	}
	out.Projects, out.Truncated = truncate(out.Projects, limit(in.Limit))
	return nil, out, nil
}

type ListAppsOutput struct {
	Apps      []AppItem `json:"apps"`
	Truncated bool      `json:"truncated,omitempty"`
}

func (s *server) listApps(ctx context.Context, req *mcp.CallToolRequest, in ListInput) (*mcp.CallToolResult, ListAppsOutput, error) {
	out := ListAppsOutput{Apps: []AppItem{}}
	indexes, err := s.indexes(ctx, req, in.Namespace)
	if err != nil {
		return nil, out, err
	}
	for _, x := range indexes {
		for _, a := range x.mergedApps(func(project, env string) bool { return in.keeps(x, project, env) }) {
			if in.contains(append(append([]string{a.Name, a.Framework}, a.Domains...), a.Hosts...)...) {
				out.Apps = append(out.Apps, a)
			}
		}
	}
	out.Apps, out.Truncated = truncate(out.Apps, limit(in.Limit))
	return nil, out, nil
}

// ComponentItem is a service or a webserver of a host.
type ComponentItem struct {
	Namespace string `json:"namespace"`
	ID        string `json:"id"`
	Kind      string `json:"kind" jsonschema:"service or webserver"`
	Type      string `json:"type"`
	Instance  string `json:"instance,omitempty"`
	Version   string `json:"version,omitempty"`
	Host      string `json:"host"`
	Project   string `json:"project,omitempty"`
	Env       string `json:"env,omitempty"`
	// Domains are those a webserver serves.
	Domains []string `json:"domains,omitempty"`
}

type ListComponentsOutput struct {
	Components []ComponentItem `json:"components"`
	Truncated  bool            `json:"truncated,omitempty"`
}

func (s *server) listServices(ctx context.Context, req *mcp.CallToolRequest, in ListInput) (*mcp.CallToolResult, ListComponentsOutput, error) {
	return s.listComponents(ctx, req, in, model.LinkNodeService)
}

func (s *server) listWebservers(ctx context.Context, req *mcp.CallToolRequest, in ListInput) (*mcp.CallToolResult, ListComponentsOutput, error) {
	return s.listComponents(ctx, req, in, model.LinkNodeWebserver)
}

func (s *server) listComponents(ctx context.Context, req *mcp.CallToolRequest, in ListInput, kind string) (*mcp.CallToolResult, ListComponentsOutput, error) {
	out := ListComponentsOutput{Components: []ComponentItem{}}
	indexes, err := s.indexes(ctx, req, in.Namespace)
	if err != nil {
		return nil, out, err
	}
	for _, x := range indexes {
		var items []ComponentItem
		if kind == model.LinkNodeService {
			for _, c := range x.inv.Services {
				project, env := x.placeOf(c.HostID, nil, nil)
				items = append(items, ComponentItem{Namespace: x.inv.Namespace.Slug, ID: c.ID.String(), Kind: kind, Type: c.Type, Instance: c.Instance,
					Version: deref(c.Version), Host: x.hostname(c.HostID), Project: project, Env: env})
			}
		} else {
			for _, w := range x.inv.Webservers {
				project, env := x.placeOf(w.HostID, nil, nil)
				item := ComponentItem{Namespace: x.inv.Namespace.Slug, ID: w.ID.String(), Kind: kind, Type: w.Type, Version: deref(w.Version),
					Host: x.hostname(w.HostID), Project: project, Env: env}
				for _, ep := range x.endpointsOf(w.ID) {
					if ep.Kind == model.EndpointKindDomain {
						item.Domains = append(item.Domains, ep.Address)
					}
				}
				items = append(items, item)
			}
		}
		for _, c := range items {
			if in.keeps(x, c.Project, c.Env) && in.contains(append([]string{c.Type, c.Instance, c.Host}, c.Domains...)...) {
				out.Components = append(out.Components, c)
			}
		}
	}
	slices.SortFunc(out.Components, func(a, b ComponentItem) int {
		return cmp.Or(cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Type, b.Type), cmp.Compare(a.Project, b.Project), cmp.Compare(a.Env, b.Env), cmp.Compare(a.Host, b.Host))
	})
	out.Components, out.Truncated = truncate(out.Components, limit(in.Limit))
	return nil, out, nil
}

type HostItem struct {
	Namespace  string    `json:"namespace"`
	ID         string    `json:"id"`
	Hostname   string    `json:"hostname"`
	Project    string    `json:"project,omitempty"`
	Env        string    `json:"env,omitempty"`
	OS         string    `json:"os,omitempty"`
	Status     string    `json:"status" jsonschema:"active, stale or inactive, by the last snapshot of its agent"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Apps       int       `json:"apps"`
}

type ListHostsOutput struct {
	Hosts     []HostItem `json:"hosts"`
	Truncated bool       `json:"truncated,omitempty"`
}

func (s *server) listHosts(ctx context.Context, req *mcp.CallToolRequest, in ListInput) (*mcp.CallToolResult, ListHostsOutput, error) {
	out := ListHostsOutput{Hosts: []HostItem{}}
	indexes, err := s.indexes(ctx, req, in.Namespace)
	if err != nil {
		return nil, out, err
	}
	now := s.clock.Now()
	for _, x := range indexes {
		apps := map[uuid.UUID]int{}
		for _, a := range x.inv.Apps {
			apps[a.HostID]++
		}
		for i := range x.inv.Hosts {
			h := &x.inv.Hosts[i]
			project, env := x.project(h.ProjectID), x.env(h.EnvironmentID)
			if !in.keeps(x, project, env) || !in.contains(h.Hostname) {
				continue
			}
			out.Hosts = append(out.Hosts, HostItem{Namespace: x.inv.Namespace.Slug, ID: h.ID.String(), Hostname: h.Hostname, Project: project, Env: env,
				OS: osName(h.OS), Status: hostStatus(&x.inv.Namespace, h, now), LastSeenAt: h.LastSeenAt, Apps: apps[h.ID]})
		}
	}
	slices.SortFunc(out.Hosts, func(a, b HostItem) int {
		return cmp.Or(cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Hostname, b.Hostname))
	})
	out.Hosts, out.Truncated = truncate(out.Hosts, limit(in.Limit))
	return nil, out, nil
}

type UsedByInput struct {
	Target    string `json:"target" jsonschema:"what is used: the slug or name of a project, the name or id of an app, the type (mysql, redis…) or id of a service, a hostname, the id of a webserver"`
	Kind      string `json:"kind,omitempty" jsonschema:"project, app, service, host or webserver, when the target could be several; guessed otherwise"`
	Project   string `json:"project,omitempty" jsonschema:"slug or name of the project of the app or the service, when several share the name"`
	Env       string `json:"env,omitempty" jsonschema:"slug of the environment of the app or the service"`
	Namespace string `json:"namespace,omitempty" jsonschema:"slug of the namespace; required when the user reads several"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum number of results, 100 by default"`
}

// Consumer is an app using the target, through the proxies of Via.
type Consumer struct {
	From       End      `json:"from" jsonschema:"the app using the target, with its host, project and env"`
	To         End      `json:"to" jsonschema:"what of the target it uses"`
	Key        string   `json:"key,omitempty" jsonschema:"the dependency of the configuration the link comes from"`
	Confidence string   `json:"confidence" jsonschema:"exact, inferred or ambiguous; the weakest of the links crossed"`
	Via        []string `json:"via,omitempty" jsonschema:"the webservers crossed, in order, as type@hostname"`
}

type UsedByOutput struct {
	Target    string     `json:"target"`
	Consumers []Consumer `json:"consumers"`
	Truncated bool       `json:"truncated,omitempty"`
}

func (s *server) usedBy(ctx context.Context, req *mcp.CallToolRequest, in UsedByInput) (*mcp.CallToolResult, UsedByOutput, error) {
	out := UsedByOutput{Consumers: []Consumer{}}
	x, err := s.indexOf(ctx, req, in.Namespace)
	if err != nil {
		return nil, out, err
	}
	target, ids, inside, err := x.resolveTarget(in)
	if err != nil {
		return nil, out, err
	}
	out.Target = target
	seen := map[string]bool{}
	for _, id := range ids {
		for _, p := range x.paths.UsedBy(id) {
			first, last := p.First(), p.Last()
			if inside(first.FromID) {
				continue
			}
			c := x.consumer(p)
			key := first.FromID.String() + "\x00" + last.ToID.String() + "\x00" + c.Key
			if !seen[key] {
				seen[key] = true
				out.Consumers = append(out.Consumers, c)
			}
		}
	}
	slices.SortFunc(out.Consumers, func(a, b Consumer) int {
		return cmp.Or(cmp.Compare(a.From.Project, b.From.Project), cmp.Compare(a.From.Env, b.From.Env), cmp.Compare(a.From.Name, b.From.Name),
			cmp.Compare(a.From.Host, b.From.Host), cmp.Compare(a.To.Name, b.To.Name), cmp.Compare(a.Key, b.Key))
	})
	out.Consumers, out.Truncated = truncate(out.Consumers, limit(in.Limit))
	return nil, out, nil
}

func (x *index) consumer(p linkpath.Path) Consumer {
	first, last := p.First(), p.Last()
	c := Consumer{From: x.end(first.FromType, &first.FromID, nil), To: x.end(last.ToType, last.ToID, last.ExternalTarget), Confidence: p.Confidence()}
	var d struct {
		Key string `json:"key"`
	}
	_ = json.Unmarshal(first.Detail, &d)
	c.Key = d.Key
	for _, l := range p.Links[1:] {
		e := x.end(l.FromType, &l.FromID, nil)
		c.Via = append(c.Via, e.Name+"@"+e.Host)
	}
	return c
}

// resolveTarget gives the entities of a target, and what is inside it: a
// project or a host using itself is no consumer.
func (x *index) resolveTarget(in UsedByInput) (string, []uuid.UUID, func(uuid.UUID) bool, error) {
	none := func(uuid.UUID) bool { return false }
	kind, ref := strings.ToLower(in.Kind), strings.TrimSpace(in.Target)
	if ref == "" {
		return "", nil, none, fmt.Errorf("the target is required")
	}
	is := func(k string) bool { return kind == "" || kind == k }
	place := ListInput{Project: in.Project, Env: in.Env}
	if id, err := uuid.Parse(ref); err == nil {
		switch {
		case x.apps[id] != nil && is("app"), x.services[id] != nil && is("service"), x.webservers[id] != nil && is("webserver"):
			return ref, []uuid.UUID{id}, func(v uuid.UUID) bool { return v == id }, nil
		}
	}
	if h := x.findHost(ref); h != nil && is("host") {
		ids := x.entitiesOf(h)
		var list []uuid.UUID
		for id := range ids {
			list = append(list, id)
		}
		return "host " + h.Hostname, list, func(v uuid.UUID) bool { return ids[v] }, nil
	}
	if p := x.findProject(ref); p != nil && is("project") {
		ids := map[uuid.UUID]bool{}
		for _, a := range x.inv.Apps {
			if project, env := x.placeOf(a.HostID, a.ProjectID, a.EnvironmentID); project == p.Slug && place.keeps(x, project, env) {
				ids[a.ID] = true
			}
		}
		for _, list := range [][]uuid.UUID{x.servicesOfProject(p.ID), x.webserversOfProject(p.ID)} {
			for _, id := range list {
				ids[id] = true
			}
		}
		var list []uuid.UUID
		for id := range ids {
			list = append(list, id)
		}
		inside := func(v uuid.UUID) bool {
			if a := x.apps[v]; a != nil {
				project, _ := x.placeOf(a.HostID, a.ProjectID, a.EnvironmentID)
				return project == p.Slug
			}
			return false
		}
		return "project " + p.Slug, list, inside, nil
	}
	var ids []uuid.UUID
	if is("app") {
		for _, a := range x.findApps(ref, nil) {
			if project, env := x.placeOf(a.HostID, a.ProjectID, a.EnvironmentID); place.keeps(x, project, env) {
				ids = append(ids, a.ID)
			}
		}
		if len(ids) > 0 {
			name := ids
			return "app " + ref, ids, func(v uuid.UUID) bool { return slices.Contains(name, v) }, nil
		}
	}
	if is("service") {
		for _, c := range x.inv.Services {
			project, env := x.placeOf(c.HostID, nil, nil)
			if (strings.EqualFold(c.Type, ref) || strings.EqualFold(serviceName(&c), ref)) && place.keeps(x, project, env) {
				ids = append(ids, c.ID)
			}
		}
		if len(ids) > 0 {
			return "service " + ref, ids, none, nil
		}
	}
	return "", nil, none, fmt.Errorf("no project, app, service, host or webserver %q in the namespace %s; use search", ref, x.inv.Namespace.Slug)
}

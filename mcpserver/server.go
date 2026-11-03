// Package mcpserver is the MCP server of Lacis: read-only tools through
// which an assistant such as Claude reads the inventory, with the rights of
// the user who authorized it.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	lacisAuth "github.com/alexandreh2ag/lacis/auth"
	"github.com/alexandreh2ag/lacis/common/clock"
	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/alexandreh2ag/lacis/techversion"
	"github.com/alexandreh2ag/lacis/topology"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// PrincipalKey is the key of the principal in the extra of the token
// info.
const PrincipalKey = "principal"

const (
	// DefaultLimit and MaxLimit bound the lists the tools return.
	DefaultLimit = 100
	MaxLimit     = 1000
	// DefaultHistoryLimit is the number of events history returns by
	// default.
	DefaultHistoryLimit = 50
)

const instructions = `Lacis is an infrastructure inventory: agents on the hosts report their OS, runtimes (php, node…),
services (mysql, redis…), webservers (nginx, traefik…), apps and containers; Lacis links them (who uses what) and keeps a
history of their changes. The inventory is split in namespaces, then projects and environments.
Start with list_namespaces when the namespace is unknown. Use search to find an entity by name, domain or IP, then
get_host or get_app for its details, and links for who uses it or what it uses. Use find_by_version for questions such as
"who runs PHP below 8.1" and list_versions for the spread of the versions of a technology. Everything is read-only.`

// Services are what the tools read.
type Services struct {
	Namespace service.NamespaceService
	Graph     service.GraphService
	History   service.HistoryService
}

type server struct {
	services Services
	clock    clock.Clock
	logger   *slog.Logger
	maxNodes int
}

// errInternal is what the client is told of an error of Lacis, the error
// itself being logged: it may hold SQL or the inner workings.
var errInternal = errors.New("internal error of Lacis")

// internalError marks an error of a service, hidden from the client.
type internalError struct{ err error }

func (e *internalError) Error() string { return e.err.Error() }
func (e *internalError) Unwrap() error { return e.err }

func internal(err error) error { return &internalError{err: err} }

// add adds a tool whose calls are logged with their user, and whose
// internal errors are hidden.
func add[In, Out any](s *server, srv *mcp.Server, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	mcp.AddTool(srv, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		start := time.Now()
		res, out, err := h(ctx, req, in)
		user := ""
		if p, perr := principal(req); perr == nil {
			user = p.User.Username
		}
		attrs := []any{"tool", t.Name, "user", user, "duration", time.Since(start)}
		var ie *internalError
		switch {
		case errors.As(err, &ie):
			s.logger.Error("MCP tool failed", append(attrs, "error", ie.err)...)
			return nil, out, errInternal
		case err != nil:
			s.logger.Info("MCP tool call refused", append(attrs, "error", err)...)
		default:
			s.logger.Info("MCP tool call", attrs...)
		}
		return res, out, err
	})
}

// New returns the MCP server; version is the one of Lacis.
func New(services Services, clk clock.Clock, logger *slog.Logger, cfg *config.Config, version string) *mcp.Server {
	s := &server{services: services, clock: clk, logger: logger, maxNodes: cfg.Graph.MaxNodes}
	srv := mcp.NewServer(&mcp.Implementation{Name: "lacis", Title: "Lacis", Version: version}, &mcp.ServerOptions{Instructions: instructions})
	readOnly := func(title string) *mcp.ToolAnnotations {
		return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(false)}
	}
	add(s, srv, &mcp.Tool{Name: "list_namespaces", Annotations: readOnly("List the namespaces"),
		Description: "Lists the namespaces of the inventory the user may read, with their projects and environments."}, s.listNamespaces)
	add(s, srv, &mcp.Tool{Name: "search", Annotations: readOnly("Search the inventory"),
		Description: "Finds hosts, apps, services, webservers and containers whose name, path, domain, vhost, image or IP contains the query, case-insensitive."}, s.search)
	add(s, srv, &mcp.Tool{Name: "get_host", Annotations: readOnly("Get a host"),
		Description: "Gives a host by hostname or id: OS, kernel, resources, IPs, runtimes, services, webservers, apps and containers."}, s.getHost)
	add(s, srv, &mcp.Tool{Name: "get_app", Annotations: readOnly("Get an app"),
		Description: "Gives an app by id, name or path: project, env, framework, runtime, vhosts, the dependencies of its configuration, what it uses and who uses it."}, s.getApp)
	add(s, srv, &mcp.Tool{Name: "links", Annotations: readOnly("Links of an entity"),
		Description: "Lists the links of a host (and of everything on it) or of an app, service or webserver: who uses it (dependents), what it uses (dependencies), or both. Use it for impact questions, e.g. what breaks if a database host stops."}, s.links)
	add(s, srv, &mcp.Tool{Name: "find_by_version", Annotations: readOnly("Find by version"),
		Description: "Finds who uses a technology (php, node, mysql, nginx, symfony…) as a runtime installed on a host, the runtime of an app, a framework, a service, a webserver or a container image, optionally in a version compared with an operator, e.g. php lt 8.1. Versions compare number by number, cut to the parts given: eq 7.4 takes 7.4.33."}, s.findByVersion)
	add(s, srv, &mcp.Tool{Name: "list_versions", Annotations: readOnly("Versions in use"),
		Description: "Counts the versions in use (as major.minor) of every technology, or of one, by kind of use."}, s.listVersions)
	add(s, srv, &mcp.Tool{Name: "graph", Annotations: readOnly("Draw the graph"),
		Description: "Draws the graph of a namespace as Mermaid, at a level: project (projects and envs), host, app or service; filtered by projects, envs or hosts."}, s.graph)
	add(s, srv, &mcp.Tool{Name: "history", Annotations: readOnly("History of the changes"),
		Description: "Lists the changes of the inventory of a namespace, latest first: hosts, runtimes, services, webservers, apps and links added, removed or changed."}, s.history)
	return srv
}

// principal is the user of the access token of the request.
func principal(req *mcp.CallToolRequest) (*lacisAuth.Principal, error) {
	var info *auth.TokenInfo
	if req != nil && req.Extra != nil {
		info = req.Extra.TokenInfo
	}
	if info == nil {
		return nil, errors.New("not authenticated")
	}
	p, ok := info.Extra[PrincipalKey].(*lacisAuth.Principal)
	if !ok || p == nil {
		return nil, errors.New("not authenticated")
	}
	return p, nil
}

// readable are the namespaces the user may read, by slug.
func (s *server) readable(ctx context.Context, p *lacisAuth.Principal) ([]model.Namespace, error) {
	all, err := s.services.Namespace.List(ctx)
	if err != nil {
		return nil, internal(err)
	}
	var out []model.Namespace
	for _, ns := range all {
		if p.Can(ns.ID, model.ResourceInventory, model.ActionRead) {
			out = append(out, ns)
		}
	}
	slices.SortFunc(out, func(a, b model.Namespace) int { return strings.Compare(a.Slug, b.Slug) })
	return out, nil
}

func slugs(namespaces []model.Namespace) string {
	out := make([]string, len(namespaces))
	for i, ns := range namespaces {
		out[i] = ns.Slug
	}
	return strings.Join(out, ", ")
}

// namespaces are the namespace of slug, or every one readable when slug
// is empty. An unknown namespace and one the user may not read give the
// same error.
func (s *server) namespaces(ctx context.Context, p *lacisAuth.Principal, slug string) ([]model.Namespace, error) {
	readable, err := s.readable(ctx, p)
	if err != nil {
		return nil, err
	}
	if slug == "" {
		return readable, nil
	}
	for _, ns := range readable {
		if strings.EqualFold(ns.Slug, slug) {
			return []model.Namespace{ns}, nil
		}
	}
	return nil, fmt.Errorf("unknown namespace %q; the namespaces are: %s", slug, slugs(readable))
}

// namespace is the namespace of slug; without slug, the only one the user
// may read.
func (s *server) namespace(ctx context.Context, p *lacisAuth.Principal, slug string) (*model.Namespace, error) {
	namespaces, err := s.namespaces(ctx, p, slug)
	if err != nil {
		return nil, err
	}
	switch len(namespaces) {
	case 0:
		return nil, errors.New("no namespace is readable")
	case 1:
		return &namespaces[0], nil
	}
	return nil, fmt.Errorf("give the namespace, one of: %s", slugs(namespaces))
}

// index loads the inventory of a namespace.
func (s *server) index(ctx context.Context, ns *model.Namespace) (*index, error) {
	inv, err := s.services.Namespace.Inventory(ctx, ns.ID)
	if err != nil {
		return nil, internal(err)
	}
	inv.Namespace = *ns
	return newIndex(inv), nil
}

// indexOf is the index of the namespace of slug, for the principal of req.
func (s *server) indexOf(ctx context.Context, req *mcp.CallToolRequest, slug string) (*index, error) {
	p, err := principal(req)
	if err != nil {
		return nil, err
	}
	ns, err := s.namespace(ctx, p, slug)
	if err != nil {
		return nil, err
	}
	return s.index(ctx, ns)
}

// indexes are the indexes of the namespace of slug, or of every one
// readable.
func (s *server) indexes(ctx context.Context, req *mcp.CallToolRequest, slug string) ([]*index, error) {
	p, err := principal(req)
	if err != nil {
		return nil, err
	}
	namespaces, err := s.namespaces(ctx, p, slug)
	if err != nil {
		return nil, err
	}
	out := make([]*index, 0, len(namespaces))
	for i := range namespaces {
		x, err := s.index(ctx, &namespaces[i])
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, nil
}

func limit(n int) int {
	switch {
	case n <= 0:
		return DefaultLimit
	case n > MaxLimit:
		return MaxLimit
	}
	return n
}

// truncate keeps the first n items, and tells whether there were more.
func truncate[T any](items []T, n int) ([]T, bool) {
	if len(items) > n {
		return items[:n], true
	}
	return items, false
}

type NoInput struct{}

type NamespaceOut struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Projects    []string `json:"projects"`
	Envs        []string `json:"envs"`
	Hosts       int      `json:"hosts"`
	Apps        int      `json:"apps"`
}

type ListNamespacesOutput struct {
	Namespaces []NamespaceOut `json:"namespaces"`
}

func (s *server) listNamespaces(ctx context.Context, req *mcp.CallToolRequest, _ NoInput) (*mcp.CallToolResult, ListNamespacesOutput, error) {
	out := ListNamespacesOutput{Namespaces: []NamespaceOut{}}
	indexes, err := s.indexes(ctx, req, "")
	if err != nil {
		return nil, out, err
	}
	for _, x := range indexes {
		ns := x.inv.Namespace
		n := NamespaceOut{Slug: ns.Slug, Name: ns.Name, Description: deref(ns.Description), Projects: []string{}, Envs: []string{},
			Hosts: len(x.inv.Hosts), Apps: len(x.inv.Apps)}
		for _, p := range x.inv.Projects {
			n.Projects = append(n.Projects, p.Slug)
		}
		envs := slices.Clone(x.inv.Environments)
		slices.SortStableFunc(envs, func(a, b model.Environment) int { return a.SortOrder - b.SortOrder })
		for _, e := range envs {
			n.Envs = append(n.Envs, e.Slug)
		}
		slices.Sort(n.Projects)
		out.Namespaces = append(out.Namespaces, n)
	}
	return nil, out, nil
}

type SearchInput struct {
	Query     string `json:"query" jsonschema:"text to look for: a hostname, an app name or path, a domain, an IP, a service or image name"`
	Namespace string `json:"namespace,omitempty" jsonschema:"slug of the namespace; every namespace readable when empty"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum number of results, 100 by default"`
}

type SearchOutput struct {
	Results   []Hit `json:"results"`
	Truncated bool  `json:"truncated,omitempty"`
}

func (s *server) search(ctx context.Context, req *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
	out := SearchOutput{Results: []Hit{}}
	if strings.TrimSpace(in.Query) == "" {
		return nil, out, errors.New("the query is required")
	}
	indexes, err := s.indexes(ctx, req, in.Namespace)
	if err != nil {
		return nil, out, err
	}
	for _, x := range indexes {
		out.Results = append(out.Results, x.search(in.Query)...)
	}
	out.Results, out.Truncated = truncate(out.Results, limit(in.Limit))
	return nil, out, nil
}

type GetHostInput struct {
	Host      string `json:"host" jsonschema:"hostname or id of the host"`
	Namespace string `json:"namespace,omitempty" jsonschema:"slug of the namespace; required when the user reads several"`
}

func (s *server) getHost(ctx context.Context, req *mcp.CallToolRequest, in GetHostInput) (*mcp.CallToolResult, HostOut, error) {
	x, err := s.indexOf(ctx, req, in.Namespace)
	if err != nil {
		return nil, HostOut{}, err
	}
	h := x.findHost(in.Host)
	if h == nil {
		return nil, HostOut{}, fmt.Errorf("no host %q in the namespace %s; use search", in.Host, x.inv.Namespace.Slug)
	}
	return nil, x.host(h), nil
}

type GetAppInput struct {
	App       string `json:"app" jsonschema:"id, name or path of the app"`
	Host      string `json:"host,omitempty" jsonschema:"hostname of the app, when several apps share the name"`
	Namespace string `json:"namespace,omitempty" jsonschema:"slug of the namespace; required when the user reads several"`
}

func (s *server) getApp(ctx context.Context, req *mcp.CallToolRequest, in GetAppInput) (*mcp.CallToolResult, AppOut, error) {
	x, err := s.indexOf(ctx, req, in.Namespace)
	if err != nil {
		return nil, AppOut{}, err
	}
	var host *model.Host
	if in.Host != "" {
		if host = x.findHost(in.Host); host == nil {
			return nil, AppOut{}, fmt.Errorf("no host %q in the namespace %s", in.Host, x.inv.Namespace.Slug)
		}
	}
	apps := x.findApps(in.App, host)
	switch len(apps) {
	case 0:
		return nil, AppOut{}, fmt.Errorf("no app %q in the namespace %s; use search", in.App, x.inv.Namespace.Slug)
	case 1:
		return nil, x.app(apps[0]), nil
	}
	candidates := make([]string, len(apps))
	for i, a := range apps {
		candidates[i] = fmt.Sprintf("%s on %s (id %s)", a.Name, x.hostname(a.HostID), a.ID)
	}
	return nil, AppOut{}, fmt.Errorf("several apps match %q, give the host or the id: %s", in.App, strings.Join(candidates, "; "))
}

type LinksInput struct {
	Target    string `json:"target" jsonschema:"hostname, or id or name of an app, or id of a service or webserver"`
	Direction string `json:"direction,omitempty" jsonschema:"dependents (who uses the target, the default), dependencies (what it uses) or both"`
	Namespace string `json:"namespace,omitempty" jsonschema:"slug of the namespace; required when the user reads several"`
}

type LinksOutput struct {
	Target string    `json:"target"`
	Links  []LinkOut `json:"links"`
}

func (s *server) links(ctx context.Context, req *mcp.CallToolRequest, in LinksInput) (*mcp.CallToolResult, LinksOutput, error) {
	out := LinksOutput{Links: []LinkOut{}}
	direction := in.Direction
	if direction == "" {
		direction = DirectionDependents
	}
	if direction != DirectionDependents && direction != DirectionDependencies && direction != DirectionBoth {
		return nil, out, fmt.Errorf("direction must be %s, %s or %s", DirectionDependents, DirectionDependencies, DirectionBoth)
	}
	x, err := s.indexOf(ctx, req, in.Namespace)
	if err != nil {
		return nil, out, err
	}
	var ids map[uuid.UUID]bool
	if h := x.findHost(in.Target); h != nil {
		ids, out.Target = x.entitiesOf(h), "host "+h.Hostname
	} else if id, err := uuid.Parse(in.Target); err == nil && (x.services[id] != nil || x.webservers[id] != nil || x.apps[id] != nil) {
		ids, out.Target = map[uuid.UUID]bool{id: true}, in.Target
	} else if apps := x.findApps(in.Target, nil); len(apps) > 0 {
		ids = map[uuid.UUID]bool{}
		for _, a := range apps {
			ids[a.ID] = true
		}
		out.Target = "app " + in.Target
	} else {
		return nil, out, fmt.Errorf("no host, app, service or webserver %q in the namespace %s; use search", in.Target, x.inv.Namespace.Slug)
	}
	if links := x.links(ids, direction); links != nil {
		out.Links = links
	}
	return nil, out, nil
}

type FindByVersionInput struct {
	Technology string `json:"technology" jsonschema:"name of the technology, e.g. php, node, python, mysql, mariadb, redis, nginx, apache, traefik, symfony, laravel"`
	Op         string `json:"op,omitempty" jsonschema:"lt, lte, eq, gte or gt; gte by default"`
	Version    string `json:"version,omitempty" jsonschema:"version to compare to, e.g. 8.1; every version when empty"`
	Namespace  string `json:"namespace,omitempty" jsonschema:"slug of the namespace; every namespace readable when empty"`
	Limit      int    `json:"limit,omitempty" jsonschema:"maximum number of results, 100 by default"`
}

type FindByVersionOutput struct {
	Count     int     `json:"count" jsonschema:"number of uses found, before the limit"`
	Usages    []Usage `json:"usages"`
	Truncated bool    `json:"truncated,omitempty"`
}

func (s *server) findByVersion(ctx context.Context, req *mcp.CallToolRequest, in FindByVersionInput) (*mcp.CallToolResult, FindByVersionOutput, error) {
	out := FindByVersionOutput{Usages: []Usage{}}
	if strings.TrimSpace(in.Technology) == "" {
		return nil, out, errors.New("the technology is required")
	}
	op := techversion.Op(strings.ToLower(in.Op))
	if op == "" {
		op = techversion.OpGte
	}
	if !slices.Contains(techversion.Ops, op) {
		return nil, out, fmt.Errorf("op must be one of lt, lte, eq, gte, gt")
	}
	indexes, err := s.indexes(ctx, req, in.Namespace)
	if err != nil {
		return nil, out, err
	}
	var usages []Usage
	for _, x := range indexes {
		usages = append(usages, filterVersions(x.usages(strings.TrimSpace(in.Technology)), op, in.Version)...)
	}
	sortUsages(usages)
	out.Count = len(usages)
	if usages != nil {
		out.Usages, out.Truncated = truncate(usages, limit(in.Limit))
	}
	return nil, out, nil
}

type ListVersionsInput struct {
	Technology string `json:"technology,omitempty" jsonschema:"name of the technology; every one when empty"`
	Namespace  string `json:"namespace,omitempty" jsonschema:"slug of the namespace; every namespace readable when empty"`
}

type ListVersionsOutput struct {
	Technologies []TechnologyVersions `json:"technologies"`
}

func (s *server) listVersions(ctx context.Context, req *mcp.CallToolRequest, in ListVersionsInput) (*mcp.CallToolResult, ListVersionsOutput, error) {
	out := ListVersionsOutput{Technologies: []TechnologyVersions{}}
	indexes, err := s.indexes(ctx, req, in.Namespace)
	if err != nil {
		return nil, out, err
	}
	var usages []Usage
	for _, x := range indexes {
		usages = append(usages, x.usages(strings.TrimSpace(in.Technology))...)
	}
	if v := versions(usages); v != nil {
		out.Technologies = v
	}
	return nil, out, nil
}

type GraphInput struct {
	Namespace string   `json:"namespace,omitempty" jsonschema:"slug of the namespace; required when the user reads several"`
	Level     string   `json:"level,omitempty" jsonschema:"project (the default), host, app or service"`
	Projects  []string `json:"projects,omitempty" jsonschema:"slugs of the projects to keep"`
	Envs      []string `json:"envs,omitempty" jsonschema:"slugs of the environments to keep"`
	Hosts     []string `json:"hosts,omitempty" jsonschema:"hostnames to keep"`
}

type GraphOutput struct {
	Mermaid string `json:"mermaid"`
	Nodes   int    `json:"nodes"`
	Edges   int    `json:"edges"`
}

func (s *server) graph(ctx context.Context, req *mcp.CallToolRequest, in GraphInput) (*mcp.CallToolResult, GraphOutput, error) {
	level := topology.Level(in.Level)
	if level == "" {
		level = topology.LevelProject
	}
	if !slices.Contains(topology.Levels, level) {
		return nil, GraphOutput{}, fmt.Errorf("level must be project, host, app or service")
	}
	p, err := principal(req)
	if err != nil {
		return nil, GraphOutput{}, err
	}
	ns, err := s.namespace(ctx, p, in.Namespace)
	if err != nil {
		return nil, GraphOutput{}, err
	}
	g, err := s.services.Graph.Graph(ctx, ns.Slug, topology.Filter{Level: level, Projects: in.Projects, Envs: in.Envs, Hosts: in.Hosts})
	if err != nil {
		return nil, GraphOutput{}, internal(err)
	}
	if s.maxNodes > 0 && len(g.Nodes) > s.maxNodes {
		return nil, GraphOutput{}, fmt.Errorf("the graph holds %d nodes, more than %d: filter it by projects, envs or hosts, or take a coarser level", len(g.Nodes), s.maxNodes)
	}
	var b strings.Builder
	if err := topology.Render(&b, g, topology.FormatMermaid, topology.Options{Labels: true}); err != nil {
		return nil, GraphOutput{}, internal(err)
	}
	return nil, GraphOutput{Mermaid: b.String(), Nodes: len(g.Nodes), Edges: len(g.Edges)}, nil
}

type HistoryInput struct {
	Namespace  string `json:"namespace,omitempty" jsonschema:"slug of the namespace; required when the user reads several"`
	Host       string `json:"host,omitempty" jsonschema:"hostname whose changes to keep"`
	EntityType string `json:"entity_type,omitempty" jsonschema:"host, runtime, service, webserver, app or link"`
	Since      string `json:"since,omitempty" jsonschema:"keeps the changes from then on: a date (2026-10-01 or RFC 3339) or a duration back from now (7d, 12h)"`
	Limit      int    `json:"limit,omitempty" jsonschema:"maximum number of changes, 50 by default"`
}

// Change is a change of the inventory.
type Change struct {
	OccurredAt time.Time `json:"occurred_at"`
	Kind       string    `json:"kind" jsonschema:"added, removed or changed"`
	EntityType string    `json:"entity_type"`
	EntityID   string    `json:"entity_id,omitempty"`
	Host       string    `json:"host,omitempty"`
	Before     any       `json:"before,omitempty"`
	After      any       `json:"after,omitempty"`
}

type HistoryOutput struct {
	Changes []Change `json:"changes"`
	HasMore bool     `json:"has_more,omitempty"`
}

// since reads a date, or a duration back from now.
func since(value string, now time.Time) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t, nil
		}
	}
	d, err := config.ParseDuration(value)
	if err != nil || d < 0 {
		return time.Time{}, fmt.Errorf("since %q: a date (2026-10-01) or a duration (7d, 12h) is expected", value)
	}
	return now.Add(-d), nil
}

func (s *server) history(ctx context.Context, req *mcp.CallToolRequest, in HistoryInput) (*mcp.CallToolResult, HistoryOutput, error) {
	out := HistoryOutput{Changes: []Change{}}
	from, err := since(in.Since, s.clock.Now())
	if err != nil {
		return nil, out, err
	}
	p, err := principal(req)
	if err != nil {
		return nil, out, err
	}
	ns, err := s.namespace(ctx, p, in.Namespace)
	if err != nil {
		return nil, out, err
	}
	n := in.Limit
	if n <= 0 {
		n = DefaultHistoryLimit
	}
	n = limit(n)
	rows, err := s.services.History.List(ctx, ns.Slug, service.HistoryFilter{Hostname: in.Host, EntityType: in.EntityType, Since: from, Limit: n + 1})
	if err != nil {
		return nil, out, internal(err)
	}
	rows, out.HasMore = truncate(rows, n)
	for _, r := range rows {
		c := Change{OccurredAt: r.OccurredAt, Kind: r.Kind, EntityType: r.EntityType, Host: deref(r.Hostname)}
		if r.EntityID != nil {
			c.EntityID = r.EntityID.String()
		}
		if len(r.Before) > 0 {
			c.Before = r.Before
		}
		if len(r.After) > 0 {
			c.After = r.After
		}
		out.Changes = append(out.Changes, c)
	}
	return nil, out, nil
}

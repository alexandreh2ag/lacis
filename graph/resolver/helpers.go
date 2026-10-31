package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/alexandreh2ag/lacis/graph"
	"github.com/alexandreh2ag/lacis/graph/union"
	"github.com/alexandreh2ag/lacis/linker"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/google/uuid"
)

// inScope tells a row whose project and env are among the slugs; an empty
// list keeps all.
func (x *index) inScope(projectID, envID *uuid.UUID, projects, envs []string) bool {
	if len(projects) > 0 && (projectID == nil || x.projects[*projectID] == nil || !slices.Contains(projects, x.projects[*projectID].Slug)) {
		return false
	}
	if len(envs) > 0 && (envID == nil || x.envs[*envID] == nil || !slices.Contains(envs, x.envs[*envID].Slug)) {
		return false
	}
	return true
}

// matches tells whether one of the fields holds the search, whatever the
// case; no search matches all.
func matches(search *string, fields ...string) bool {
	if search == nil || strings.TrimSpace(*search) == "" {
		return true
	}
	s := strings.ToLower(strings.TrimSpace(*search))
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), s) {
			return true
		}
	}
	return false
}

// status is the freshness of a host, by the thresholds of its namespace.
func status(ns *model.Namespace, h *model.Host, now time.Time) graph.HostStatus {
	age := now.Sub(h.LastSeenAt)
	switch {
	case age > ns.InactiveAfter.Duration():
		return graph.HostStatusInactive
	case age > ns.StaleAfter.Duration():
		return graph.HostStatusStale
	}
	return graph.HostStatusActive
}

// history returns a page of the change events, and whether more follow.
func (r *Resolver) history(ctx context.Context, namespace string, f service.HistoryFilter, page *graph.PageInput) (*graph.ChangeEventList, error) {
	offset, limit := bounds(page)
	f.Offset, f.Limit = offset, limit+1
	rows, err := r.HistoryService.List(ctx, namespace, f)
	if err != nil {
		return nil, err
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	return &graph.ChangeEventList{Items: rows, HasMore: more}, nil
}

// detail decodes the detail of a link.
func detail(l *model.Link) linker.Detail {
	var d linker.Detail
	_ = json.Unmarshal(l.Detail, &d)
	return d
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// host returns a host of the namespace.
func (r *Resolver) host(ctx context.Context, namespaceID, hostID uuid.UUID) (*model.Host, error) {
	x, err := r.index(ctx, namespaceID)
	if err != nil {
		return nil, err
	}
	h, ok := x.hosts[hostID]
	if !ok {
		return nil, fmt.Errorf("host %s not found", hostID)
	}
	return h, nil
}

// end returns an end of a link: the app, service, webserver or host it
// names, or the external target.
func (r *Resolver) end(ctx context.Context, l *model.Link, kind string, id *uuid.UUID) (union.LinkEnd, error) {
	if kind == model.LinkNodeExternal || id == nil {
		target := ""
		if l.ExternalTarget != nil {
			target = *l.ExternalTarget
		}
		return &graph.External{Target: target, Reason: optional(detail(l).Reason)}, nil
	}
	x, err := r.index(ctx, l.NamespaceID)
	if err != nil {
		return nil, err
	}
	end := x.entity(kind, *id)
	if end == nil {
		return nil, fmt.Errorf("link %s: %s %s not found", l.ID, kind, id)
	}
	return end, nil
}

// entity is the app, service, webserver or host of the id; nil when
// unknown.
func (x *index) entity(kind string, id uuid.UUID) union.LinkEnd {
	switch kind {
	case model.LinkNodeApp:
		if a, ok := x.apps[id]; ok {
			return a
		}
	case model.LinkNodeService:
		if s, ok := x.services[id]; ok {
			return s
		}
	case model.LinkNodeWebserver:
		if w, ok := x.webservers[id]; ok {
			return w
		}
	case model.LinkNodeHost:
		if h, ok := x.hosts[id]; ok {
			return h
		}
	}
	return nil
}

// seconds reads a number of seconds of the API.
func seconds(s *int) *time.Duration {
	if s == nil {
		return nil
	}
	d := time.Duration(*s) * time.Second
	return &d
}

// roleInput reads a role of the API; its permissions are replaced when one
// of their lists is given.
func roleInput(in graph.RoleInput) service.RoleInput {
	out := service.RoleInput{Code: in.Code, Description: deref(in.Description), OIDCGroups: in.OidcGroups}
	if in.Permissions == nil && in.GlobalPermissions == nil {
		return out
	}
	perms := &service.RolePermissions{}
	for _, p := range in.Permissions {
		perms.Namespace = append(perms.Namespace, model.NamespacePermission{NamespaceID: p.Namespace, Resource: p.Resource, Action: p.Action})
	}
	for _, p := range in.GlobalPermissions {
		perms.Global = append(perms.Global, model.GlobalPermission{Resource: p.Resource, Action: p.Action})
	}
	out.Permissions = perms
	return out
}

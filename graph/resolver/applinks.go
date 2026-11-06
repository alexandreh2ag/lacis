package resolver

import (
	"encoding/base64"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/domainmatch"
	"github.com/alexandreh2ag/lacis/graph"
	"github.com/alexandreh2ag/lacis/graph/union"
	"github.com/alexandreh2ag/lacis/linker"
	"github.com/alexandreh2ag/lacis/linkpath"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
)

// The effective links are given whatever the hosts: an app is its name in
// its project and env, the unicity that holds, its deployments are one.
// The hosts stay in the steps of the routes, as an information.

// sep joins the parts of an identity and of the id of an app link.
const sep = linkpath.Sep

// identity names an end of a link whatever its host.
func (x *index) identity(kind string, id *uuid.UUID, external *string) string {
	return x.ids.Of(kind, id, external)
}

// groups merges effective links into app links: one per consumer,
// dependency and thing used, whatever their hosts.
func (x *index) groups(paths []linkpath.Path) []linkpath.Group {
	return linkpath.GroupBy(paths, func(p linkpath.Path) string {
		first, last := p.First(), p.Last()
		return strings.Join([]string{x.identity(first.FromType, &first.FromID, nil), detail(first).Key,
			x.identity(last.ToType, last.ToID, last.ExternalTarget)}, sep+sep)
	})
}

// appUses are the app links of all the deployments of an app.
func (x *index) appUses(a *model.App) []linkpath.Group {
	var paths []linkpath.Path
	for _, d := range x.deploymentsOf(a) {
		paths = append(paths, x.paths.Uses(d.ID)...)
	}
	return orEmpty(x.groups(paths))
}

// appUsedBy are the app links to all the deployments of an app.
func (x *index) appUsedBy(a *model.App) []linkpath.Group {
	var paths []linkpath.Path
	for _, d := range x.deploymentsOf(a) {
		paths = append(paths, x.paths.UsedBy(d.ID)...)
	}
	return orEmpty(x.groups(paths))
}

// encodeID and decodeID give the id of an app link as the API carries it.
func encodeID(id string) string { return base64.RawURLEncoding.EncodeToString([]byte(id)) }

func decodeID(s string) (from, key, to string, ok bool) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "", "", "", false
	}
	parts := strings.Split(string(b), sep+sep)
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// linkRoutes are the routes of app links by their ids: the effective links
// of the apps of the consumer, by the dependency and to the thing used.
func (x *index) linkRoutes(ids []string) []graph.LinkRoute {
	var paths []linkpath.Path
	done := map[string]bool{}
	for _, id := range ids {
		from, key, to, ok := decodeID(id)
		if !ok || done[id] {
			continue
		}
		done[id] = true
		for i := range x.inv.Apps {
			a := &x.inv.Apps[i]
			if x.identity(model.LinkNodeApp, &a.ID, nil) != from {
				continue
			}
			for _, p := range x.paths.Uses(a.ID) {
				last := p.Last()
				if detail(p.First()).Key == key && x.identity(last.ToType, last.ToID, last.ExternalTarget) == to {
					paths = append(paths, p)
				}
			}
		}
	}
	return x.routes(paths)
}

// routes are the routes of effective links, step by step, the routes
// through the same nodes merged: their hosts and details add up.
func (x *index) routes(paths []linkpath.Path) []graph.LinkRoute {
	type step struct {
		graph.RouteStep
		confidence string
	}
	var order []string
	merged := map[string][]step{}
	for _, p := range paths {
		first := p.First()
		d := detail(first)
		name := domainmatch.NameOf(d.Target)
		steps := []step{{RouteStep: graph.RouteStep{Node: x.endOf(first.FromType, &first.FromID, nil, first), Hosts: x.hostsOf(first.FromType, &first.FromID)}}}
		sig := []string{x.identity(first.FromType, &first.FromID, nil)}
		for i, l := range p.Links {
			ld := detail(l)
			s := step{RouteStep: graph.RouteStep{Node: x.endOf(l.ToType, l.ToID, l.ExternalTarget, l), Hosts: x.hostsOf(l.ToType, l.ToID),
				Rule: new(ruleOf(ld)), Details: []string{}, Names: []string{}}, confidence: l.Confidence}
			switch {
			case i == 0 && d.Key != "" && d.Target != "":
				s.Details = append(s.Details, d.Key+" = "+d.Target+d.Path)
			case i == 0:
				s.Details = appendNew(s.Details, d.Key, d.Target+d.Path)
			case len(ld.Paths) > 0:
				s.Details = appendNew(s.Details, strings.Join(ld.Routes, ", ")+" ("+strings.Join(ld.Paths, ", ")+")")
			default:
				s.Details = appendNew(s.Details, strings.Join(ld.Routes, ", "))
			}
			if i > 0 && name != "" {
				for _, dom := range ld.Domains {
					if domainmatch.Match(dom, name) {
						s.Names = appendNew(s.Names, dom)
					}
				}
			}
			steps = append(steps, s)
			sig = append(sig, x.identity(l.ToType, l.ToID, l.ExternalTarget)+sep+*s.Rule)
		}
		key := strings.Join(sig, sep+sep)
		cur, ok := merged[key]
		if !ok {
			order = append(order, key)
			merged[key] = steps
			continue
		}
		for i, s := range steps {
			cur[i].Hosts = appendNew(cur[i].Hosts, s.Hosts...)
			cur[i].Details = appendNew(cur[i].Details, s.Details...)
			cur[i].Names = appendNew(cur[i].Names, s.Names...)
			if linkRank[s.confidence] > linkRank[cur[i].confidence] {
				cur[i].confidence = s.confidence
			}
		}
	}
	out := make([]graph.LinkRoute, 0, len(order))
	for _, k := range order {
		route := graph.LinkRoute{Steps: make([]graph.RouteStep, 0, len(merged[k]))}
		for _, s := range merged[k] {
			slices.SortFunc(s.Hosts, natural)
			if s.confidence != "" {
				s.Confidence = new(graph.LinkConfidence(strings.ToUpper(s.confidence)))
			}
			if s.Details == nil {
				s.Details = []string{}
			}
			if s.Names == nil {
				s.Names = []string{}
			}
			route.Steps = append(route.Steps, s.RouteStep)
		}
		out = append(out, route)
	}
	return out
}

var linkRank = map[string]int{model.LinkConfidenceExact: 0, model.LinkConfidenceInferred: 1, model.LinkConfidenceAmbiguous: 2}

// endOf is the end of a link the API gives: the entity, or the external
// target with the reason of the link.
func (x *index) endOf(kind string, id *uuid.UUID, external *string, l *model.Link) union.LinkEnd {
	if id != nil {
		if e := x.entity(kind, *id); e != nil {
			return e
		}
	}
	return &graph.External{Target: deref(external), Reason: optional(detail(l).Reason)}
}

// hostsOf are the hostnames of an end: its host, the host itself, none
// for an external target.
func (x *index) hostsOf(kind string, id *uuid.UUID) []string {
	if id == nil {
		return []string{}
	}
	var hostID uuid.UUID
	switch kind {
	case model.LinkNodeApp:
		if a := x.apps[*id]; a != nil {
			hostID = a.HostID
		}
	case model.LinkNodeService:
		if s := x.services[*id]; s != nil {
			hostID = s.HostID
		}
	case model.LinkNodeWebserver:
		if w := x.webservers[*id]; w != nil {
			hostID = w.HostID
		}
	case model.LinkNodeHost:
		hostID = *id
	}
	if h := x.hosts[hostID]; h != nil {
		return []string{h.Hostname}
	}
	return []string{}
}

// ruleOf tells how a link was resolved: its rule, its reason, its CDN when
// named.
func ruleOf(d linker.Detail) string {
	parts := []string{d.Rule}
	if d.Reason != "" {
		parts = append(parts, d.Reason)
	}
	if d.CDN != "" && d.CDN != "cdn" {
		parts = append(parts, d.CDN)
	}
	return strings.Join(parts, " · ")
}

// via are the webservers the first route crossing one goes through, by
// type.
func (x *index) via(g *linkpath.Group) []string {
	for _, p := range g.Paths {
		if len(p.Links) < 2 {
			continue
		}
		out := []string{}
		for _, l := range p.Links[1:] {
			if w := x.webservers[l.FromID]; w != nil {
				out = append(out, w.Type)
			}
		}
		return out
	}
	return []string{}
}

// deadEnd tells an app link one route of which ends at a webserver.
func deadEnd(g *linkpath.Group) bool {
	return slices.ContainsFunc(g.Paths, func(p linkpath.Path) bool { return p.DeadEnd })
}

func appendNew(list []string, values ...string) []string {
	for _, v := range values {
		if v != "" && !slices.Contains(list, v) {
			list = append(list, v)
		}
	}
	return list
}

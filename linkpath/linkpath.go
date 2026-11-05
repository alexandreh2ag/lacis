// Package linkpath gives the effective links of the inventory: what an app
// uses, or what uses an app or a service, through the proxies in between.
//
// A link to a webserver goes on along its routes, as far as the name called
// leads: an app calling https://api.example through a Traefik, then the
// nginx of the backend, uses the app that nginx serves for api.example. The
// links crossed are the path of the effective link, kept for whoever wants
// the route of the requests.
package linkpath

import (
	"encoding/json"
	"slices"

	"github.com/alexandreh2ag/lacis/domainmatch"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
)

// Path is an effective link: the links crossed in order, from the consumer
// to what it uses.
type Path struct {
	Links []*model.Link
	// DeadEnd tells the path ends at a webserver whose routes lead nowhere
	// known for the name called.
	DeadEnd bool
}

// First is the link the path starts with: the dependency of the consumer.
func (p Path) First() *model.Link { return p.Links[0] }

// Last is the link reaching what is used.
func (p Path) Last() *model.Link { return p.Links[len(p.Links)-1] }

// Confidence is the weakest confidence of the links crossed.
func (p Path) Confidence() string {
	rank := map[string]int{model.LinkConfidenceExact: 0, model.LinkConfidenceInferred: 1, model.LinkConfidenceAmbiguous: 2}
	out := model.LinkConfidenceExact
	for _, l := range p.Links {
		if rank[l.Confidence] > rank[out] {
			out = l.Confidence
		}
	}
	return out
}

// Graph holds the links of a namespace by end.
type Graph struct {
	from map[uuid.UUID][]*model.Link
	to   map[uuid.UUID][]*model.Link
	// domains are the names each link routes, its target the name it calls.
	domains map[*model.Link][]string
	names   map[*model.Link]string
}

// New indexes the links of a namespace; they must not change while the
// graph is used.
func New(links []model.Link) *Graph {
	g := &Graph{from: map[uuid.UUID][]*model.Link{}, to: map[uuid.UUID][]*model.Link{},
		domains: map[*model.Link][]string{}, names: map[*model.Link]string{}}
	for i := range links {
		l := &links[i]
		g.from[l.FromID] = append(g.from[l.FromID], l)
		if l.ToID != nil {
			g.to[*l.ToID] = append(g.to[*l.ToID], l)
		}
		var d struct {
			Target  string   `json:"target"`
			Domains []string `json:"domains"`
		}
		_ = json.Unmarshal(l.Detail, &d)
		g.domains[l], g.names[l] = d.Domains, domainmatch.NameOf(d.Target)
	}
	return g
}

// Uses are the effective links from an entity: one per link, or per route
// taken through the webservers it reaches.
func (g *Graph) Uses(id uuid.UUID) []Path {
	var out []Path
	for _, l := range g.from[id] {
		out = append(out, g.From(l)...)
	}
	return out
}

// From are the effective links starting with a link of the graph: the
// link alone, or the routes it goes on through when it reaches a
// webserver. Of the routes reaching the same end, the shortest is kept:
// one path per thing used.
func (g *Graph) From(l *model.Link) []Path {
	var names []string
	if n := g.names[l]; n != "" {
		names = []string{n}
	}
	var out []Path
	for _, p := range g.follow(nil, []*model.Link{l}, names) {
		i := slices.IndexFunc(out, func(o Path) bool { return sameEnd(o.Last(), p.Last()) })
		switch {
		case i < 0:
			out = append(out, p)
		case len(p.Links) < len(out[i].Links):
			out[i] = p
		}
	}
	return out
}

// UsedBy are the effective links to an entity: the paths of the consumers
// reaching it, directly or through webservers. A webserver is not a
// consumer: what calls it is.
func (g *Graph) UsedBy(id uuid.UUID) []Path {
	// The consumers are what reaches the entity going back through the
	// webservers; their paths are walked forward, as Uses does, so that
	// both ways agree.
	consumers := map[uuid.UUID]bool{}
	var order []uuid.UUID
	seen := map[uuid.UUID]bool{id: true}
	queue := []uuid.UUID{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, l := range g.to[cur] {
			if l.FromType == model.LinkNodeWebserver {
				if !seen[l.FromID] {
					seen[l.FromID] = true
					queue = append(queue, l.FromID)
				}
				continue
			}
			if !consumers[l.FromID] {
				consumers[l.FromID] = true
				order = append(order, l.FromID)
			}
		}
	}
	var out []Path
	for _, c := range order {
		for _, p := range g.Uses(c) {
			if last := p.Last(); last.ToID != nil && *last.ToID == id {
				out = append(out, p)
			}
		}
	}
	return out
}

// follow goes on from the end of the path while it is a webserver, along
// the routes of the names called; nil names stand for a call by IP.
func (g *Graph) follow(out []Path, path []*model.Link, names []string) []Path {
	last := path[len(path)-1]
	if last.ToType != model.LinkNodeWebserver || last.ToID == nil {
		return append(out, Path{Links: path})
	}
	w := *last.ToID
	// A webserver met twice is a loop of routes: the path stops there.
	if slices.ContainsFunc(path, func(l *model.Link) bool { return l.FromID == w }) {
		return append(out, Path{Links: path, DeadEnd: true})
	}
	routes := g.routes(g.from[w], names)
	if len(routes) == 0 {
		return append(out, Path{Links: path, DeadEnd: true})
	}
	for _, r := range routes {
		out = g.follow(out, append(slices.Clone(path), r.link), r.names)
	}
	return out
}

type route struct {
	link  *model.Link
	names []string
}

// routes are the links of a webserver the names called go on through. The
// routes of these names win over the routes of no name, as a webserver
// picks the vhost or the router of the name first. A call by IP takes the
// routes of no name, or the routes of names when they all lead to the same
// place: anything else would be a guess.
func (g *Graph) routes(links []*model.Link, names []string) []route {
	var named, open []route
	var anyName []*model.Link
	for _, l := range links {
		domains := g.domains[l]
		switch {
		case len(domains) == 0:
			open = append(open, route{link: l, names: names})
		case names == nil:
			anyName = append(anyName, l)
		default:
			if routed := domainmatch.Routed(names, domains); len(routed) > 0 {
				named = append(named, route{link: l, names: routed})
			}
		}
	}
	if len(named) > 0 {
		return toApps(named)
	}
	if len(open) > 0 || len(anyName) == 0 {
		return toApps(open)
	}
	for _, l := range anyName[1:] {
		if !sameEnd(l, anyName[0]) {
			return nil
		}
	}
	out := make([]route, 0, len(anyName))
	for _, l := range anyName {
		out = append(out, route{link: l})
	}
	return out
}

// toApps leaves out, when the routes reach an app, those reaching a
// service: the app a vhost serves is what the name leads to, the PHP-FPM
// its requests are passed to only runs it.
func toApps(routes []route) []route {
	if !slices.ContainsFunc(routes, func(r route) bool { return r.link.ToType == model.LinkNodeApp }) {
		return routes
	}
	return slices.DeleteFunc(routes, func(r route) bool { return r.link.ToType == model.LinkNodeService })
}

func sameEnd(a, b *model.Link) bool {
	if a.ToType != b.ToType {
		return false
	}
	if a.ToID == nil || b.ToID == nil {
		return a.ToID == nil && b.ToID == nil && a.ExternalTarget != nil && b.ExternalTarget != nil && *a.ExternalTarget == *b.ExternalTarget
	}
	return *a.ToID == *b.ToID
}

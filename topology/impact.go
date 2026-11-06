package topology

import (
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/domainmatch"
	"github.com/alexandreh2ag/lacis/model"
)

// Direction is the way an impact walks the links.
type Direction string

const (
	// Dependents are what stops working when the node does.
	Dependents Direction = "dependents"
	// Needs are what the node needs to work.
	Needs Direction = "needs"
)

// Impact is what an impact reaches, as nodes and edges of the graph of the
// same filter.
type Impact struct {
	// Levels give each node reached its distance to the target: 1 for a
	// direct neighbour.
	Levels map[string]int
	// Edges are the edges walked, "<from>→<to>".
	Edges []string
}

// raw is a link of the inventory as the impact walks it.
type raw struct {
	from, to         string
	fromKind, toKind NodeKind
	// target is the name an app calls a webserver by; domains the names a
	// webserver routes to the other end.
	target  string
	domains []string
	// hops are, through a folded webserver, the link reaching it then the
	// link to the app it serves: the walk crosses both at once.
	hops []raw
}

// step is a node reached. A webserver is only reached for some names:
// those routed to what is down, or those called by who needs it; nil
// stands for all of it.
type step struct {
	id    string
	names []string
}

// BuildImpact walks the links from the node of the graph of the filter,
// following through a webserver only the routes of the names concerned:
// an app calling the traefik of production for a staging domain does not
// depend on the production backends of that traefik. Through proxies, the
// webservers are crossed without taking a level, and left out of the
// levels: an app behind a traefik then an nginx is a direct neighbour.
//
// The walk takes the full links, even when the graph draws the effective
// ones: an app calling a backend through a proxy depends on the proxy. An
// effective link drawn is then lit when both its ends are.
func BuildImpact(inv *model.Inventory, f Filter, node string, dir Direction, throughProxies bool) (*Impact, error) {
	full := f
	full.FullLinks = true
	b, err := prepare(inv, full)
	if err != nil {
		return nil, err
	}
	graph := b.draw()
	if !f.FullLinks {
		if graph, err = Build(inv, f); err != nil {
			return nil, err
		}
	}
	drawn := map[string]bool{}
	for _, n := range graph.Nodes {
		drawn[n.ID] = true
	}
	links := b.walked()
	// next gives the links leaving a node in the direction of the walk.
	next := map[string][]int{}
	for i, r := range links {
		near := r.to
		if dir == Needs {
			near = r.from
		}
		next[near] = append(next[near], i)
	}
	// The walk starts from all the node stands for, its content when it is
	// opened.
	target := func(v string) bool { return v == node || b.inZone(v, node) }
	var frontier []step
	start := map[string]bool{}
	for _, r := range links {
		for _, end := range [][2]string{{r.from, r.to}, {r.to, r.from}} {
			if !start[end[0]] && slices.ContainsFunc(b.view(end[0], end[1]), target) {
				start[end[0]] = true
				frontier = append(frontier, step{id: end[0]})
			}
		}
	}
	out := &Impact{Levels: map[string]int{}}
	// whole are the nodes reached for all of them; visited the steps taken.
	whole := map[string]bool{}
	visited := map[string]bool{}
	for id := range start {
		whole[id] = true
	}
	edges := map[string]bool{}
	for level := 1; len(frontier) > 0; level++ {
		var further []step
		// A webserver crossed through proxies joins the frontier of its
		// level: the loop goes on over it.
		for k := 0; k < len(frontier); k++ {
			s := frontier[k]
			for _, i := range next[s.id] {
				r := links[i]
				near, far, farKind := r.to, r.from, r.fromKind
				if dir == Needs {
					near, far, farKind = r.from, r.to, r.toKind
				}
				proxy := throughProxies && farKind == KindWebserver
				names, ok := cross(r, s.names, dir)
				if !ok {
					continue
				}
				for _, pair := range b.pairs(r.from, r.to) {
					if vf, vt := pair[0], pair[1]; vf != vt && drawn[vf] && drawn[vt] {
						edges[vf+"→"+vt] = true
					}
				}
				for _, v := range b.view(far, near) {
					if l, ok := out.Levels[v]; !proxy && !target(v) && drawn[v] && (!ok || level < l) {
						out.Levels[v] = level
					}
				}
				key := far + "\x00" + strings.Join(names, ",")
				if whole[far] || visited[key] {
					continue
				}
				visited[key] = true
				if names == nil {
					whole[far] = true
				}
				if proxy {
					frontier = append(frontier, step{id: far, names: names})
				} else {
					further = append(further, step{id: far, names: names})
				}
			}
		}
		frontier = further
	}
	lit := func(id string) bool { _, ok := out.Levels[id]; return ok || id == node }
	for _, e := range graph.Edges {
		if !f.FullLinks && lit(e.From) && lit(e.To) {
			edges[e.From+"→"+e.To] = true
		}
	}
	drawnEdges := make(map[string]bool, len(graph.Edges))
	for _, e := range graph.Edges {
		drawnEdges[e.From+"→"+e.To] = true
	}
	for e := range edges {
		// The full links walked through a proxy are not drawn as such.
		if f.FullLinks || drawnEdges[e] {
			out.Edges = append(out.Edges, e)
		}
	}
	slices.Sort(out.Edges)
	return out, nil
}

// walked are the visible links as the impact walks them. At the level of
// the apps, a folded webserver is drawn as the apps it serves: a link
// reaching it goes on to each of them, its links inside its host go.
func (b *builder) walked() []raw {
	rawOf := func(l *link) raw {
		r := raw{from: l.from, to: l.to, fromKind: l.fromKind, toKind: l.toKind, domains: l.detail.Domains}
		if l.toKind == KindWebserver && l.fromKind != KindWebserver {
			r.target = domainmatch.NameOf(l.detail.Target)
		}
		return r
	}
	served := map[string][]raw{}
	for _, l := range b.visible {
		if b.internal(l) && l.toKind == KindApp {
			served[l.from] = append(served[l.from], rawOf(l))
		}
	}
	out := make([]raw, 0, len(b.visible))
	for _, l := range b.visible {
		if b.internal(l) {
			continue
		}
		r := rawOf(l)
		if _, folded := b.folded[l.to]; !folded || l.toKind != KindWebserver {
			out = append(out, r)
			continue
		}
		for _, s := range served[l.to] {
			out = append(out, raw{from: r.from, to: s.to, fromKind: r.fromKind, toKind: s.toKind, hops: []raw{r, s}})
		}
	}
	return out
}

// cross tells whether the walk crosses a link, through all its hops, and
// for which names it reaches its far end.
func cross(r raw, names []string, dir Direction) ([]string, bool) {
	if len(r.hops) == 0 {
		return walk(r, names, dir)
	}
	hops := slices.Clone(r.hops)
	if dir == Dependents {
		slices.Reverse(hops)
	}
	for _, h := range hops {
		var ok bool
		if names, ok = walk(h, names, dir); !ok {
			return nil, false
		}
	}
	return names, true
}

// walk tells whether the walk crosses a link, and for which names it
// reaches its far end.
func walk(r raw, names []string, dir Direction) ([]string, bool) {
	if dir == Dependents {
		// A webserver routing to what is down is down for those domains.
		if r.fromKind == model.LinkNodeWebserver {
			if len(r.domains) == 0 {
				return names, true
			}
			if names == nil {
				return r.domains, true
			}
			routed := domainmatch.Routed(names, r.domains)
			return routed, len(routed) > 0
		}
		// An app is down when it calls one of them; one calling by IP may
		// reach any of them.
		if names != nil && r.target != "" {
			return nil, slices.ContainsFunc(names, func(d string) bool { return domainmatch.Match(d, r.target) })
		}
		return nil, true
	}
	// An app calling a webserver by a name needs the routes of that name.
	if r.target != "" {
		return []string{r.target}, true
	}
	if r.fromKind == model.LinkNodeWebserver && names != nil {
		if len(r.domains) > 0 && len(domainmatch.Routed(names, r.domains)) == 0 {
			return nil, false
		}
		if r.toKind == model.LinkNodeWebserver {
			return names, true
		}
	}
	return nil, true
}

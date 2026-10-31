package topology

import (
	"encoding/json"
	"net/netip"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/linker"
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
	fromKind, toKind string
	// target is the name an app calls a webserver by; domains the names a
	// webserver routes to the other end.
	target  string
	domains []string
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
// depend on the production backends of that traefik.
func BuildImpact(inv *model.Inventory, f Filter, node string, dir Direction) (*Impact, error) {
	g, err := Build(inv, f)
	if err != nil {
		return nil, err
	}
	drawn := map[string]bool{}
	for _, n := range g.Nodes {
		drawn[n.ID] = true
	}
	b := newBuilder(inv, f)
	b.nodes()
	b.stack()
	var links []raw
	for _, l := range inv.Links {
		from, to := b.id(l.FromType, l.FromID, nil), b.id(l.ToType, deref(l.ToID), l.ExternalTarget)
		if _, ok := b.all[from]; !ok {
			continue
		}
		if _, ok := b.all[to]; !ok {
			continue
		}
		if (f.NoExternals && l.ToType == model.LinkNodeExternal) || (len(f.Confidences) > 0 && !slices.Contains(f.Confidences, l.Confidence)) {
			continue
		}
		var d linker.Detail
		_ = json.Unmarshal(l.Detail, &d)
		r := raw{from: from, to: to, fromKind: l.FromType, toKind: l.ToType, domains: d.Domains}
		if l.ToType == model.LinkNodeWebserver && l.FromType != model.LinkNodeWebserver {
			r.target = nameOf(d.Target)
		}
		links = append(links, r)
	}
	// The walk starts from all the node stands for, its content when it is
	// opened.
	target := func(v string) bool { return v == node || strings.HasPrefix(v, itemPrefix(node)) }
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
		var next []step
		for _, s := range frontier {
			for _, r := range links {
				near, far := r.to, r.from
				if dir == Needs {
					near, far = r.from, r.to
				}
				if near != s.id {
					continue
				}
				names, ok := walk(r, s.names, dir)
				if !ok {
					continue
				}
				for _, vf := range b.view(r.from, r.to) {
					for _, vt := range b.view(r.to, r.from) {
						if vf != vt && drawn[vf] && drawn[vt] {
							edges[vf+"→"+vt] = true
						}
					}
				}
				for _, v := range b.view(far, near) {
					if l, ok := out.Levels[v]; !target(v) && drawn[v] && (!ok || level < l) {
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
				next = append(next, step{id: far, names: names})
			}
		}
		frontier = next
	}
	for e := range edges {
		out.Edges = append(out.Edges, e)
	}
	slices.Sort(out.Edges)
	return out, nil
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
			routed := routedNames(names, r.domains)
			return routed, len(routed) > 0
		}
		// An app is down when it calls one of them; one calling by IP may
		// reach any of them.
		if names != nil && r.target != "" {
			return nil, slices.ContainsFunc(names, func(d string) bool { return matchDomain(r.target, d) })
		}
		return nil, true
	}
	// An app calling a webserver by a name needs the routes of that name.
	if r.target != "" {
		return []string{r.target}, true
	}
	if r.fromKind == model.LinkNodeWebserver && names != nil {
		if len(r.domains) > 0 && len(routedNames(names, r.domains)) == 0 {
			return nil, false
		}
		if r.toKind == model.LinkNodeWebserver {
			return names, true
		}
	}
	return nil, true
}

// routedNames keeps the names the domains route.
func routedNames(names, domains []string) []string {
	out := []string{}
	for _, n := range names {
		if slices.ContainsFunc(domains, func(d string) bool { return matchDomain(n, d) || matchDomain(d, n) }) {
			out = append(out, n)
		}
	}
	return out
}

// matchDomain tells a name a domain routes: the same, or under a wildcard.
func matchDomain(name, domain string) bool {
	name, domain = strings.ToLower(name), strings.ToLower(domain)
	return name == domain || (strings.HasPrefix(domain, "*.") && strings.HasSuffix(name, domain[1:]))
}

// nameOf is the name an address calls; an IP or a socket calls none, so
// that nothing narrows the routes.
func nameOf(target string) string {
	host := target
	if _, rest, ok := strings.Cut(host, "://"); ok {
		host = rest
	}
	host, _, _ = strings.Cut(host, "/")
	if rest, ok := strings.CutPrefix(host, "["); ok {
		host, _, _ = strings.Cut(rest, "]")
	} else if h, _, ok := strings.Cut(host, ":"); ok {
		host = h
	}
	if host == "" {
		return ""
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return ""
	}
	return host
}

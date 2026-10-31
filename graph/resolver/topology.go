package resolver

import (
	"cmp"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/graph"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/topology"
)

var graphLevels = map[graph.GraphLevel]topology.Level{
	graph.GraphLevelProject: topology.LevelProject,
	graph.GraphLevelHost:    topology.LevelHost,
	graph.GraphLevelApp:     topology.LevelApp,
}

// freshness orders the statuses, the freshest first.
var freshness = []graph.HostStatus{graph.HostStatusActive, graph.HostStatusStale, graph.HostStatusInactive}

// topologyFilter is the filter of the graph of a namespace at a level;
// the inactive hosts are left out unless the filter asks for them.
func (r *Resolver) topologyFilter(x *index, level graph.GraphLevel, filter *graph.GraphFilter) topology.Filter {
	var f graph.GraphFilter
	if filter != nil {
		f = *filter
	}
	statuses := f.Status
	if len(statuses) == 0 {
		statuses = []graph.HostStatus{graph.HostStatusActive, graph.HostStatusStale}
	}
	now := r.Clock.Now()
	tf := topology.Filter{Level: graphLevels[level], Projects: f.Projects, Envs: f.Envs, Hosts: f.Hosts,
		NoExternals: f.Externals != nil && !*f.Externals, Expand: f.Expand,
		Hidden: func(h *model.Host) bool {
			return !slices.Contains(statuses, status(&x.inv.Namespace, h, now))
		}}
	for _, c := range f.Confidences {
		tf.Confidences = append(tf.Confidences, strings.ToLower(string(c)))
	}
	return tf
}

// impact walks the links from a node of the graph.
func (r *Resolver) impact(x *index, level graph.GraphLevel, filter *graph.GraphFilter, node string, dir graph.ImpactDirection) (*graph.GraphImpact, error) {
	d := topology.Dependents
	if dir == graph.ImpactDirectionNeeds {
		d = topology.Needs
	}
	im, err := topology.BuildImpact(x.inv, r.topologyFilter(x, level, filter), node, d)
	if err != nil {
		return nil, err
	}
	out := &graph.GraphImpact{Nodes: make([]graph.GraphImpactNode, 0, len(im.Levels)), Edges: orEmpty(im.Edges)}
	for id, l := range im.Levels {
		out.Nodes = append(out.Nodes, graph.GraphImpactNode{ID: id, Level: l})
	}
	slices.SortFunc(out.Nodes, func(a, b graph.GraphImpactNode) int {
		return cmp.Or(cmp.Compare(a.Level, b.Level), cmp.Compare(a.ID, b.ID))
	})
	return out, nil
}

// graph draws the graph of a namespace at a level.
func (r *Resolver) graph(x *index, level graph.GraphLevel, filter *graph.GraphFilter) (*graph.Graph, error) {
	now := r.Clock.Now()
	g, err := topology.Build(x.inv, r.topologyFilter(x, level, filter))
	if err != nil {
		return nil, err
	}
	out := &graph.Graph{Level: level, Nodes: make([]graph.GraphNode, 0, len(g.Nodes)), Edges: make([]graph.GraphEdge, 0, len(g.Edges)),
		Groups: make([]graph.GraphGroup, 0, len(g.Groups))}
	for _, n := range g.Nodes {
		gn := graph.GraphNode{ID: n.ID, Kind: graph.GraphNodeKind(strings.ToUpper(n.Kind)), Label: n.Label, Detail: optional(n.Detail),
			Hostname: optional(n.Host), Project: optional(n.Project), Env: optional(n.Env), Role: optional(n.Role), Version: optional(n.Version),
			Techs: orEmpty(n.Techs), Count: n.Count, Apps: n.Apps, Members: orEmpty(n.Members), Selected: n.Selected, Expanded: n.Expanded,
			Hosts: []model.Host{}, Divergences: make([]graph.GraphDivergence, 0, len(n.Divergences))}
		if n.Entity != nil {
			gn.Entity = x.entity(n.Kind, *n.Entity)
		}
		for _, id := range n.HostIDs {
			h, ok := x.hosts[id]
			if !ok {
				continue
			}
			gn.Hosts = append(gn.Hosts, *h)
			s := status(&x.inv.Namespace, h, now)
			if gn.Status == nil || slices.Index(freshness, s) > slices.Index(freshness, *gn.Status) {
				gn.Status = &s
			}
		}
		for _, d := range n.Divergences {
			gd := graph.GraphDivergence{App: d.App, Hosts: make([]graph.GraphDeployment, 0, len(d.Hosts))}
			for _, h := range d.Hosts {
				gd.Hosts = append(gd.Hosts, graph.GraphDeployment{Hostname: h.Host, Version: optional(h.Version), Commit: optional(h.Commit)})
			}
			gn.Divergences = append(gn.Divergences, gd)
		}
		out.Nodes = append(out.Nodes, gn)
	}
	for _, e := range g.Edges {
		out.Edges = append(out.Edges, graph.GraphEdge{ID: e.From + "→" + e.To, From: e.From, To: e.To, Source: e.Source,
			Confidence: graph.LinkConfidence(strings.ToUpper(e.Confidence)), Rule: e.Rule, Labels: orEmpty(e.Labels), Count: e.Count,
			CrossEnv: e.CrossEnv})
	}
	for _, gr := range g.Groups {
		out.Groups = append(out.Groups, graph.GraphGroup{ID: gr.ID, Label: gr.Label, Project: optional(gr.Project), Env: optional(gr.Env), Nodes: gr.Nodes})
	}
	return out, nil
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

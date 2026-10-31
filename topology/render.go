package topology

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/alexandreh2ag/lacis/model"
)

// Format is an output of a graph.
type Format string

const (
	FormatMermaid Format = "mermaid"
	FormatDOT     Format = "dot"
	FormatJSON    Format = "json"
)

// Formats are the formats, the default first.
var Formats = []Format{FormatMermaid, FormatDOT, FormatJSON}

// Layout is the layout engine of a Mermaid graph.
type Layout string

const (
	// LayoutDagre is the default of Mermaid.
	LayoutDagre Layout = "dagre"
	// LayoutELK routes the edges of a large graph better and keeps the
	// groups apart; mermaid.live and the recent renderers have it.
	LayoutELK Layout = "elk"
)

// Layouts are the layouts, the default first.
var Layouts = []Layout{LayoutDagre, LayoutELK}

// Options tell how to draw a graph; the JSON format ignores them.
type Options struct {
	// Labels writes on each edge its keys or domains.
	Labels bool
	// Layout is the layout engine of Mermaid; empty is the default.
	Layout Layout
}

// maxLabels is the number of labels an edge shows; the others are counted.
const maxLabels = 3

// edgeColors tell the edges apart: the edges leaving a node share its
// color, so that one can follow them across the graph.
var edgeColors = []string{"#2563eb", "#dc2626", "#16a34a", "#9333ea", "#ea580c", "#0891b2", "#db2777", "#854d0e"}

// Render writes the graph in the format.
func Render(w io.Writer, g *Graph, format Format, opts Options) error {
	switch format {
	case FormatMermaid:
		return mermaid(w, g, opts)
	case FormatDOT:
		return dot(w, g, opts)
	case FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(g)
	}
	return fmt.Errorf("unknown format %q", format)
}

// shortIDs names the nodes n0, n1… and the groups g0, g1…: the ids of the
// graph hold characters the formats do not take.
func shortIDs(g *Graph) map[string]string {
	ids := make(map[string]string, len(g.Nodes)+len(g.Groups))
	for i, n := range g.Nodes {
		ids[n.ID] = "n" + strconv.Itoa(i)
	}
	for i, gr := range g.Groups {
		ids[gr.ID] = "g" + strconv.Itoa(i)
	}
	return ids
}

// nodeText names a node; a node drawn out of a group names its host too.
func nodeText(n Node, withHost bool) string {
	label := n.Label
	if len(n.Members) > 1 && n.Kind != KindProject {
		label += " ×" + strconv.Itoa(len(n.Members))
	}
	lines := []string{label}
	if n.Detail != "" {
		lines = append(lines, n.Detail)
	}
	if withHost && n.Host != "" && n.Kind != model.LinkNodeHost {
		lines = append(lines, "@ "+n.Host)
	}
	return strings.Join(lines, "\n")
}

func groupText(gr Group) string {
	var scope []string
	for _, s := range []string{gr.Project, gr.Env} {
		if s != "" {
			scope = append(scope, s)
		}
	}
	if len(scope) == 0 {
		return gr.Label
	}
	return gr.Label + " (" + strings.Join(scope, " · ") + ")"
}

func edgeText(e Edge) string {
	labels := e.Labels
	more := ""
	if len(labels) > maxLabels {
		labels, more = labels[:maxLabels], " +"+strconv.Itoa(len(e.Labels)-maxLabels)
	}
	return strings.Join(labels, ", ") + more
}

// drawnGroups are the groups drawn as a box: a host holding a single node
// is not, its node naming it, so that the layout has fewer boxes to place.
func drawnGroups(g *Graph) []Group {
	var out []Group
	for _, gr := range g.Groups {
		if len(gr.Nodes) > 1 {
			out = append(out, gr)
		}
	}
	return out
}

// grouped tells the nodes drawn inside a group.
func grouped(groups []Group) map[string]bool {
	in := map[string]bool{}
	for _, gr := range groups {
		for _, id := range gr.Nodes {
			in[id] = true
		}
	}
	return in
}

// edgeColor is the color of the edges leaving a node, by the order the
// nodes appear as a source.
func edgeColorer(g *Graph) func(Edge) string {
	colors := map[string]string{}
	return func(e Edge) string {
		c, ok := colors[e.From]
		if !ok {
			c = edgeColors[len(colors)%len(edgeColors)]
			colors[e.From] = c
		}
		return c
	}
}

func mermaidText(s string) string {
	s = strings.NewReplacer(`"`, "#quot;", "\n", "<br/>").Replace(s)
	return `"` + s + `"`
}

// mermaidShapes draws each kind of node: an app round, a service as a
// database, a webserver as a hexagon, a host as a box, an external target
// as an asymmetric flag.
var mermaidShapes = map[string][2]string{
	model.LinkNodeApp:       {"(", ")"},
	model.LinkNodeService:   {"[(", ")]"},
	model.LinkNodeWebserver: {"{{", "}}"},
	model.LinkNodeHost:      {"[", "]"},
	model.LinkNodeExternal:  {">", "]"},
	KindProject:             {"[[", "]]"},
}

func mermaid(w io.Writer, g *Graph, opts Options) error {
	ids := shortIDs(g)
	var b strings.Builder
	if opts.Layout != "" && opts.Layout != LayoutDagre {
		fmt.Fprintf(&b, "---\nconfig:\n  layout: %s\n---\n", opts.Layout)
	}
	b.WriteString("flowchart LR\n")
	node := func(indent string, n Node, withHost bool) {
		shape := mermaidShapes[n.Kind]
		fmt.Fprintf(&b, "%s%s%s%s%s:::%s\n", indent, ids[n.ID], shape[0], mermaidText(nodeText(n, withHost)), shape[1], n.Kind)
	}
	byID := map[string]Node{}
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	groups := drawnGroups(g)
	for _, gr := range groups {
		fmt.Fprintf(&b, "  subgraph %s[%s]\n", ids[gr.ID], mermaidText(groupText(gr)))
		for _, id := range gr.Nodes {
			node("    ", byID[id], false)
		}
		b.WriteString("  end\n")
	}
	in := grouped(groups)
	for _, n := range g.Nodes {
		if !in[n.ID] {
			node("  ", n, true)
		}
	}
	color := edgeColorer(g)
	var byColor []string
	edgesOf := map[string][]string{}
	for i, e := range g.Edges {
		arrow := "-->"
		if e.Confidence != model.LinkConfidenceExact {
			arrow = "-.->"
		}
		if text := edgeText(e); opts.Labels && text != "" {
			fmt.Fprintf(&b, "  %s %s|%s| %s\n", ids[e.From], arrow, mermaidText(text), ids[e.To])
		} else {
			fmt.Fprintf(&b, "  %s %s %s\n", ids[e.From], arrow, ids[e.To])
		}
		c := color(e)
		if _, ok := edgesOf[c]; !ok {
			byColor = append(byColor, c)
		}
		edgesOf[c] = append(edgesOf[c], strconv.Itoa(i))
	}
	for _, c := range byColor {
		fmt.Fprintf(&b, "  linkStyle %s stroke:%s,color:%s\n", strings.Join(edgesOf[c], ","), c, c)
	}
	b.WriteString("  classDef app fill:#dbeafe,stroke:#1d4ed8\n")
	b.WriteString("  classDef service fill:#dcfce7,stroke:#15803d\n")
	b.WriteString("  classDef webserver fill:#fef3c7,stroke:#b45309\n")
	b.WriteString("  classDef host fill:#f3f4f6,stroke:#4b5563\n")
	b.WriteString("  classDef project fill:#f3f4f6,stroke:#4b5563\n")
	b.WriteString("  classDef external fill:#fee2e2,stroke:#b91c1c,stroke-dasharray:4 2\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func dotText(s string) string {
	return strconv.Quote(s)
}

var dotShapes = map[string]string{
	model.LinkNodeApp:       `shape=box, style="rounded,filled", fillcolor="#dbeafe"`,
	model.LinkNodeService:   `shape=cylinder, style=filled, fillcolor="#dcfce7"`,
	model.LinkNodeWebserver: `shape=hexagon, style=filled, fillcolor="#fef3c7"`,
	model.LinkNodeHost:      `shape=box, style=filled, fillcolor="#f3f4f6"`,
	KindProject:             `shape=folder, style=filled, fillcolor="#f3f4f6"`,
	model.LinkNodeExternal:  `shape=cds, style="filled,dashed", fillcolor="#fee2e2"`,
}

func dot(w io.Writer, g *Graph, opts Options) error {
	ids := shortIDs(g)
	var b strings.Builder
	fmt.Fprintf(&b, "digraph %s {\n  rankdir=LR;\n  ranksep=1.2;\n  node [fontname=\"Helvetica\"];\n  edge [fontname=\"Helvetica\", fontsize=10];\n", dotText(g.Namespace))
	node := func(indent string, n Node, withHost bool) {
		fmt.Fprintf(&b, "%s%s [label=%s, %s];\n", indent, ids[n.ID], dotText(nodeText(n, withHost)), dotShapes[n.Kind])
	}
	byID := map[string]Node{}
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	groups := drawnGroups(g)
	for _, gr := range groups {
		fmt.Fprintf(&b, "  subgraph cluster_%s {\n    label=%s;\n    style=rounded;\n", ids[gr.ID], dotText(groupText(gr)))
		for _, id := range gr.Nodes {
			node("    ", byID[id], false)
		}
		b.WriteString("  }\n")
	}
	in := grouped(groups)
	for _, n := range g.Nodes {
		if !in[n.ID] {
			node("  ", n, true)
		}
	}
	color := edgeColorer(g)
	for _, e := range g.Edges {
		c := dotText(color(e))
		attrs := []string{"color=" + c}
		if text := edgeText(e); opts.Labels && text != "" {
			attrs = append(attrs, "label="+dotText(text), "fontcolor="+c)
		}
		if e.Confidence != model.LinkConfidenceExact {
			attrs = append(attrs, "style=dashed")
		}
		slices.Sort(attrs)
		suffix := ""
		if len(attrs) > 0 {
			suffix = " [" + strings.Join(attrs, ", ") + "]"
		}
		fmt.Fprintf(&b, "  %s -> %s%s;\n", ids[e.From], ids[e.To], suffix)
	}
	b.WriteString("}\n")
	_, err := io.WriteString(w, b.String())
	return err
}

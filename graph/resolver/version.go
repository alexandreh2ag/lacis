package resolver

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/graph"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/techversion"
)

// satisfies tells whether a framework or a runtime meets the constraint.
func satisfies(name, version string, c *graph.VersionConstraint) bool {
	if c == nil {
		return true
	}
	if name != c.Name {
		return false
	}
	if c.Version == nil {
		return true
	}
	op := graph.VersionOpGte
	if c.Op != nil {
		op = *c.Op
	}
	return techversion.Satisfies(version, techversion.Op(strings.ToLower(string(op))), *c.Version)
}

// appRuntime reads the runtime of an app; empty when none was collected.
func appRuntime(a *model.App) (string, string) {
	var rt struct {
		Type    string `json:"type"`
		Version string `json:"version"`
	}
	_ = json.Unmarshal(a.Runtime, &rt)
	return rt.Type, rt.Version
}

// versions gathers the versions of names as major.minor.
type versions map[string][]string

func (v versions) add(name, version string) {
	if name == "" {
		return
	}
	if _, ok := v[name]; !ok {
		v[name] = []string{}
	}
	if m := techversion.Minor(version); m != "" && !slices.Contains(v[name], m) {
		v[name] = append(v[name], m)
	}
}

// facets lists the names and their versions in order.
func (v versions) facets() []graph.VersionFacet {
	out := []graph.VersionFacet{}
	for name, vs := range v {
		slices.SortFunc(vs, func(a, b string) int { return techversion.Compare(a, b) })
		out = append(out, graph.VersionFacet{Name: name, Versions: vs})
	}
	slices.SortFunc(out, func(a, b graph.VersionFacet) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

// facets are what the inventory holds, to filter by.
func facets(inv *model.Inventory) *graph.Facets {
	frameworks, runtimes := versions{}, versions{}
	for i := range inv.Apps {
		a := &inv.Apps[i]
		frameworks.add(deref(a.Framework), deref(a.FrameworkVersion))
		runtimes.add(appRuntime(a))
	}
	out := &graph.Facets{Frameworks: frameworks.facets(), Runtimes: runtimes.facets(), ServiceTypes: []string{}, WebserverTypes: []string{}}
	for _, s := range inv.Services {
		if !slices.Contains(out.ServiceTypes, s.Type) {
			out.ServiceTypes = append(out.ServiceTypes, s.Type)
		}
	}
	for _, w := range inv.Webservers {
		if !slices.Contains(out.WebserverTypes, w.Type) {
			out.WebserverTypes = append(out.WebserverTypes, w.Type)
		}
	}
	slices.Sort(out.ServiceTypes)
	slices.Sort(out.WebserverTypes)
	return out
}

package resolver

import (
	"cmp"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"

	"github.com/alexandreh2ag/lacis/graph"
	"github.com/alexandreh2ag/lacis/model"
)

var versionParts = regexp.MustCompile(`\d+`)

// parts are the numbers of a version: 8.3.14 gives 8, 3, 14.
func parts(version string) []int {
	var out []int
	for _, p := range versionParts.FindAllString(version, -1) {
		n, _ := strconv.Atoi(p)
		out = append(out, n)
	}
	return out
}

// compareVersions compares a version to a constraint, cut to the parts
// of the constraint; a missing part counts as 0.
func compareVersions(version, constraint string) int {
	v, c := parts(version), parts(constraint)
	for i := range c {
		got := 0
		if i < len(v) {
			got = v[i]
		}
		if d := cmp.Compare(got, c[i]); d != 0 {
			return d
		}
	}
	return 0
}

// satisfies tells whether a framework or a runtime meets the constraint.
func satisfies(name, version string, c *graph.VersionConstraint) bool {
	if c == nil {
		return true
	}
	if name != c.Name {
		return false
	}
	if c.Version == nil || *c.Version == "" || len(parts(*c.Version)) == 0 {
		return true
	}
	if len(parts(version)) == 0 {
		return false
	}
	d := compareVersions(version, *c.Version)
	op := graph.VersionOpGte
	if c.Op != nil {
		op = *c.Op
	}
	switch op {
	case graph.VersionOpLt:
		return d < 0
	case graph.VersionOpLte:
		return d <= 0
	case graph.VersionOpEq:
		return d == 0
	case graph.VersionOpGt:
		return d > 0
	}
	return d >= 0
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

// minor is a version as major.minor.
func minor(version string) string {
	p := parts(version)
	switch {
	case len(p) == 0:
		return ""
	case len(p) == 1:
		return strconv.Itoa(p[0])
	}
	return strconv.Itoa(p[0]) + "." + strconv.Itoa(p[1])
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
	if m := minor(version); m != "" && !slices.Contains(v[name], m) {
		v[name] = append(v[name], m)
	}
}

// facets lists the names and their versions in order.
func (v versions) facets() []graph.VersionFacet {
	out := []graph.VersionFacet{}
	for name, vs := range v {
		slices.SortFunc(vs, func(a, b string) int { return compareVersions(a, b) })
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
	out := &graph.Facets{Frameworks: frameworks.facets(), Runtimes: runtimes.facets(), ServiceTypes: []string{}}
	for _, s := range inv.Services {
		if !slices.Contains(out.ServiceTypes, s.Type) {
			out.ServiceTypes = append(out.ServiceTypes, s.Type)
		}
	}
	slices.Sort(out.ServiceTypes)
	return out
}

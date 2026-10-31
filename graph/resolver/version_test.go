package resolver

import (
	"encoding/json"
	"testing"

	"github.com/alexandreh2ag/lacis/graph"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/stretchr/testify/assert"
)

func TestSatisfies(t *testing.T) {
	op := func(o graph.VersionOp) *graph.VersionOp { return &o }
	v := func(s string) *string { return &s }
	tests := []struct {
		name    string
		runtime string
		version string
		c       *graph.VersionConstraint
		want    bool
	}{
		{name: "no constraint", runtime: "php", version: "8.3.14", want: true},
		{name: "any version of the runtime", runtime: "php", version: "7.0.33", c: &graph.VersionConstraint{Name: "php"}, want: true},
		{name: "another runtime", runtime: "node", version: "20.11", c: &graph.VersionConstraint{Name: "php"}, want: false},
		{name: "greater, the default", runtime: "php", version: "7.2.1", c: &graph.VersionConstraint{Name: "php", Version: v("7.1")}, want: true},
		{name: "greater, the same minor is not", runtime: "php", version: "7.1.33", c: &graph.VersionConstraint{Name: "php", Op: op(graph.VersionOpGt), Version: v("7.1")}, want: false},
		{name: "lower or equal takes the patches", runtime: "php", version: "7.4.33", c: &graph.VersionConstraint{Name: "php", Op: op(graph.VersionOpLte), Version: v("7.4")}, want: true},
		{name: "lower", runtime: "php", version: "7.4.33", c: &graph.VersionConstraint{Name: "php", Op: op(graph.VersionOpLt), Version: v("7.4")}, want: false},
		{name: "equal by the parts given", runtime: "php", version: "8.3.14", c: &graph.VersionConstraint{Name: "php", Op: op(graph.VersionOpEq), Version: v("8")}, want: true},
		{name: "numbers, not text", runtime: "node", version: "18.20.4", c: &graph.VersionConstraint{Name: "node", Op: op(graph.VersionOpGt), Version: v("9")}, want: true},
		{name: "a missing part counts as 0", runtime: "python", version: "3", c: &graph.VersionConstraint{Name: "python", Op: op(graph.VersionOpLt), Version: v("3.1")}, want: true},
		{name: "no version collected", runtime: "php", version: "", c: &graph.VersionConstraint{Name: "php", Version: v("7")}, want: false},
		{name: "a version without number takes all", runtime: "php", version: "8.3", c: &graph.VersionConstraint{Name: "php", Version: v("latest")}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, satisfies(tt.runtime, tt.version, tt.c))
		})
	}
}

func TestFacets(t *testing.T) {
	app := func(framework, version, runtime string) model.App {
		a := model.App{Runtime: json.RawMessage(runtime)}
		if framework != "" {
			a.Framework, a.FrameworkVersion = &framework, &version
		}
		return a
	}
	got := facets(&model.Inventory{
		Apps: []model.App{
			app("symfony", "7.2.6", `{"type":"php","version":"8.3.14"}`),
			app("laravel", "10.48.2", `{"type":"php","version":"7.4.33"}`),
			app("symfony", "6.4.1", `{"type":"php","version":"8.10.0"}`),
			app("", "", `{"type":"node","version":"20.11.1"}`),
			app("", "", `{}`),
		},
		Services: []model.Service{{Type: "redis"}, {Type: "mariadb"}, {Type: "redis"}},
	})
	assert.Equal(t, &graph.Facets{
		Frameworks:   []graph.VersionFacet{{Name: "laravel", Versions: []string{"10.48"}}, {Name: "symfony", Versions: []string{"6.4", "7.2"}}},
		Runtimes:     []graph.VersionFacet{{Name: "node", Versions: []string{"20.11"}}, {Name: "php", Versions: []string{"7.4", "8.3", "8.10"}}},
		ServiceTypes: []string{"mariadb", "redis"},
	}, got)
}

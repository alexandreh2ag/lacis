package pathmatch

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScore(t *testing.T) {
	tests := []struct {
		matcher, path string
		want          int
	}{
		{"/api", "/api/v1", 4},
		{"/api", "/apis", 4},
		{"/api", "/other", -1},
		{"=/health", "/health", exactBonus + 8},
		{"=/health", "/health/x", -1},
		{"~^/(graphql|api)", "/graphql", 15},
		{"~^/(graphql|api)", "/web", -1},
		{"~^/(", "/x", -1},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, Score(tt.matcher, tt.path), "%s %s", tt.matcher, tt.path)
	}
}

func TestSelect(t *testing.T) {
	routes := [][]string{
		nil,                         // the site
		{"/inspirations-ia"},        // a tool
		{"/simulateur-enr", "/sim"}, // a simulator
		{"/simulateur-enr"},         // its replica
	}
	tests := []struct {
		name   string
		path   string
		routes [][]string
		want   []int
	}{
		{name: "no path: every route", path: "", routes: routes, want: []int{0, 1, 2, 3}},
		{name: "a prefix", path: "/inspirations-ia/chat", routes: routes, want: []int{1}},
		{name: "the same best prefix", path: "/simulateur-enr", routes: routes, want: []int{2, 3}},
		{name: "the longest prefix", path: "/simulateur-enr/x", routes: [][]string{{"/sim"}, {"/simulateur-enr"}}, want: []int{1}},
		{name: "no prefix: any path", path: "/contact", routes: routes, want: []int{0}},
		{name: "served by none: all", path: "/contact", routes: [][]string{{"/api"}, {"/graphql"}}, want: []int{0, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Select(tt.path, tt.routes))
		})
	}
}

func TestFor(t *testing.T) {
	matches := []Match{
		{Domains: []string{"www.acme.example"}, Paths: []string{"/simu"}},
		{Domains: []string{"shop.acme.example"}},
		{Domains: []string{"www.acme.example"}, Paths: []string{"/quiz"}},
	}
	assert.Equal(t, []string{"/simu", "/quiz"}, For(matches, []string{"www.acme.example"}))
	assert.Nil(t, For(matches, []string{"shop.acme.example"}), "another name of the link: any path")
	assert.Nil(t, For(matches, []string{"other.acme.example"}), "no route of the name: any path")
	assert.Nil(t, For(matches, nil), "a call by IP: any route")
	assert.Nil(t, For(nil, []string{"www.acme.example"}))
	assert.Equal(t, []string{"/api"}, For([]Match{{Paths: []string{"/api"}}}, []string{"www.acme.example"}), "a route of any name")
}

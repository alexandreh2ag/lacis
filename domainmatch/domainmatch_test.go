package domainmatch

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern, name string
		want          bool
	}{
		{"a.example", "a.example", true},
		{"A.Example.", "a.example", true},
		{"a.example", "A.EXAMPLE", true},
		{"a.example", "b.example", false},
		{"*.example", "a.example", true},
		{"*.example", "a.b.example", true},
		{"*.example", "example", false},
		{"*.example", "aexample", false},
		{".example", "example", true},
		{".example", "a.b.example", true},
		{".example", "aexample", false},
		{`~^api\.`, "api.example", true},
		{`~^api\.`, "API.example", true},
		{`~^api\.`, "www.example", false},
		{`~(`, "x", false},
		{"*", "x", false},
		{"", "", true},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, Match(tt.pattern, tt.name), "%s ~ %s", tt.pattern, tt.name)
	}
}

func TestIndex_Lookup(t *testing.T) {
	patterns := []string{"a.example", "*.example", ".shop.example", `~^api\.`, `~(`, "*.b.example", "a.example", "A.Other.example."}
	x := NewIndex[int]()
	for i, p := range patterns {
		x.Add(p, i)
	}
	tests := []struct {
		name string
		want []int
	}{
		{name: "a.example", want: []int{0, 1, 6}},
		{name: "x.b.example", want: []int{1, 5}},
		{name: "shop.example", want: []int{1, 2}},
		{name: "eu.shop.example", want: []int{1, 2}},
		{name: "api.shop.example", want: []int{1, 2, 3}},
		{name: "API.elsewhere", want: []int{3}},
		{name: "a.other.example", want: []int{1, 7}},
		{name: "example", want: []int{}},
		{name: "", want: []int{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, x.Lookup(tt.name))
			// The index agrees with Match.
			brute := []int{}
			for i, p := range patterns {
				if Match(p, tt.name) {
					brute = append(brute, i)
				}
			}
			assert.Equal(t, brute, x.Lookup(tt.name))
		})
	}
}

func TestIndex_Values(t *testing.T) {
	x := NewIndex[string]()
	x.Add("*.example", "rp")
	x.Add("a.example", "web")
	x.Add("a.example", "rp")
	assert.Equal(t, []string{"rp", "web", "rp"}, x.Lookup("a.example"), "once per pattern, in the order added")
}

func BenchmarkIndex_Lookup(b *testing.B) {
	x := NewIndex[int]()
	for i := range 10000 {
		x.Add(fmt.Sprintf("site%d.example", i), i)
		x.Add(fmt.Sprintf("*.site%d.example", i), i)
	}
	x.Add(`~^[a-z]+\.shop\.example$`, -1)
	for b.Loop() {
		x.Lookup("www.site5000.example")
	}
}

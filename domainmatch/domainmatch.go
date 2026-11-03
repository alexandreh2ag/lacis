// Package domainmatch tells whether a host name is served by a domain
// pattern as webservers write them. It is the only implementation of the
// rule: the linker and the impact analysis both use it.
//
// The patterns:
//
//   - "www.example.com": that name;
//   - "*.example.com": any name under example.com, at any depth, not
//     example.com itself;
//   - ".example.com": example.com and any name under it;
//   - "~regex": a regex, as nginx writes a regex server name and as the
//     normalization stores a Traefik HostRegexp.
//
// Names and patterns are compared in lower case, without a trailing dot.
package domainmatch

import (
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

// Normalize gives the form a name or a pattern is compared in.
func Normalize(s string) string {
	return strings.ToLower(strings.TrimSuffix(s, "."))
}

// IsRegex tells a regex pattern.
func IsRegex(pattern string) bool {
	return strings.HasPrefix(pattern, "~")
}

// Match tells whether the pattern serves the name. An invalid regex
// serves nothing.
func Match(pattern, name string) bool {
	if IsRegex(pattern) {
		re := compile(pattern[1:])
		return re != nil && re.MatchString(Normalize(name))
	}
	pattern, name = Normalize(pattern), Normalize(name)
	switch {
	case pattern == name:
		return true
	case strings.HasPrefix(pattern, "*."):
		return strings.HasSuffix(name, pattern[1:])
	case strings.HasPrefix(pattern, "."):
		return name == pattern[1:] || strings.HasSuffix(name, pattern)
	}
	return false
}

// The regexes Match compiled, so that a pattern is compiled once; past
// maxCached, the others are compiled at each call.
const maxCached = 4096

var (
	cache  sync.Map // string → *regexp.Regexp, nil when invalid
	cached atomic.Int64
)

func compile(expr string) *regexp.Regexp {
	if re, ok := cache.Load(expr); ok {
		return re.(*regexp.Regexp)
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		re = nil
	}
	if cached.Load() < maxCached {
		if _, loaded := cache.LoadOrStore(expr, re); !loaded {
			cached.Add(1)
		}
	}
	return re
}

// Index finds the values whose pattern serves a name without going through
// every pattern: a map lookup for the exact names, one per suffix of the
// name for the wildcards, and the regexes, compiled once. It is not safe
// for concurrent Add.
type Index[T any] struct {
	values []T
	exact  map[string][]int
	// suffix holds ".example.com" for "*.example.com" and ".example.com".
	suffix map[string][]int
	regex  []indexedRegex
}

type indexedRegex struct {
	re *regexp.Regexp
	at int
}

func NewIndex[T any]() *Index[T] {
	return &Index[T]{exact: map[string][]int{}, suffix: map[string][]int{}}
}

// Add records a value for a pattern; an invalid regex is left out.
func (x *Index[T]) Add(pattern string, v T) {
	at := len(x.values)
	if IsRegex(pattern) {
		re, err := regexp.Compile(pattern[1:])
		if err != nil {
			return
		}
		x.values = append(x.values, v)
		x.regex = append(x.regex, indexedRegex{re: re, at: at})
		return
	}
	pattern = Normalize(pattern)
	x.values = append(x.values, v)
	switch {
	case strings.HasPrefix(pattern, "*."):
		x.suffix[pattern[1:]] = append(x.suffix[pattern[1:]], at)
	case strings.HasPrefix(pattern, "."):
		x.suffix[pattern] = append(x.suffix[pattern], at)
		x.exact[pattern[1:]] = append(x.exact[pattern[1:]], at)
	default:
		x.exact[pattern] = append(x.exact[pattern], at)
	}
}

// Lookup gives the values whose pattern serves the name, in the order they
// were added; a value added for several matching patterns comes once per
// pattern.
func (x *Index[T]) Lookup(name string) []T {
	name = Normalize(name)
	hits := slices.Clone(x.exact[name])
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			hits = append(hits, x.suffix[name[i:]]...)
		}
	}
	for _, r := range x.regex {
		if r.re.MatchString(name) {
			hits = append(hits, r.at)
		}
	}
	slices.Sort(hits)
	hits = slices.Compact(hits)
	out := make([]T, 0, len(hits))
	for _, at := range hits {
		out = append(out, x.values[at])
	}
	return out
}

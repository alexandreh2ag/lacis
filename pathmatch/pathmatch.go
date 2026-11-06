// Package pathmatch matches the path of a URL an app calls against the
// paths the routes of a webserver serve, written as nginx locations: "/api"
// a prefix, "=/health" an exact path, "~^/(graphql|api)" a regexp.
package pathmatch

import (
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/alexandreh2ag/lacis/domainmatch"
)

const exactBonus = 1 << 20

var (
	mu       sync.Mutex
	compiled = map[string]*regexp.Regexp{}
)

func compile(pattern string) *regexp.Regexp {
	mu.Lock()
	defer mu.Unlock()
	re, ok := compiled[pattern]
	if !ok {
		re, _ = regexp.Compile(pattern) // nil when invalid: matches nothing
		compiled[pattern] = re
	}
	return re
}

// Score tells how well a matcher serves a path: -1 when it does not. An
// exact path wins, then the longest prefix or regexp, as Traefik gives the
// priority to the longest rule.
func Score(matcher, path string) int {
	switch {
	case strings.HasPrefix(matcher, "="):
		if matcher[1:] == path {
			return exactBonus + len(matcher)
		}
	case strings.HasPrefix(matcher, "~"):
		if re := compile(matcher[1:]); re != nil && re.MatchString(path) {
			return len(matcher) - 1
		}
	case strings.HasPrefix(path, matcher):
		return len(matcher)
	}
	return -1
}

// Select gives the indexes of the routes a path goes to, each route being
// the matchers of its paths, none for any path: those whose best matcher
// scores highest, else those of any path. A call without path, or one no
// route serves (a matcher Lacis cannot read), keeps them all: anything else
// would be a guess.
func Select(path string, routes [][]string) []int {
	all := make([]int, len(routes))
	for i := range routes {
		all[i] = i
	}
	if path == "" {
		return all
	}
	best, scores := -1, make([]int, len(routes))
	var open []int
	for i, matchers := range routes {
		scores[i] = -1
		if len(matchers) == 0 {
			open = append(open, i)
			continue
		}
		for _, m := range matchers {
			scores[i] = max(scores[i], Score(m, path))
		}
		best = max(best, scores[i])
	}
	switch {
	case best >= 0:
		var out []int
		for i, s := range scores {
			if s == best {
				out = append(out, i)
			}
		}
		return out
	case len(open) > 0:
		return open
	}
	return all
}

// Match is what a route of a link serves: the names of its domains, none
// for any, on its paths, none for any. A link merges the routes to its
// end: each keeps its own paths, those of a name are not those of another.
type Match struct {
	Domains []string `json:"domains,omitempty"`
	Paths   []string `json:"paths,omitempty"`
}

// For gives the paths a link serves the names called on, nil for any
// path: those of its routes of these names, nil names standing for a call
// by IP, which any route may serve.
func For(matches []Match, names []string) []string {
	var out []string
	for _, m := range matches {
		if len(m.Domains) > 0 && names != nil && len(domainmatch.Routed(names, m.Domains)) == 0 {
			continue
		}
		if len(m.Paths) == 0 {
			return nil
		}
		for _, p := range m.Paths {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

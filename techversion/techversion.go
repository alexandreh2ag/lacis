// Package techversion compares the versions of the technologies the agents
// collect: free text such as 8.1.2-1ubuntu2.18 or 10.11.6-MariaDB, read by
// their numbers.
package techversion

import (
	"cmp"
	"regexp"
	"strconv"
)

// Op compares a version to a constraint.
type Op string

const (
	OpLt  Op = "lt"
	OpLte Op = "lte"
	OpEq  Op = "eq"
	OpGte Op = "gte"
	OpGt  Op = "gt"
)

// Ops are the operators, as a constraint names them.
var Ops = []Op{OpLt, OpLte, OpEq, OpGte, OpGt}

var numbers = regexp.MustCompile(`\d+`)

// Parts are the numbers of a version: 8.3.14 gives 8, 3, 14.
func Parts(version string) []int {
	var out []int
	for _, p := range numbers.FindAllString(version, -1) {
		n, _ := strconv.Atoi(p)
		out = append(out, n)
	}
	return out
}

// Compare compares a version to a constraint, cut to the parts of the
// constraint, so that 7.4.33 equals 7.4; a missing part counts as 0.
func Compare(version, constraint string) int {
	v, c := Parts(version), Parts(constraint)
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

// Satisfies tells whether version meets op constraint. A constraint
// without number takes every version; a version without number meets no
// constraint that has one.
func Satisfies(version string, op Op, constraint string) bool {
	if len(Parts(constraint)) == 0 {
		return true
	}
	if len(Parts(version)) == 0 {
		return false
	}
	d := Compare(version, constraint)
	switch op {
	case OpLt:
		return d < 0
	case OpLte:
		return d <= 0
	case OpEq:
		return d == 0
	case OpGt:
		return d > 0
	}
	return d >= 0
}

// Minor is a version as major.minor; empty without number.
func Minor(version string) string {
	p := Parts(version)
	switch {
	case len(p) == 0:
		return ""
	case len(p) == 1:
		return strconv.Itoa(p[0])
	}
	return strconv.Itoa(p[0]) + "." + strconv.Itoa(p[1])
}

// Order compares two versions on all their parts, to sort them; a version
// without number comes first.
func Order(a, b string) int {
	pa, pb := Parts(a), Parts(b)
	for i := range max(len(pa), len(pb)) {
		x, y := -1, -1
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if d := cmp.Compare(x, y); d != 0 {
			return d
		}
	}
	return 0
}

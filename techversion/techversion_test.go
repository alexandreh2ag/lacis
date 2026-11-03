package techversion

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParts(t *testing.T) {
	tests := []struct {
		version string
		want    []int
	}{
		{version: "8.3.14", want: []int{8, 3, 14}},
		{version: "8.1.2-1ubuntu2.18", want: []int{8, 1, 2, 1, 2, 18}},
		{version: "10.11.6-MariaDB", want: []int{10, 11, 6}},
		{version: "latest"},
		{version: ""},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			assert.Equal(t, tt.want, Parts(tt.version))
		})
	}
}

func TestSatisfies(t *testing.T) {
	tests := []struct {
		name       string
		version    string
		op         Op
		constraint string
		want       bool
	}{
		{name: "greater or equal", version: "7.2.1", op: OpGte, constraint: "7.1", want: true},
		{name: "unknown operator is greater or equal", version: "7.0", op: "", constraint: "7.1", want: false},
		{name: "greater, the same minor is not", version: "7.1.33", op: OpGt, constraint: "7.1", want: false},
		{name: "lower or equal takes the patches", version: "7.4.33", op: OpLte, constraint: "7.4", want: true},
		{name: "lower", version: "7.4.33", op: OpLt, constraint: "7.4", want: false},
		{name: "lower, distribution suffix ignored", version: "8.1.2-1ubuntu2.18", op: OpLt, constraint: "8.2", want: true},
		{name: "equal by the parts given", version: "8.3.14", op: OpEq, constraint: "8", want: true},
		{name: "numbers, not text", version: "18.20.4", op: OpGt, constraint: "9", want: true},
		{name: "a missing part counts as 0", version: "3", op: OpLt, constraint: "3.1", want: true},
		{name: "no version collected", version: "", op: OpGte, constraint: "7", want: false},
		{name: "a constraint without number takes all", version: "8.3", op: OpLt, constraint: "latest", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Satisfies(tt.version, tt.op, tt.constraint))
		})
	}
}

func TestCompare(t *testing.T) {
	assert.Equal(t, -1, Compare("8.9", "8.10"))
	assert.Equal(t, 0, Compare("8.10.3", "8.10"))
	assert.Equal(t, 1, Compare("9", "8.10"))
}

func TestMinor(t *testing.T) {
	assert.Equal(t, "8.3", Minor("8.3.14"))
	assert.Equal(t, "20", Minor("20"))
	assert.Equal(t, "", Minor("latest"))
}

func TestOrder(t *testing.T) {
	assert.Equal(t, -1, Order("8.1", "8.1.2"))
	assert.Equal(t, 1, Order("8.10", "8.9.9"))
	assert.Equal(t, 0, Order("8.1.2", "v8.1.2"))
	assert.Equal(t, -1, Order("unknown", "1"))
}

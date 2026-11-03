package ini

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	content := `global = 1
; comment
# comment
[program:web]
command = php a
  --b ;inline
no equal sign
[ production : staging ]
x=1
x = 2
`
	tests := []struct {
		name string
		opts Options
		want []Section
	}{
		{
			name: "plain",
			want: []Section{
				{Keys: []Key{{Name: "global", Value: "1", Line: 1}}},
				{Name: "program:web", Line: 4, Keys: []Key{{Name: "command", Value: "php a", Line: 5}}},
				{Name: "production : staging", Line: 8, Keys: []Key{{Name: "x", Value: "1", Line: 9}, {Name: "x", Value: "2", Line: 10}}},
			},
		},
		{
			name: "continuation and inline comments",
			opts: Options{Continuation: true, Value: func(v string) string {
				v, _, _ = strings.Cut(v, " ;")
				return strings.TrimSpace(v)
			}},
			want: []Section{
				{Keys: []Key{{Name: "global", Value: "1", Line: 1}}},
				{Name: "program:web", Line: 4, Keys: []Key{{Name: "command", Value: "php a --b", Line: 5}}},
				{Name: "production : staging", Line: 8, Keys: []Key{{Name: "x", Value: "1", Line: 9}, {Name: "x", Value: "2", Line: 10}}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse([]byte(content), tt.opts)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParse_ContinuationWithoutKey(t *testing.T) {
	got, err := Parse([]byte("[a]\n  orphan\n"), Options{Continuation: true})
	require.NoError(t, err)
	assert.Equal(t, []Section{{}, {Name: "a", Line: 1}}, got)
}

func TestParse_LongLine(t *testing.T) {
	long := "k=" + strings.Repeat("x", 100_000)
	got, err := Parse([]byte(long), Options{})
	require.NoError(t, err)
	assert.Len(t, got[0].Keys[0].Value, 100_000)

	_, err = Parse([]byte("k="+strings.Repeat("x", MaxLine)), Options{})
	assert.Error(t, err)
}

func TestSection_Get(t *testing.T) {
	s := Section{Keys: []Key{{Name: "x", Value: "1"}, {Name: "x", Value: "2"}}}
	v, ok := s.Get("x")
	assert.True(t, ok)
	assert.Equal(t, "2", v)
	_, ok = s.Get("y")
	assert.False(t, ok)
}

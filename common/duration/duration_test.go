package duration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := map[string]time.Duration{
		"7d": 7 * Day, "1d12h": 36 * time.Hour, "30m": 30 * time.Minute, "0d": 0, " 2h ": 2 * time.Hour,
	}
	for in, want := range tests {
		got, err := Parse(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"", "d", "7days", "1dxx", "99999999999999999999d", "200000d", "soon"} {
		_, err := Parse(in)
		assert.ErrorIs(t, err, ErrInvalid, in)
	}
}

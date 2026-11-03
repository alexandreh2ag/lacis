package cdn

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKnown(t *testing.T) {
	assert.Equal(t, []string{"cloudflare"}, Names())
	for _, name := range Names() {
		c, ok := Known(name)
		require.True(t, ok)
		assert.Equal(t, name, c.Name)
		for _, p := range c.Ranges {
			assert.Equal(t, p, p.Masked(), "%s: %s is not the network address", name, p)
		}
	}
	_, ok := Known("akamai")
	assert.False(t, ok)
}

func TestContains(t *testing.T) {
	c, _ := Known("cloudflare")
	tests := []struct {
		ip   string
		want bool
	}{
		{"104.16.1.1", true},
		{"172.67.10.20", true},
		{"2606:4700::6810:84e5", true},
		{"203.0.113.10", false},
		{"10.0.0.1", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, c.Contains(netip.MustParseAddr(tt.ip)), tt.ip)
	}
}

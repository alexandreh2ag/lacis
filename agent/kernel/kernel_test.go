package kernel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The real system is read: these tests only check consistency.

func TestOS_Interfaces(t *testing.T) {
	ifaces, err := New().Interfaces()
	require.NoError(t, err)
	var loopback *Interface
	for i := range ifaces {
		if ifaces[i].Loopback {
			loopback = &ifaces[i]
		}
	}
	require.NotNil(t, loopback, "every Linux host has a loopback")
	for _, p := range loopback.Addrs {
		assert.True(t, p.Addr().IsLoopback(), p)
	}
}

func TestOS_DiskSize(t *testing.T) {
	size, err := New().DiskSize("/")
	require.NoError(t, err)
	assert.Positive(t, size)

	_, err = New().DiskSize("/does/not/exist")
	assert.Error(t, err)
}

func TestOS_LookupHost(t *testing.T) {
	addrs, err := New().LookupHost(t.Context(), "localhost")
	require.NoError(t, err)
	assert.NotEmpty(t, addrs)
}

package clock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestReal_Now(t *testing.T) {
	before := time.Now()
	now := New().Now()
	assert.False(t, now.Before(before))
}

func TestReal_After(t *testing.T) {
	select {
	case <-New().After(time.Millisecond):
	case <-time.After(time.Second):
		t.Fatal("After did not fire")
	}
}

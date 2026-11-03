// Package clock abstracts time so that intervals, jitter, statuses and purges
// can be tested with a fixed time.
package clock

import "time"

// Clock gives the current time and timers.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// Real is the Clock backed by the time package.
type Real struct{}

var _ Clock = Real{}

// New returns the real clock.
func New() Clock {
	return Real{}
}

func (Real) Now() time.Time {
	return time.Now()
}

func (Real) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

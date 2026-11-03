// Package duration parses the durations of the server and agent
// configurations: time.ParseDuration plus a leading day count ("7d", "1d12h").
package duration

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Day is 24 hours; the unit "d" ignores daylight saving changes.
const Day = 24 * time.Hour

// ErrInvalid is wrapped by every error returned by Parse.
var ErrInvalid = errors.New("invalid duration")

var daysRe = regexp.MustCompile(`^(\d+)d(.*)$`)

// Parse is time.ParseDuration plus a leading day count: "7d", "1d12h".
func Parse(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if m := daysRe.FindStringSubmatch(s); m != nil {
		days, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil || days > math.MaxInt64/int64(Day) {
			return 0, fmt.Errorf("%w: %q", ErrInvalid, s)
		}
		d := time.Duration(days) * Day
		if m[2] == "" {
			return d, nil
		}
		rest, err := time.ParseDuration(m[2])
		if err != nil {
			return 0, fmt.Errorf("%w: %q", ErrInvalid, s)
		}
		return d + rest, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrInvalid, s)
	}
	return d, nil
}

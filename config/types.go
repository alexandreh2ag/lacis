package config

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
)

// ByteSize is a size in bytes, written "5MB" in the configuration.
type ByteSize int64

const (
	B  ByteSize = 1
	KB          = 1024 * B
	MB          = 1024 * KB
	GB          = 1024 * MB
)

var (
	ErrInvalidSize     = errors.New("invalid size")
	ErrInvalidDuration = errors.New("invalid duration")

	sizeRe = regexp.MustCompile(`^(\d+)\s*([KMG]I?B?|B)?$`)
	daysRe = regexp.MustCompile(`^(\d+)d(.*)$`)
)

// ParseByteSize parses "512", "512B", "64KB", "5MB", "1GB". The units are
// powers of 1024; "KiB", "K" and "Ki" are accepted as well.
func ParseByteSize(s string) (ByteSize, error) {
	m := sizeRe.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(s)))
	if m == nil {
		return 0, fmt.Errorf("%w: %q", ErrInvalidSize, s)
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrInvalidSize, s)
	}
	unit := B
	switch strings.TrimSuffix(strings.TrimSuffix(m[2], "B"), "I") {
	case "K":
		unit = KB
	case "M":
		unit = MB
	case "G":
		unit = GB
	}
	return ByteSize(n) * unit, nil
}

// ParseDuration is time.ParseDuration plus a leading day count: "7d",
// "1d12h".
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if m := daysRe.FindStringSubmatch(s); m != nil {
		days, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%w: %q", ErrInvalidDuration, s)
		}
		d := time.Duration(days) * day
		if m[2] == "" {
			return d, nil
		}
		rest, err := time.ParseDuration(m[2])
		if err != nil {
			return 0, fmt.Errorf("%w: %q", ErrInvalidDuration, s)
		}
		return d + rest, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrInvalidDuration, s)
	}
	return d, nil
}

var (
	durationType = reflect.TypeOf(time.Duration(0))
	byteSizeType = reflect.TypeOf(ByteSize(0))
)

// DurationHookFunc decodes a string with ParseDuration.
func DurationHookFunc() mapstructure.DecodeHookFuncType {
	return func(from, to reflect.Type, data any) (any, error) {
		if to != durationType || from.Kind() != reflect.String {
			return data, nil
		}
		return ParseDuration(data.(string))
	}
}

// ByteSizeHookFunc decodes a string with ParseByteSize.
func ByteSizeHookFunc() mapstructure.DecodeHookFuncType {
	return func(from, to reflect.Type, data any) (any, error) {
		if to != byteSizeType || from.Kind() != reflect.String {
			return data, nil
		}
		return ParseByteSize(data.(string))
	}
}

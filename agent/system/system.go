// Package system holds what the collectors share and read once per cycle:
// the distribution, the installed packages, the processes, the listening
// sockets and the launchers (supervisor programs). All but the first are
// read on first use, so that a cycle whose collectors do not need them
// never pays for them. Collectors read a System, they never change it; it
// is safe for concurrent use.
package system

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/spf13/afero"
)

// OSReleaseFiles are read in this order; the first one found wins.
var OSReleaseFiles = []string{"/etc/os-release", "/usr/lib/os-release"}

// Family is a group of distributions sharing their paths and tools.
type Family string

const (
	FamilyDebian  Family = "debian"
	FamilyRHEL    Family = "rhel"
	FamilyUnknown Family = "unknown"
)

type System struct {
	// OS is nil when no os-release file is found.
	OS     *snapshot.OS
	Family Family

	ctx       *context.Context
	packages  lazy[Packages]
	processes lazy[[]Process]
	listeners lazy[[]Listener]
	launchers lazy[[]Launcher]
}

// lazy computes a value once, on first use.
type lazy[T any] struct {
	once  sync.Once
	value T
	err   error
}

func (l *lazy[T]) get(fn func() (T, error)) (T, error) {
	l.once.Do(func() { l.value, l.err = fn() })
	return l.value, l.err
}

// Load reads the system once for a collection cycle.
func Load(ctx *context.Context) (*System, error) {
	sys := &System{Family: FamilyUnknown, ctx: ctx}
	for _, path := range OSReleaseFiles {
		f, err := ctx.Fs.Open(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		os, err := parseOSRelease(f)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		sys.OS = os
		sys.Family = family(os)
		break
	}
	return sys, nil
}

// parseOSRelease reads the os-release format: KEY=value lines, values
// optionally quoted.
func parseOSRelease(f afero.File) (*snapshot.OS, error) {
	os := &snapshot.OS{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !ok || strings.HasPrefix(key, "#") {
			continue
		}
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		} else {
			value = strings.Trim(value, `'"`)
		}
		switch key {
		case "ID":
			os.ID = value
		case "ID_LIKE":
			os.IDLike = strings.Fields(value)
		case "VERSION_ID":
			os.Version = value
		case "PRETTY_NAME":
			os.Pretty = value
		}
	}
	return os, scanner.Err()
}

func family(os *snapshot.OS) Family {
	ids := append([]string{os.ID}, os.IDLike...)
	switch {
	case slices.Contains(ids, "debian") || slices.Contains(ids, "ubuntu"):
		return FamilyDebian
	case slices.Contains(ids, "rhel") || slices.Contains(ids, "fedora") || slices.Contains(ids, "centos"):
		return FamilyRHEL
	default:
		return FamilyUnknown
	}
}

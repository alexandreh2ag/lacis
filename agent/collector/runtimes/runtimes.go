// Package runtimes collects the language runtimes installed or running on
// the host: PHP, Node.js (system, nvm, n, volta) and Java.
package runtimes

import (
	"bufio"
	"bytes"
	stdContext "context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/collector"
	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/spf13/afero"
)

const Name = "runtimes"

const (
	TypePHP  = "php"
	TypeNode = "node"
	TypeJava = "java"
)

// Globs are where the runtimes are looked for, besides the binaries of the
// running processes.
var Globs = map[string][]string{
	TypePHP: {"/usr/bin/php*", "/usr/local/bin/php*", "/opt/remi/php*/root/usr/bin/php"},
	TypeNode: {
		"/usr/bin/node", "/usr/local/bin/node",
		"/usr/local/n/versions/node/*/bin/node",
		"/home/*/.nvm/versions/node/*/bin/node", "/root/.nvm/versions/node/*/bin/node",
		// nvm installed for every user.
		"/var/lib/nvm/versions/node/*/bin/node", "/usr/local/nvm/versions/node/*/bin/node", "/opt/nvm/versions/node/*/bin/node",
		"/home/*/.volta/tools/image/node/*/bin/node", "/root/.volta/tools/image/node/*/bin/node",
	},
	TypeJava: {"/usr/lib/jvm/*/bin/java"},
}

var (
	phpBinary  = regexp.MustCompile(`^php(\d+(\.\d+)?)?$`)
	phpVersion = regexp.MustCompile(`^PHP (\d+\.\d+\.\d+)`)
	// Node version managers keep one directory per version.
	nodeManagers = []struct {
		name string
		re   *regexp.Regexp
	}{
		{"nvm", regexp.MustCompile(`/\.?nvm/versions/node/v?([^/]+)/bin/node$`)},
		{"n", regexp.MustCompile(`/n/versions/node/v?([^/]+)/bin/node$`)},
		{"volta", regexp.MustCompile(`/\.volta/tools/image/node/v?([^/]+)/bin/node$`)},
	}
)

func init() {
	collector.Register(Name, func(*context.Context) (collector.Collector, error) { return &Collector{}, nil })
}

type Collector struct {
	// candidates are found by Applicable and read by Collect, binary path
	// (links resolved) to runtime type.
	candidates map[string]string
	errs       []error
}

func (*Collector) Name() string { return Name }

// Applicable looks for runtime binaries on disk and among the processes.
func (c *Collector) Applicable(ctx *context.Context, sys *system.System) bool {
	c.candidates, c.errs = map[string]string{}, nil
	add := func(path, typ string) {
		resolved, err := sys.Resolve(path)
		if err != nil {
			c.errs = append(c.errs, err)
			return
		}
		c.candidates[resolved] = typ
	}
	for _, typ := range []string{TypePHP, TypeNode, TypeJava} {
		for _, glob := range Globs[typ] {
			matches, _ := afero.Glob(ctx.Fs, glob) // only ErrBadPattern
			for _, path := range matches {
				if typeOf(path) == typ {
					add(path, typ)
				}
			}
		}
	}
	processes, err := sys.Processes()
	if err != nil {
		c.errs = append(c.errs, err)
	}
	for _, p := range processes {
		// The binary of a containerized process is not on the host.
		if typ := typeOf(p.Exe); typ != "" && !p.InContainer {
			add(p.Exe, typ)
		}
	}
	return len(c.candidates) > 0 || len(c.errs) > 0
}

// typeOf returns the runtime type of a binary from its name.
func typeOf(path string) string {
	switch base := filepath.Base(path); {
	case phpBinary.MatchString(base):
		return TypePHP
	case base == "node":
		return TypeNode
	case base == "java":
		return TypeJava
	}
	return ""
}

// Collect reads the version of each runtime found. A runtime whose version
// cannot be read is reported and left out.
func (c *Collector) Collect(stdCtx stdContext.Context, ctx *context.Context, _ *system.System, out *snapshot.Snapshot) error {
	errs := c.errs
	for path, typ := range c.candidates {
		r, err := c.runtime(stdCtx, ctx, path, typ)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		out.Runtimes = append(out.Runtimes, r)
	}
	slices.SortFunc(out.Runtimes, func(a, b snapshot.Runtime) int {
		if c := strings.Compare(a.Type, b.Type); c != 0 {
			return c
		}
		if c := compareVersions(a.Version, b.Version); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })
	return errors.Join(errs...)
}

func (c *Collector) runtime(stdCtx stdContext.Context, ctx *context.Context, path, typ string) (snapshot.Runtime, error) {
	r := snapshot.Runtime{Type: typ, Path: path}
	var err error
	switch typ {
	case TypePHP:
		r.Version, err = run(stdCtx, ctx, phpVersion, path, "-n", "-v")
	case TypeNode:
		for _, m := range nodeManagers {
			if match := m.re.FindStringSubmatch(path); match != nil {
				r.Manager, r.Version = m.name, match[1]
				return r, nil
			}
		}
		r.Version, err = run(stdCtx, ctx, nil, path, "--version")
		r.Version = strings.TrimPrefix(r.Version, "v")
	case TypeJava:
		r.Version, err = javaVersion(ctx.Fs, filepath.Dir(filepath.Dir(path)))
	}
	return r, err
}

// run runs a binary and returns the first submatch of re in its output, or
// its first line when re is nil.
func run(stdCtx stdContext.Context, ctx *context.Context, re *regexp.Regexp, path string, args ...string) (string, error) {
	out, err := ctx.Executor.Run(stdCtx, path, args...)
	if err != nil {
		return "", err
	}
	first, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if re == nil {
		if first == "" {
			return "", errors.New("no version printed")
		}
		return first, nil
	}
	m := re.FindStringSubmatch(first)
	if m == nil {
		return "", fmt.Errorf("unexpected version %q", first)
	}
	return m[1], nil
}

// javaVersion reads JAVA_VERSION in the release file of a JDK or JRE home.
func javaVersion(fsys afero.Fs, home string) (string, error) {
	b, err := afero.ReadFile(fsys, filepath.Join(home, "release"))
	if err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(bytes.NewReader(b))
	for scanner.Scan() {
		if value, ok := strings.CutPrefix(scanner.Text(), "JAVA_VERSION="); ok {
			if v, err := strconv.Unquote(value); err == nil {
				return v, nil
			}
			return strings.Trim(value, `"`), nil
		}
	}
	return "", errors.New("no JAVA_VERSION in release")
}

// compareVersions compares dotted versions part by part, numerically when
// both parts are numbers: 20.9.0 < 20.19.3.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range min(len(pa), len(pb)) {
		na, errA := strconv.Atoi(pa[i])
		nb, errB := strconv.Atoi(pb[i])
		var c int
		if errA == nil && errB == nil {
			c = na - nb
		} else {
			c = strings.Compare(pa[i], pb[i])
		}
		if c != 0 {
			return c
		}
	}
	return len(pa) - len(pb)
}

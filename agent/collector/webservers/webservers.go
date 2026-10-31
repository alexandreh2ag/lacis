// Package webservers collects the webservers and proxies of the host with
// their routing: nginx, Apache, HAProxy and Traefik. A webserver
// is both an entry, through its domains and ports, and a source, through
// its roots, FastCGI and proxy targets.
package webservers

import (
	stdContext "context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/collector"
	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/snapshot"
)

const Name = "webservers"

const TypeNginx = "nginx"

func init() {
	collector.Register(Name, func(*context.Context) (collector.Collector, error) { return &Collector{}, nil })
}

// kind is a type of webserver: how to find its main process and read it.
type kind struct {
	typ   string
	match func(p system.Process) bool
	read  func(stdCtx stdContext.Context, ctx *context.Context, sys *system.System, p system.Process) (snapshot.Webserver, error)
	// inContainer accepts a containerized process: the webserver is read
	// through its API, not its files.
	inContainer bool
}

var kinds = []kind{
	{typ: TypeNginx, match: func(p system.Process) bool { return filepath.Base(p.Exe) == "nginx" }, read: readNginx},
	{typ: TypeApache, match: isApache, read: readApache},
	{typ: TypeHAProxy, match: func(p system.Process) bool { return filepath.Base(p.Exe) == "haproxy" }, read: readHAProxy},
	{typ: TypeTraefik, match: func(p system.Process) bool { return filepath.Base(p.Exe) == "traefik" }, read: readTraefik, inContainer: true},
}

type Collector struct {
	found []found
}

type found struct {
	kind    *kind
	process system.Process
}

func (*Collector) Name() string { return Name }

// Applicable looks for the main process of a known webserver, its workers
// and containerized ones left out.
func (c *Collector) Applicable(_ *context.Context, sys *system.System) bool {
	processes, err := sys.Processes()
	if err != nil {
		return true // reported by Collect
	}
	byPID := map[int]system.Process{}
	for _, p := range processes {
		byPID[p.PID] = p
	}
	c.found = nil
	for _, p := range processes {
		for i := range kinds {
			k := &kinds[i]
			if !k.match(p) || (p.InContainer && !k.inContainer) {
				continue
			}
			if parent, ok := byPID[p.PPID]; !ok || !k.match(parent) {
				c.found = append(c.found, found{kind: k, process: p})
			}
			break
		}
	}
	return len(c.found) > 0
}

// Collect reads each webserver found; one that cannot be read is reported
// with what could be read of it.
func (c *Collector) Collect(stdCtx stdContext.Context, ctx *context.Context, sys *system.System, out *snapshot.Snapshot) error {
	if _, err := sys.Processes(); err != nil {
		return err
	}
	var errs []error
	for _, f := range c.found {
		ws, err := f.kind.read(stdCtx, ctx, sys, f.process)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f.kind.typ, err))
		}
		ws.Type = f.kind.typ
		out.Webservers = append(out.Webservers, ws)
	}
	slices.SortStableFunc(out.Webservers, func(a, b snapshot.Webserver) int { return strings.Compare(a.Type, b.Type) })
	return errors.Join(errs...)
}

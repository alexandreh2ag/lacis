// Package collector runs the collectors of the agent. Each collector fills
// its part of the snapshot; Run gives each one its status, duration and
// timeout, and orders those depending on others.
package collector

import (
	stdContext "context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/snapshot"
)

type Collector interface {
	Name() string
	// Applicable makes a cheap test; false gives the status "skipped".
	Applicable(ctx *context.Context, sys *system.System) bool
	// Collect fills the part of out owned by the collector. It stops when
	// stdCtx is done. Collectors run concurrently: a collector only writes
	// its own fields, and only reads those of the collectors it depends on.
	Collect(stdCtx stdContext.Context, ctx *context.Context, sys *system.System, out *snapshot.Snapshot) error
}

// Dependent is implemented by a collector reading what other collectors
// produced: it runs once they are done.
type Dependent interface {
	DependsOn() []string
}

type CreateFn func(ctx *context.Context) (Collector, error)

var (
	registryMu sync.Mutex
	registry   = map[string]CreateFn{}
)

// Register adds a collector; called from the init function of its package.
func Register(name string, fn CreateFn) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = fn
}

var ErrDependencyCycle = errors.New("collector dependency cycle")

// Warning is an anomaly that does not make the collection fail: it goes to
// collector_errors while the status stays ok.
type Warning struct {
	Err error
}

func (w *Warning) Error() string { return w.Err.Error() }
func (w *Warning) Unwrap() error { return w.Err }

// Warn wraps err into a Warning.
func Warn(err error) error {
	return &Warning{Err: err}
}

// onlyWarnings reports whether err is made of warnings only.
func onlyWarnings(err error) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, e := range joined.Unwrap() {
			if !onlyWarnings(e) {
				return false
			}
		}
		return true
	}
	_, ok := err.(*Warning)
	return ok
}

// CreateCollectors creates every registered collector, sorted by name.
func CreateCollectors(ctx *context.Context) ([]Collector, error) {
	registryMu.Lock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	fns := make([]CreateFn, 0, len(registry))
	sort.Strings(names)
	for _, name := range names {
		fns = append(fns, registry[name])
	}
	registryMu.Unlock()

	collectors := make([]Collector, 0, len(fns))
	for i, fn := range fns {
		c, err := fn(ctx)
		if err != nil {
			return nil, fmt.Errorf("create collector %s: %w", names[i], err)
		}
		collectors = append(collectors, c)
	}
	for _, name := range ctx.Config.Collectors.Disabled {
		if !slices.Contains(names, name) {
			ctx.Logger.Warn("unknown collector in collectors.disabled", "collector", name)
		}
	}
	if err := checkCycles(collectors); err != nil {
		return nil, err
	}
	return collectors, nil
}

func dependencies(c Collector) []string {
	if d, ok := c.(Dependent); ok {
		return d.DependsOn()
	}
	return nil
}

// checkCycles rejects dependencies that would make Run wait forever.
func checkCycles(collectors []Collector) error {
	deps := map[string][]string{}
	for _, c := range collectors {
		deps[c.Name()] = dependencies(c)
	}
	const (
		visiting = 1
		done     = 2
	)
	state := map[string]int{}
	var visit func(name string, path []string) error
	visit = func(name string, path []string) error {
		switch state[name] {
		case visiting:
			return fmt.Errorf("%w: %v", ErrDependencyCycle, append(path, name))
		case done:
			return nil
		}
		state[name] = visiting
		for _, dep := range deps[name] {
			if err := visit(dep, append(path, name)); err != nil {
				return err
			}
		}
		state[name] = done
		return nil
	}
	for _, c := range collectors {
		if err := visit(c.Name(), nil); err != nil {
			return err
		}
	}
	return nil
}

// Run runs the collectors and records their statuses and errors in out, in
// the order of collectors. A collector starts once those it depends on
// are done, whatever their status; an unknown dependency is ignored.
func Run(stdCtx stdContext.Context, ctx *context.Context, sys *system.System, collectors []Collector, out *snapshot.Snapshot) {
	done := make(map[string]chan struct{}, len(collectors))
	for _, c := range collectors {
		done[c.Name()] = make(chan struct{})
	}
	runs := make([]snapshot.CollectorRun, len(collectors))
	errs := make([]error, len(collectors))

	var wg sync.WaitGroup
	for i, c := range collectors {
		wg.Go(func() {
			defer close(done[c.Name()])
			for _, dep := range dependencies(c) {
				if ch, ok := done[dep]; ok {
					<-ch
				}
			}
			runs[i], errs[i] = runOne(stdCtx, ctx, sys, c, out)
		})
	}
	wg.Wait()

	out.Collectors = runs
	for i, err := range errs {
		if err != nil {
			out.CollectorErrors = append(out.CollectorErrors, snapshot.CollectorError{Collector: runs[i].Name, Error: err.Error()})
		}
	}
}

func runOne(stdCtx stdContext.Context, ctx *context.Context, sys *system.System, c Collector, out *snapshot.Snapshot) (snapshot.CollectorRun, error) {
	run := snapshot.CollectorRun{Name: c.Name(), Status: snapshot.CollectorStatusDisabled}
	if slices.Contains(ctx.Config.Collectors.Disabled, c.Name()) {
		return run, nil
	}
	start := ctx.Clock.Now()
	applicable := false
	err := safely(func() error {
		if applicable = c.Applicable(ctx, sys); !applicable {
			return nil
		}
		timeout := ctx.Config.Collectors.Timeout
		cCtx, cancel := stdContext.WithTimeout(stdCtx, timeout)
		defer cancel()
		err := c.Collect(cCtx, ctx, sys, out)
		if err != nil && errors.Is(cCtx.Err(), stdContext.DeadlineExceeded) {
			err = fmt.Errorf("timeout after %s: %w", timeout, err)
		}
		return err
	})
	switch {
	case err != nil && onlyWarnings(err):
		run.Status = snapshot.CollectorStatusOK
		ctx.Logger.Warn("collector warning", "collector", c.Name(), "warning", err)
	case err != nil:
		run.Status = snapshot.CollectorStatusError
		ctx.Logger.Warn("collector failed", "collector", c.Name(), "error", err)
	case !applicable:
		run.Status = snapshot.CollectorStatusSkipped
		return run, nil
	default:
		run.Status = snapshot.CollectorStatusOK
	}
	run.DurationMs = ctx.Clock.Now().Sub(start).Milliseconds()
	return run, err
}

// safely runs fn, turning a panic into an error.
func safely(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return fn()
}

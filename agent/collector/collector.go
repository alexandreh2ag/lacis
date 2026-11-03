// Package collector runs the collectors of the agent. Each collector fills
// its part of the snapshot; Run gives each one its status, duration and
// timeout, and orders those depending on others.
package collector

import (
	stdContext "context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"sync"
	"time"
	"unicode/utf8"

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

// abandonGrace is how long Run waits for a collector past its timeout
// before abandoning it: a collector blocked in a read that ignores its
// context (a FIFO, a hung NFS mount) must not stop the snapshots.
var abandonGrace = 5 * time.Second

// MaxErrorLength caps the error of a collector sent in the snapshot.
const MaxErrorLength = 512

// Run runs the collectors and records their statuses and errors in out, in
// the order of collectors. A collector starts once those it depends on
// are done, whatever their status; an unknown dependency is ignored.
//
// Each collector works on its own copy of out, merged back when it ends in
// time: a collector abandoned after its timeout keeps running in the
// background, but never touches out again.
func Run(stdCtx stdContext.Context, ctx *context.Context, sys *system.System, collectors []Collector, out *snapshot.Snapshot) {
	done := make(map[string]chan struct{}, len(collectors))
	for _, c := range collectors {
		done[c.Name()] = make(chan struct{})
	}
	runs := make([]snapshot.CollectorRun, len(collectors))
	errs := make([]error, len(collectors))

	var mu sync.Mutex // guards out
	var wg sync.WaitGroup
	for i, c := range collectors {
		wg.Go(func() {
			defer close(done[c.Name()])
			for _, dep := range dependencies(c) {
				if ch, ok := done[dep]; ok {
					<-ch
				}
			}
			runs[i], errs[i] = runIsolated(stdCtx, ctx, sys, c, &mu, out)
		})
	}
	wg.Wait() // each run ends at the latest abandonGrace after its timeout

	out.Collectors = runs
	for i, err := range errs {
		if err != nil {
			out.CollectorErrors = append(out.CollectorErrors, snapshot.CollectorError{Collector: runs[i].Name, Error: truncate(err.Error(), MaxErrorLength)})
		}
	}
}

// truncate cuts s to at most n bytes, on a rune boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	const ellipsis = "…"
	cut := n - len(ellipsis)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + ellipsis
}

// runIsolated runs a collector on a copy of out, and merges what it
// changed back into out when it ends before its timeout and abandonGrace.
func runIsolated(stdCtx stdContext.Context, ctx *context.Context, sys *system.System, c Collector, mu *sync.Mutex, out *snapshot.Snapshot) (snapshot.CollectorRun, error) {
	mu.Lock()
	base, err := clone(out)
	var local *snapshot.Snapshot
	if err == nil {
		local, err = clone(out)
	}
	mu.Unlock()
	if err != nil {
		return snapshot.CollectorRun{Name: c.Name(), Status: snapshot.CollectorStatusError}, err
	}

	type result struct {
		run snapshot.CollectorRun
		err error
	}
	results := make(chan result, 1)
	start := ctx.Clock.Now()
	go func() {
		run, err := runOne(stdCtx, ctx, sys, c, local)
		results <- result{run, err}
	}()
	timer := time.NewTimer(ctx.Config.Collectors.Timeout + abandonGrace)
	defer timer.Stop()
	select {
	case r := <-results:
		mu.Lock()
		merge(out, base, local)
		mu.Unlock()
		return r.run, r.err
	case <-timer.C:
		err := fmt.Errorf("abandoned, still running %s after its timeout", ctx.Config.Collectors.Timeout+abandonGrace)
		ctx.Logger.Warn("collector abandoned", "collector", c.Name(), "error", err)
		return snapshot.CollectorRun{
			Name:       c.Name(),
			Status:     snapshot.CollectorStatusError,
			DurationMs: ctx.Clock.Now().Sub(start).Milliseconds(),
		}, err
	}
}

// clone deep copies a snapshot through its JSON form.
func clone(s *snapshot.Snapshot) (*snapshot.Snapshot, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("copy snapshot: %w", err)
	}
	c := &snapshot.Snapshot{}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("copy snapshot: %w", err)
	}
	return c, nil
}

// merge copies into out the fields a collector changed in local, base
// being local before the collector ran. Collectors write their own fields,
// or those of the collectors they depend on, never concurrently. The runs
// and errors of the collectors are Run's.
func merge(out, base, local *snapshot.Snapshot) {
	o, b, l := reflect.ValueOf(out).Elem(), reflect.ValueOf(base).Elem(), reflect.ValueOf(local).Elem()
	for i := range o.NumField() {
		switch o.Type().Field(i).Name {
		case "Collectors", "CollectorErrors":
			continue
		}
		if !reflect.DeepEqual(b.Field(i).Interface(), l.Field(i).Interface()) {
			o.Field(i).Set(l.Field(i))
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

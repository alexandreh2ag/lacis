package collector

import (
	"bytes"
	stdContext "context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fake is a configurable collector.
type fake struct {
	name       string
	applicable bool
	deps       []string
	collect    func(stdCtx stdContext.Context, out *snapshot.Snapshot) error
}

func (f *fake) Name() string                                     { return f.name }
func (f *fake) Applicable(*context.Context, *system.System) bool { return f.applicable }
func (f *fake) DependsOn() []string                              { return f.deps }
func (f *fake) Collect(stdCtx stdContext.Context, _ *context.Context, _ *system.System, out *snapshot.Snapshot) error {
	if f.collect == nil {
		return nil
	}
	return f.collect(stdCtx, out)
}

// withRegistry replaces the registry for the test.
func withRegistry(t *testing.T, fns map[string]CreateFn) {
	registryMu.Lock()
	previous := registry
	registry = map[string]CreateFn{}
	registryMu.Unlock()
	for name, fn := range fns {
		Register(name, fn)
	}
	t.Cleanup(func() {
		registryMu.Lock()
		registry = previous
		registryMu.Unlock()
	})
}

func create(c Collector) CreateFn {
	return func(*context.Context) (Collector, error) { return c, nil }
}

func TestCreateCollectors(t *testing.T) {
	withRegistry(t, map[string]CreateFn{
		"web":  create(&fake{name: "web", deps: []string{"host"}}),
		"host": create(&fake{name: "host"}),
	})
	logs := &bytes.Buffer{}
	ctx := context.TestContext(logs)
	ctx.Config.Collectors.Disabled = []string{"web", "dockr"}

	got, err := CreateCollectors(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "host", got[0].Name(), "sorted by name")
	assert.Equal(t, "web", got[1].Name())
	assert.Contains(t, logs.String(), "collector=dockr")
	assert.NotContains(t, logs.String(), "collector=web")
}

func TestCreateCollectors_Errors(t *testing.T) {
	t.Run("creation fails", func(t *testing.T) {
		withRegistry(t, map[string]CreateFn{
			"host": func(*context.Context) (Collector, error) { return nil, errors.New("boom") },
		})
		_, err := CreateCollectors(context.TestContext(nil))
		assert.EqualError(t, err, "create collector host: boom")
	})
	t.Run("cycle", func(t *testing.T) {
		withRegistry(t, map[string]CreateFn{
			"a": create(&fake{name: "a", deps: []string{"b"}}),
			"b": create(&fake{name: "b", deps: []string{"c"}}),
			"c": create(&fake{name: "c", deps: []string{"a"}}),
			"d": create(&fake{name: "d", deps: []string{"a"}}),
		})
		_, err := CreateCollectors(context.TestContext(nil))
		assert.ErrorIs(t, err, ErrDependencyCycle)
		assert.ErrorContains(t, err, "[a b c a]")
	})
}

func TestRun_Statuses(t *testing.T) {
	ctx := context.TestContext(nil)
	ctx.Config.Collectors.Disabled = []string{"off"}
	ctx.Config.Collectors.Timeout = 50 * time.Millisecond
	collectors := []Collector{
		&fake{name: "ok", applicable: true, collect: func(_ stdContext.Context, out *snapshot.Snapshot) error {
			out.Host.Kernel = "6.8"
			return nil
		}},
		&fake{name: "absent"},
		&fake{name: "off", applicable: true, collect: func(stdContext.Context, *snapshot.Snapshot) error {
			t.Error("a disabled collector must not run")
			return nil
		}},
		&fake{name: "broken", applicable: true, collect: func(stdContext.Context, *snapshot.Snapshot) error {
			return errors.New("permission denied")
		}},
		&fake{name: "panics", applicable: true, collect: func(stdContext.Context, *snapshot.Snapshot) error {
			panic("nil map")
		}},
		&fake{name: "warns", applicable: true, collect: func(stdContext.Context, *snapshot.Snapshot) error {
			return errors.Join(Warn(errors.New("two version files")), Warn(errors.New("max_files reached")))
		}},
		&fake{name: "slow", applicable: true, collect: func(stdCtx stdContext.Context, _ *snapshot.Snapshot) error {
			<-stdCtx.Done()
			return stdCtx.Err()
		}},
	}
	out := snapshot.New(snapshot.AgentInfo{}, ctx.Config.Labels, "host")

	Run(t.Context(), ctx, &system.System{}, collectors, out)

	assert.Equal(t, "6.8", out.Host.Kernel)
	statuses := map[string]snapshot.CollectorStatus{}
	for _, r := range out.Collectors {
		statuses[r.Name] = r.Status
	}
	assert.Equal(t, map[string]snapshot.CollectorStatus{
		"ok": "ok", "absent": "skipped", "off": "disabled", "broken": "error", "panics": "error", "warns": "ok", "slow": "error",
	}, statuses)
	assert.Equal(t, []string{"ok", "absent", "off", "broken", "panics", "warns", "slow"}, names(out.Collectors), "in the order given")
	assert.Equal(t, []snapshot.CollectorError{
		{Collector: "broken", Error: "permission denied"},
		{Collector: "panics", Error: "panic: nil map"},
		{Collector: "warns", Error: "two version files\nmax_files reached"},
		{Collector: "slow", Error: "timeout after 50ms: context deadline exceeded"},
	}, out.CollectorErrors)
	assert.Zero(t, out.Collectors[1].DurationMs, "no duration when skipped")
	assert.GreaterOrEqual(t, out.Collectors[6].DurationMs, int64(50))
}

func names(runs []snapshot.CollectorRun) []string {
	out := make([]string, len(runs))
	for i, r := range runs {
		out[i] = r.Name
	}
	return out
}

func TestRun_Dependencies(t *testing.T) {
	var webDone atomic.Bool
	var order []string
	collectors := []Collector{
		&fake{name: "apps", applicable: true, deps: []string{"web", "missing"}, collect: func(stdContext.Context, *snapshot.Snapshot) error {
			assert.True(t, webDone.Load(), "apps runs after web")
			order = append(order, "apps")
			return nil
		}},
		&fake{name: "web", applicable: true, collect: func(stdContext.Context, *snapshot.Snapshot) error {
			time.Sleep(20 * time.Millisecond)
			webDone.Store(true)
			return errors.New("nginx -T failed")
		}},
	}
	out := &snapshot.Snapshot{}
	Run(t.Context(), context.TestContext(nil), &system.System{}, collectors, out)

	assert.Equal(t, []string{"apps"}, order, "a failed dependency still lets its dependents run")
	assert.Equal(t, snapshot.CollectorStatusOK, out.Collectors[0].Status)
}

func TestOnlyWarnings(t *testing.T) {
	w := Warn(errors.New("w"))
	assert.True(t, onlyWarnings(w))
	assert.True(t, onlyWarnings(errors.Join(w, errors.Join(w))))
	assert.False(t, onlyWarnings(errors.Join(w, errors.New("e"))))
	assert.False(t, onlyWarnings(errors.New("e")))
	assert.ErrorIs(t, w, w.(*Warning).Err)
}

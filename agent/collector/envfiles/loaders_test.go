package envfiles

import (
	stdContext "context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/agent/config"
	"github.com/alexandreh2ag/lacis/common/dsn"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestCollect_Frameworks(t *testing.T) {
	ctx, _ := newContext(t, map[string]string{
		"/srv/laravel/.env":            "APP_ENV=production\nDB_HOST=10.0.0.1\nDB_PORT=3306\nDB_PASSWORD=secret\nredis_url=redis://10.0.0.2:6379/1\n",
		"/srv/laravel/.env.production": "DB_HOST=10.0.0.99\n",
		"/srv/nuxt/.env":               "NUXT_API_URL=http://10.0.0.3:3001/api/token\n",
		"/srv/nuxt/.env.production":    "NUXT_API_URL=http://10.0.0.4:3001\n",
		"/srv/nuxt-dev/.env":           "NODE_ENV=staging\nNUXT_API_URL=http://10.0.0.3:3001\n",
		"/srv/nuxt-dev/.env.staging":   "NUXT_API_URL=http://10.0.0.5:3001\n",
		"/srv/nuxt-escape/.env":        "NODE_ENV=../../etc/x\nNUXT_API_URL=http://10.0.0.3:3001\n",
		"/srv/etc/x":                   "NUXT_API_URL=http://10.0.0.66:3001\n",
		"/srv/nest/.env":               "MONGO_URL=mongodb://u:p@10.0.0.6:27017/catalog\n",
		"/srv/nest/.env.local":         "MONGO_URL=mongodb://10.0.0.99/ignored\n",
		"/srv/sym/conf/app.env":        "QUEUE_URL=amqp://10.0.0.7\n",
		"/srv/sym/.env":                "QUEUE_URL=amqp://10.0.0.99\n",
	})
	// A declarative detector with env sources replaces the built-in loader.
	ctx.Config.Collectors.Apps.Frameworks = []config.FrameworkDetector{
		{Name: "symfony", Config: []config.FrameworkSource{{Path: "conf/${MISSING}.env", Format: "env"}, {Path: "conf/*.env", Format: "env"}}},
		{Name: "nestjs", Detect: config.FrameworkDetect{File: "nest-cli.json"}},
	}
	apps, err := collect(t, ctx,
		snapshot.App{Path: "/srv/laravel", Framework: &snapshot.Framework{Name: "laravel"}},
		snapshot.App{Path: "/srv/nuxt", Framework: &snapshot.Framework{Name: "nuxt"}},
		snapshot.App{Path: "/srv/nuxt-dev", Framework: &snapshot.Framework{Name: "nuxt"}},
		snapshot.App{Path: "/srv/nuxt-escape", Framework: &snapshot.Framework{Name: "nuxt"}},
		snapshot.App{Path: "/srv/nest", Framework: &snapshot.Framework{Name: "nestjs"}},
		snapshot.App{Path: "/srv/sym", Framework: &snapshot.Framework{Name: "symfony"}},
	)
	require.NoError(t, err)
	targets := func(app snapshot.App) []string {
		var out []string
		for _, d := range app.Dependencies {
			out = append(out, fmt.Sprintf("%s=%s://%s:%d/%s", d.Key, d.Scheme, d.Target.Value, d.Port, d.Path))
		}
		return out
	}
	assert.Equal(t, []string{"DB_HOST=://10.0.0.1:3306/", "redis_url=redis://10.0.0.2:6379/1"}, targets(apps[0]), "laravel: .env alone, lower case keys")
	assert.Equal(t, []string{"NUXT_API_URL=http://10.0.0.4:3001/"}, targets(apps[1]), "nuxt: .env.production by default")
	assert.Equal(t, []string{"NUXT_API_URL=http://10.0.0.5:3001/"}, targets(apps[2]), "nuxt: .env.<NODE_ENV>")
	assert.Equal(t, []string{"NUXT_API_URL=http://10.0.0.3:3001/"}, targets(apps[3]), "a NODE_ENV that is a path is not followed")
	assert.Equal(t, []string{"MONGO_URL=mongodb://10.0.0.6:27017/catalog"}, targets(apps[4]), "nestjs: .env; a detector without sources keeps the built-in loader")
	assert.Equal(t, []string{"QUEUE_URL=amqp://10.0.0.7:5672/"}, targets(apps[5]))
}

type fakeLoader struct{}

func (fakeLoader) Framework() string { return "fake" }

func (fakeLoader) Load(*reader, string, *snapshot.App) (env, []block, error) {
	return nil, []block{{path: "x", values: map[string]value{"host": {value: "10.0.0.8"}}}}, errors.New("partial")
}

func TestRegisterLoader(t *testing.T) {
	RegisterLoader(fakeLoader{})
	t.Cleanup(func() { delete(loaders, "fake") })
	ctx, _ := newContext(t, nil)
	apps, err := collect(t, ctx, snapshot.App{Path: "/srv/x", Framework: &snapshot.Framework{Name: "fake"}})
	assert.ErrorContains(t, err, "partial")
	require.Len(t, apps[0].Dependencies, 1)
	assert.Equal(t, "10.0.0.8", apps[0].Dependencies[0].Target.Value)
	for _, name := range []string{"symfony", "zend1", "next", "laravel", "nuxt", "nestjs"} {
		assert.Equal(t, name, loaders[name].Framework())
	}
}

func TestResolveAll_Parallel(t *testing.T) {
	ctx, k := newContext(t, nil)
	var inFlight, peak atomic.Int32
	k.EXPECT().LookupHost(gomock.Any(), gomock.Any()).DoAndReturn(func(_ stdContext.Context, name string) ([]string, error) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		return []string{"10.0.0." + strings.TrimPrefix(strings.TrimSuffix(name, ".example.com"), "h")}, nil
	}).Times(20)
	r := newResolver(ctx, t.Context())
	var targets []*snapshot.Target
	for i := range 20 {
		targets = append(targets, r.target(dsn.DSN{Host: fmt.Sprintf("h%d.example.com", i)}))
	}
	start := time.Now()
	r.resolveAll()
	assert.Less(t, time.Since(start), 20*20*time.Millisecond, "names resolved in parallel")
	assert.LessOrEqual(t, peak.Load(), int32(LookupConcurrency))
	assert.Greater(t, peak.Load(), int32(1))
	for i, target := range targets {
		assert.Equal(t, []string{fmt.Sprintf("10.0.0.%d", i)}, target.Resolved)
	}
}

func TestResolveAll_Budget(t *testing.T) {
	budget, timeout := ResolveBudget, LookupTimeout
	ResolveBudget, LookupTimeout = 100*time.Millisecond, time.Second
	t.Cleanup(func() { ResolveBudget, LookupTimeout = budget, timeout })

	ctx, k := newContext(t, nil)
	// Every lookup hangs until its context ends: the budget ends them all.
	k.EXPECT().LookupHost(gomock.Any(), gomock.Any()).DoAndReturn(func(c stdContext.Context, _ string) ([]string, error) {
		<-c.Done()
		return nil, c.Err()
	}).MinTimes(1).MaxTimes(LookupConcurrency + 1)
	r := newResolver(ctx, t.Context())
	var targets []*snapshot.Target
	for i := range 30 {
		targets = append(targets, r.target(dsn.DSN{Host: fmt.Sprintf("dead%d.example.com", i)}))
	}
	start := time.Now()
	r.resolveAll()
	assert.Less(t, time.Since(start), 900*time.Millisecond, "the budget bounds the whole resolution")
	for _, target := range targets {
		assert.Equal(t, snapshot.ResolverFailed, target.Resolver)
		assert.Empty(t, target.Resolved)
	}
}

// FuzzParseDotenv checks that parseDotenv and resolve never panic nor
// loop, whatever the file.
func FuzzParseDotenv(f *testing.F) {
	for _, seed := range []string{
		"A=1\nB='lit ${A}'\nC=\"multi\nline ${A} \\\"q\\\" ${MISSING:-def}\"\nD=${A}x # comment\n",
		"export A=${A}\nB=${C}\nC=${B}\n",
		"A='open\n",
		"A=\"x\\",
		"=x\n A B=x\n",
		"G='-----BEGIN KEY-----\nabc\n-----END KEY-----'\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, content string) {
		e := env{}
		_ = e.parseDotenv("f", content)
		e.resolve()
		for key, v := range e {
			if key == "" || v.line < 1 {
				t.Fatalf("key %q at line %d", key, v.line)
			}
		}
	})
}

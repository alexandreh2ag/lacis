package envfiles

import (
	"errors"
	"slices"
	"testing"

	"github.com/alexandreh2ag/lacis/agent/collector"
	"github.com/alexandreh2ag/lacis/agent/config"
	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/fakefs"
	mockKernel "github.com/alexandreh2ag/lacis/agent/mocks/kernel"
	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/dsn"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func newContext(t *testing.T, files map[string]string) (*context.Context, *mockKernel.MockKernel) {
	f := fakefs.New()
	f.Files(t, files)
	ctx := context.TestContext(nil)
	ctx.Fs = f
	k := mockKernel.NewMockKernel(gomock.NewController(t))
	ctx.Kernel = k
	return ctx, k
}

func collect(t *testing.T, ctx *context.Context, apps ...snapshot.App) ([]snapshot.App, error) {
	sys, err := system.Load(ctx)
	require.NoError(t, err)
	out := &snapshot.Snapshot{Apps: apps}
	c := &Collector{}
	require.True(t, c.Applicable(ctx, sys))
	err = c.Collect(t.Context(), ctx, sys, out)
	return out.Apps, err
}

func TestCollect_Symfony(t *testing.T) {
	ctx, k := newContext(t, map[string]string{
		HostsFile: "127.0.0.1 localhost\n10.0.1.20 db.internal db # primary\n",
		"/var/www/api/.env": `# defaults
APP_ENV=dev
APP_SECRET=s3cr3t
DATABASE_URL="postgresql://app:p4ss@db.internal:5432/app_${APP_ENV}?serverVersion=16"
REDIS_URL=redis://localhost:6379/2
MESSENGER_TRANSPORT_DSN=doctrine://default
MAILER_DSN=null://null
`,
		"/var/www/api/.env.local": "APP_ENV=prod\nSENTRY_DSN=https://key@o1.ingest.sentry.io/42\n",
		"/var/www/api/.env.prod":  "export ELASTICSEARCH_HOST=search.internal\nELASTICSEARCH_PORT=9201\nAPI_URL=https://api.partner.fr/v2 # partner\n",
		"/var/www/api/.env.prod.local": `RABBITMQ_URL='amqp://guest:guest@10.0.1.30:5672/%2f'
CACHE_DSN=redis://10.0.1.40:6379,10.0.1.41:6379
SESSION_URL=redis://${UNDEFINED_HOST}:6379
AWS_KEY_URL=https://s3.amazonaws.com
`,
		"/etc/supervisor/conf.d/w.conf": "[program:consumer]\ncommand=php bin/console messenger:consume\nenvironment=APP_ENV=\"prod\",REDIS_URL=\"redis://cache.internal:6380\"\n",
	})
	k.EXPECT().LookupHost(gomock.Any(), "search.internal").Return([]string{"10.0.1.51", "10.0.1.50"}, nil)
	k.EXPECT().LookupHost(gomock.Any(), "api.partner.fr").Return(nil, errors.New("no such host"))
	k.EXPECT().LookupHost(gomock.Any(), "cache.internal").Return([]string{"10.0.1.60"}, nil)
	k.EXPECT().LookupHost(gomock.Any(), "o1.ingest.sentry.io").Return([]string{"34.120.195.249"}, nil)

	apps, err := collect(t, ctx, snapshot.App{
		Path: "/var/www/api", Framework: &snapshot.Framework{Name: "symfony"},
		Launchers: []snapshot.Launcher{{Name: "consumer"}},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"APP_ENV": "prod"}, apps[0].Info)
	assert.Equal(t, []snapshot.Dependency{
		{Key: "API_URL", Source: snapshot.DependencySource{File: "/var/www/api/.env.prod", Line: 3}, Scheme: "https", Port: 443, Path: "v2",
			Target: &snapshot.Target{Kind: "name", Value: "api.partner.fr", Resolver: "failed"}},
		// A cluster gives one dependency per host.
		{Key: "CACHE_DSN", Source: snapshot.DependencySource{File: "/var/www/api/.env.prod.local", Line: 2}, Scheme: "redis", Port: 6379,
			Target: &snapshot.Target{Kind: "ip", Value: "10.0.1.40"}},
		{Key: "CACHE_DSN", Source: snapshot.DependencySource{File: "/var/www/api/.env.prod.local", Line: 2}, Scheme: "redis", Port: 6379,
			Target: &snapshot.Target{Kind: "ip", Value: "10.0.1.41"}},
		{Key: "DATABASE_URL", Source: snapshot.DependencySource{File: "/var/www/api/.env", Line: 4}, Scheme: "postgresql", Port: 5432, Path: "app_prod", // ${APP_ENV} of .env.local
			Target: &snapshot.Target{Kind: "name", Value: "db.internal", Resolved: []string{"10.0.1.20"}, Resolver: "hosts"}},
		{Key: "ELASTICSEARCH_HOST", Source: snapshot.DependencySource{File: "/var/www/api/.env.prod", Line: 1}, Port: 9201,
			Target: &snapshot.Target{Kind: "name", Value: "search.internal", Resolved: []string{"10.0.1.50", "10.0.1.51"}, Resolver: "dns"}},
		{Key: "RABBITMQ_URL", Source: snapshot.DependencySource{File: "/var/www/api/.env.prod.local", Line: 1}, Scheme: "amqp", Port: 5672,
			Target: &snapshot.Target{Kind: "ip", Value: "10.0.1.30"}},
		{Key: "REDIS_URL", Source: snapshot.DependencySource{File: "/etc/supervisor/conf.d/w.conf"}, Scheme: "redis", Port: 6380,
			Target: &snapshot.Target{Kind: "name", Value: "cache.internal", Resolved: []string{"10.0.1.60"}, Resolver: "dns"}},
		{Key: "SENTRY_DSN", Source: snapshot.DependencySource{File: "/var/www/api/.env.local", Line: 2}, Scheme: "https", Port: 443, Path: "42",
			Target: &snapshot.Target{Kind: "name", Value: "o1.ingest.sentry.io", Resolved: []string{"34.120.195.249"}, Resolver: "dns"}},
		{Key: "SESSION_URL", Source: snapshot.DependencySource{File: "/var/www/api/.env.prod.local", Line: 3}, Unparsed: true},
	}, apps[0].Dependencies, "AWS_KEY_URL, APP_SECRET denied; doctrine and null not network")
}

func TestCollect_SymfonyDumpEnv(t *testing.T) {
	ctx, _ := newContext(t, map[string]string{
		"/srv/api/releases/3/.env.local.php": `<?php

// This file was generated by running "composer dump-env prod"

return array (
  'APP_ENV' => 'prod',
  'APP_DEBUG' => false,
  "DATABASE_URL" => 'mysql://u:p@127.0.0.1:3306/shop?serverVersion=mariadb-11.8.6',
  'REDIS_URL' => 'redis:///run/redis/redis.sock',
);
`,
		"/srv/api/releases/3/.env": "DATABASE_URL=mysql://ignored:3306/x\n",
	})
	apps, err := collect(t, ctx, snapshot.App{Path: "/srv/api/current", ResolvedPath: "/srv/api/releases/3", Framework: &snapshot.Framework{Name: "symfony"}})
	require.NoError(t, err)
	assert.Equal(t, []snapshot.Dependency{
		{Key: "DATABASE_URL", Source: snapshot.DependencySource{File: "/srv/api/releases/3/.env.local.php", Line: 8}, Scheme: "mysql", Port: 3306, Path: "shop",
			Target: &snapshot.Target{Kind: "ip", Value: "127.0.0.1"}},
		{Key: "REDIS_URL", Source: snapshot.DependencySource{File: "/srv/api/releases/3/.env.local.php", Line: 9}, Scheme: "redis",
			Target: &snapshot.Target{Kind: "socket", Value: "/run/redis/redis.sock"}},
	}, apps[0].Dependencies)
}

func TestCollect_NextAndOthers(t *testing.T) {
	ctx, _ := newContext(t, map[string]string{
		"/srv/front/.env":                  "NEXT_PUBLIC_API_URL=http://10.0.0.1:8000\nNODE_ENV=development\n",
		"/srv/front/.env.production":       "NEXT_PUBLIC_API_URL=http://10.0.0.2:8000\n",
		"/srv/front/.env.local":            "NODE_ENV=production\n",
		"/srv/front/.env.production.local": "NEXT_PUBLIC_API_URL=http://10.0.0.3:8000\n",
		"/srv/plain/.env":                  "DB_HOST=10.0.0.9\nDB_PORT=3306\n",
		"/srv/custom/conf/app.env":         "QUEUE_URL=amqp://10.0.0.7\n",
		"/srv/custom/.env":                 "QUEUE_URL=amqp://ignored\n",
	})
	ctx.Config.Collectors.Apps.Frameworks = []config.FrameworkDetector{
		{Name: "custom", Config: []config.FrameworkSource{{Path: "conf/*.env", Format: "env"}, {Path: "conf/app.php", Format: "php"}}},
	}
	apps, err := collect(t, ctx,
		snapshot.App{Path: "/srv/front", Framework: &snapshot.Framework{Name: "next"}},
		snapshot.App{Path: "/srv/plain"},
		snapshot.App{Path: "/srv/custom", Framework: &snapshot.Framework{Name: "custom"}},
		snapshot.App{Path: "/srv/none"},
	)
	require.NoError(t, err)
	assert.Equal(t, "http://10.0.0.3:8000", "http://"+apps[0].Dependencies[0].Target.Value+":8000")
	assert.Equal(t, map[string]string{"NODE_ENV": "production"}, apps[0].Info)
	assert.Equal(t, []snapshot.Dependency{{Key: "DB_HOST", Source: snapshot.DependencySource{File: "/srv/plain/.env", Line: 1}, Port: 3306,
		Target: &snapshot.Target{Kind: "ip", Value: "10.0.0.9"}}}, apps[1].Dependencies)
	assert.Equal(t, "10.0.0.7", apps[2].Dependencies[0].Target.Value)
	assert.Equal(t, []snapshot.Dependency{}, apps[3].Dependencies)
	assert.Nil(t, apps[3].Info)
}

func TestCollect_BrokenFile(t *testing.T) {
	ctx, _ := newContext(t, map[string]string{"/srv/app/.env": "OK_URL=http://10.0.0.1\nnot a line\nAFTER_URL=http://10.0.0.2\nMAILER_DSN=sendmail://default\n"})
	apps, err := collect(t, ctx, snapshot.App{Path: "/srv/app"})
	assert.EqualError(t, err, "/srv/app: /srv/app/.env:2: invalid line")
	var w *collector.Warning
	assert.ErrorAs(t, err, &w, "a warning: the status stays ok")
	assert.Len(t, apps[0].Dependencies, 2, "the other lines are read, sendmail left out")
}

func TestParseDotenv(t *testing.T) {
	e := env{}
	require.NoError(t, e.parseDotenv("f", "A=1\nB='lit ${A}'\nC=\"multi\nline ${A} \\\"q\\\" ${MISSING:-def}\"\nD=${A}x # comment\n\nE=\nF=$NOPE\nG='-----BEGIN KEY-----\nabc\n-----END KEY-----'\nH=after\n"))
	e.resolve()
	assert.Equal(t, "1", e["A"].value)
	assert.Equal(t, "lit ${A}", e["B"].value)
	assert.Equal(t, "multi\nline 1 \"q\" def", e["C"].value)
	assert.Equal(t, 3, e["C"].line)
	assert.Equal(t, "1x", e["D"].value)
	assert.Equal(t, 5, e["D"].line)
	assert.Equal(t, "", e["E"].value)
	assert.True(t, e["F"].unresolved)
	assert.Equal(t, "-----BEGIN KEY-----\nabc\n-----END KEY-----", e["G"].value, "single quotes on several lines")
	assert.Equal(t, 12, e["H"].line)

	e = env{}
	err := e.parseDotenv("f", "A=1\nnot a line\nB='open\n")
	assert.EqualError(t, err, "f:2: invalid line\nf:3: unterminated quote")
	assert.Equal(t, "1", e["A"].value, "an invalid line does not stop the file")

	for content, want := range map[string]string{
		"=x":    "f:1: invalid line",
		"A B=x": "f:1: invalid line",
		"A='x":  "f:1: unterminated quote",
		`A="x`:  "f:1: unterminated quote",
	} {
		assert.EqualError(t, env{}.parseDotenv("f", content), want, content)
	}
}

func TestAllowed(t *testing.T) {
	cfg := config.DefaultConfig().Collectors.Envfiles
	for key, want := range map[string]bool{
		"DATABASE_URL": true, "REDIS_DSN": true, "DB_HOST": true, "APP_ENV": true,
		"APP_SECRET": false, "DB_PASSWORD": false, "JWT_TOKEN_URL": false, "API_KEY_URL": false, "FOO": false,
	} {
		assert.Equal(t, want, allowed(key, cfg), key)
	}
}

func TestResolverCache(t *testing.T) {
	ctx, k := newContext(t, nil)
	k.EXPECT().LookupHost(gomock.Any(), "db").Return([]string{"10.0.0.1"}, nil).Times(1)
	r := newResolver(ctx, t.Context())
	for range 3 {
		d, _ := dsn.Parse("postgresql://db/x")
		assert.Equal(t, "10.0.0.1", r.target(d).Resolved[0])
	}
}

const applicationINI = `[production]
phpSettings.display_errors = 0
resources.db.adapter = "PDO_MYSQL"
resources.db.params.host = "db.internal"
resources.db.params.username = "site"
resources.db.params.password = "secret"
resources.db.params.dbname = "site_prod_"
resources.cachemanager.page.backend.options.servers.host = 10.0.1.70 ; memcached
resources.cachemanager.page.backend.options.servers.port = 11211
resources.mail.transport.host = "smtp.internal:2525"
resources.log.stream.url = "file:///var/log/site.log"
resources.search.url = "http://10.0.1.80:9200/site"
resources.broken.server = "a,b"
resources.local.params.adapter = "pdo_sqlite"
resources.local.params.host = "localhost"

[staging : production]
resources.db.params.host = "db-staging.internal"
resources.db.params.dbname = "site_staging_"

[pp : staging]
resources.db.params.dbname = "site_pp_"
`

func TestCollect_Zend1(t *testing.T) {
	ctx, k := newContext(t, map[string]string{
		HostsFile: "10.0.1.20 db.internal\n10.0.1.21 db-staging.internal\n10.0.1.90 smtp.internal\n",
		"/srv/site/production/www/application/configs/application.ini": applicationINI,
		"/srv/site/staging/www/application/configs/application.ini":    applicationINI,
		"/srv/site/pp/www/application/configs/application.ini":         applicationINI,
		"/srv/site/pp/www/public/.htaccess":                            "RewriteEngine On\nSetEnv APPLICATION_ENV \"pp\"\n",
		"/srv/site/missing/www/composer.json":                          "{}",
	})
	k.EXPECT().LookupHost(gomock.Any(), gomock.Any()).Times(0)
	zend1 := &snapshot.Framework{Name: "zend1"}
	apps, err := collect(t, ctx,
		snapshot.App{Path: "/srv/site/production/www", Env: "production", Framework: zend1},
		snapshot.App{Path: "/srv/site/staging/www", Env: "staging", Framework: zend1},
		snapshot.App{Path: "/srv/site/pp/www", Env: "dev", Framework: zend1},
		snapshot.App{Path: "/srv/site/missing/www", Env: "dev", Framework: zend1},
	)
	require.NoError(t, err)
	file := "/srv/site/production/www/application/configs/application.ini"
	assert.Equal(t, []snapshot.Dependency{
		{Key: "resources.broken", Source: snapshot.DependencySource{File: file, Line: 13, KeyPath: "resources.broken"}, Unparsed: true},
		{Key: "resources.cachemanager.page.backend.options.servers", Source: snapshot.DependencySource{File: file, Line: 8, KeyPath: "resources.cachemanager.page.backend.options.servers"},
			Port: 11211, Target: &snapshot.Target{Kind: "ip", Value: "10.0.1.70"}},
		{Key: "resources.db.params", Source: snapshot.DependencySource{File: file, Line: 4, KeyPath: "resources.db.params"},
			Scheme: "mysql", Port: 3306, Path: "site_prod_", Target: &snapshot.Target{Kind: "name", Value: "db.internal", Resolved: []string{"10.0.1.20"}, Resolver: "hosts"}},
		{Key: "resources.mail.transport", Source: snapshot.DependencySource{File: file, Line: 10, KeyPath: "resources.mail.transport"},
			Port: 2525, Target: &snapshot.Target{Kind: "name", Value: "smtp.internal", Resolved: []string{"10.0.1.90"}, Resolver: "hosts"}},
		{Key: "resources.search", Source: snapshot.DependencySource{File: file, Line: 12, KeyPath: "resources.search"},
			Scheme: "http", Port: 9200, Path: "site", Target: &snapshot.Target{Kind: "ip", Value: "10.0.1.80"}},
	}, apps[0].Dependencies, "file:// and sqlite left out, credentials never read")

	db := func(app snapshot.App) snapshot.Dependency {
		i := slices.IndexFunc(app.Dependencies, func(d snapshot.Dependency) bool { return d.Key == "resources.db.params" })
		require.GreaterOrEqual(t, i, 0)
		return app.Dependencies[i]
	}
	assert.Equal(t, "db-staging.internal", db(apps[1]).Target.Value, "staging extends production")
	assert.Equal(t, 18, db(apps[1]).Source.Line)
	assert.Equal(t, "site_pp_", db(apps[2]).Path, "the .htaccess env wins")
	assert.Equal(t, "db-staging.internal", db(apps[2]).Target.Value)
	assert.Empty(t, apps[3].Dependencies, "no application.ini")
}

func TestReadINI_Fallbacks(t *testing.T) {
	f := fakefs.New()
	f.Write(t, "/a.ini", "[first]\nx.host = a\n[loop : loop]\ny.host = b\n")
	r := &reader{fs: f}
	sections, order, err := readINI(f, "/a.ini")
	require.NoError(t, err)
	assert.Equal(t, []string{"first", "loop"}, order)
	assert.Len(t, chain(sections, "loop"), 1, "a section extending itself stops")
	f.Write(t, "/app/"+Zend1Config, "[first]\nx.host = a\n")
	b, err := r.zend1Blocks("/app", "unknown")
	require.NoError(t, err)
	require.Len(t, b, 1, "without the env nor production: the first section")
	_, _, err = readINI(f, "/missing.ini")
	assert.Error(t, err)
}

func TestResolve_Late(t *testing.T) {
	e := env{}
	require.NoError(t, e.parseDotenv(".env", "MERCURE_PUBLIC_URL=https://mercure.${BASE_DOMAIN}/.well-known/mercure\nLOOP=${LOOP}x\nA=${B}\nB=${C}\nC=c\n"))
	require.NoError(t, e.parseDotenv(".env.local", "BASE_DOMAIN=example.fr\n"))
	e.resolve()
	assert.Equal(t, "https://mercure.example.fr/.well-known/mercure", e["MERCURE_PUBLIC_URL"].value, "defined by a later file")
	assert.False(t, e["MERCURE_PUBLIC_URL"].unresolved)
	assert.Equal(t, "c", e["A"].value, "chained references")
	assert.True(t, e["LOOP"].unresolved)
}

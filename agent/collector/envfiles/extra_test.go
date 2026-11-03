package envfiles

import (
	"testing"

	"github.com/alexandreh2ag/lacis/agent/config"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollect_ExtraConfig(t *testing.T) {
	ctx, _ := newContext(t, map[string]string{
		"/srv/shop/releases/3/.env":       "DATABASE_URL=mysql://u:p@10.0.0.1:3306/shop\nREDIS_URL=redis://10.0.0.2:6379\n",
		"/srv/shop/releases/3/conf/a.env": "QUEUE_URL=amqp://10.0.0.3:5672\n",
		"/srv/shop/releases/3/conf/b.env": "QUEUE_URL=amqp://10.0.0.4:5672\n",
		"/etc/acme/shop.env":              "DATABASE_URL=mysql://u:p@10.0.0.9:3306/shop\n",
		"/etc/acme/queue.yaml":            "queue:\n  url: amqp://10.0.0.6:5672\n",
		"/srv/blog/.env":                  "DATABASE_URL=mysql://u:p@10.0.0.5:3306/blog\n",
		"/etc/supervisor/conf.d/w.conf":   "[program:consumer]\ncommand=php bin/console messenger:consume\nenvironment=REDIS_URL=\"redis://10.0.0.8:6379\"\n",
	})
	ctx.Config.Collectors.Apps.ExtraConfig = []config.ExtraConfig{
		// Matched by the resolved path; the launcher environment still wins.
		{App: "/srv/shop/releases/*", Config: []config.FrameworkSource{
			{Path: "/etc/acme/shop.env", Format: "env"},
			{Path: "conf/*.env", Format: "env"},
			{Path: "/etc/acme/missing.env", Format: "env"},
			{Path: "/etc/acme/queue.yaml", Format: "yaml"}, // flattened to QUEUE_URL, read last
		}},
		{App: "/srv/other", Config: []config.FrameworkSource{{Path: "/etc/acme/shop.env", Format: "env"}}},
	}
	apps, err := collect(t, ctx,
		snapshot.App{Path: "/srv/shop/current", ResolvedPath: "/srv/shop/releases/3", Launchers: []snapshot.Launcher{{Name: "consumer"}}},
		snapshot.App{Path: "/srv/blog"},
	)
	require.NoError(t, err)
	assert.Equal(t, []snapshot.Dependency{
		{Key: "DATABASE_URL", Source: snapshot.DependencySource{File: "/etc/acme/shop.env", Line: 1}, Scheme: "mysql", Port: 3306, Path: "shop",
			Target: &snapshot.Target{Kind: "ip", Value: "10.0.0.9"}},
		{Key: "QUEUE_URL", Source: snapshot.DependencySource{File: "/etc/acme/queue.yaml", Line: 2, KeyPath: "queue.url"}, Scheme: "amqp", Port: 5672,
			Target: &snapshot.Target{Kind: "ip", Value: "10.0.0.6"}},
		{Key: "REDIS_URL", Source: snapshot.DependencySource{File: "/etc/supervisor/conf.d/w.conf"}, Scheme: "redis", Port: 6379,
			Target: &snapshot.Target{Kind: "ip", Value: "10.0.0.8"}},
	}, apps[0].Dependencies)
	require.Len(t, apps[1].Dependencies, 1)
	assert.Equal(t, "10.0.0.5", apps[1].Dependencies[0].Target.Value)
}

func TestMatchApp(t *testing.T) {
	app := &snapshot.App{Path: "/var/www/shop/current", ResolvedPath: "/var/www/shop/releases/42"}
	assert.True(t, matchApp("/var/www/*/current", app))
	assert.True(t, matchApp("/var/www/shop/releases/*", app))
	assert.False(t, matchApp("/var/www/*", app), "a glob star never crosses a slash")
	assert.False(t, matchApp("/srv/*", &snapshot.App{Path: "/srv/a/b"}))
	assert.False(t, matchApp("", &snapshot.App{Path: "/srv/a"}))
}

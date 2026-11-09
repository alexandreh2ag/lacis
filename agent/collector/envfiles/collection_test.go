package envfiles

import (
	"testing"

	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollect_Collections(t *testing.T) {
	src := func(line int, keyPath string) snapshot.DependencySource {
		return snapshot.DependencySource{File: "/srv/app/.env", Line: line, KeyPath: keyPath}
	}
	ip := func(v string) *snapshot.Target { return &snapshot.Target{Kind: "ip", Value: v} }
	tests := []struct {
		name string
		env  string
		want []snapshot.Dependency
	}{
		{
			name: "JSON array of objects",
			env:  `ELASTIC_SERVERS='[{"host":"172.16.1.12","port":9200},{"host":"172.16.1.13","port":9200}]'`,
			want: []snapshot.Dependency{
				{Key: "ELASTIC_SERVERS", Source: src(1, "ELASTIC_SERVERS.0"), Port: 9200, Target: ip("172.16.1.12")},
				{Key: "ELASTIC_SERVERS", Source: src(1, "ELASTIC_SERVERS.1"), Port: 9200, Target: ip("172.16.1.13")},
			},
		},
		{
			name: "JSON array of strings, double quoted",
			env:  `ELASTICSEARCH_SERVERS="[\"172.16.1.15:9200\",\"redis://:s3cr3t@172.16.1.16:6379/1\"]"`,
			want: []snapshot.Dependency{
				{Key: "ELASTICSEARCH_SERVERS", Source: src(1, "ELASTICSEARCH_SERVERS.0"), Port: 9200, Target: ip("172.16.1.15")},
				{Key: "ELASTICSEARCH_SERVERS", Source: src(1, "ELASTICSEARCH_SERVERS.1"), Scheme: "redis", Port: 6379, Path: "1", Target: ip("172.16.1.16")},
			},
		},
		{
			name: "JSON object, its secrets never read",
			env:  `DB_SERVERS={"adapter":"pdo_mysql","host":"10.0.0.5","dbname":"shop","password":"10.9.9.9"}`,
			want: []snapshot.Dependency{
				{Key: "DB_SERVERS", Source: src(1, "DB_SERVERS"), Scheme: "mysql", Port: 3306, Path: "shop", Target: ip("10.0.0.5")},
			},
		},
		{
			name: "YAML flow",
			env:  `CACHE_SERVERS='[{host: 10.0.0.7, port: 11211}, {url: "memcached://10.0.0.8:11212"}]'`,
			want: []snapshot.Dependency{
				{Key: "CACHE_SERVERS", Source: src(1, "CACHE_SERVERS.0"), Port: 11211, Target: ip("10.0.0.7")},
				{Key: "CACHE_SERVERS", Source: src(1, "CACHE_SERVERS.1"), Scheme: "memcached", Port: 11212, Target: ip("10.0.0.8")},
			},
		},
		{
			name: "YAML block on several lines",
			env:  "A=1\nQUEUE_SERVERS='- 10.0.0.9:5672\n- es1.internal\n- amqp://10.0.0.10'",
			want: []snapshot.Dependency{
				{Key: "QUEUE_SERVERS", Source: src(2, "QUEUE_SERVERS.0"), Port: 5672, Target: ip("10.0.0.9")},
				{Key: "QUEUE_SERVERS", Source: src(2, "QUEUE_SERVERS.1"),
					Target: &snapshot.Target{Kind: "name", Value: "es1.internal", Resolved: []string{"10.0.0.11"}, Resolver: "hosts"}},
				{Key: "QUEUE_SERVERS", Source: src(2, "QUEUE_SERVERS.2"), Scheme: "amqp", Port: 5672, Target: ip("10.0.0.10")},
			},
		},
		{
			name: "bare words neither sent nor resolved",
			env:  `OPAQUE_SERVERS='["a1b2c3d4", "10.0.0.12:80", "sync://"]'`,
			want: []snapshot.Dependency{
				{Key: "OPAQUE_SERVERS", Source: src(1, "OPAQUE_SERVERS.0"), Unparsed: true},
				{Key: "OPAQUE_SERVERS", Source: src(1, "OPAQUE_SERVERS.1"), Port: 80, Target: ip("10.0.0.12")},
			},
		},
		{
			name: "references resolved before decoding",
			env:  "IP=10.0.0.13\nWEB_SERVERS=\"[\\\"${IP}:8080\\\"]\"",
			want: []snapshot.Dependency{
				{Key: "WEB_SERVERS", Source: src(2, "WEB_SERVERS.0"), Port: 8080, Target: ip("10.0.0.13")},
			},
		},
		{
			name: "nothing usable is unparsed",
			env:  `MISC_SERVERS='[{"name":"x"}, [1, 2]]'`,
			want: []snapshot.Dependency{{Key: "MISC_SERVERS", Source: src(1, ""), Unparsed: true}},
		},
		{
			name: "invalid JSON falls back to the value",
			env:  `BROKEN_SERVERS='[{"host":"10.0.0.1"'`,
			want: []snapshot.Dependency{{Key: "BROKEN_SERVERS", Source: src(1, ""), Unparsed: true}},
		},
		{
			name: "a scalar is not a collection",
			env:  "PLAIN_SERVERS=10.0.0.14:9200",
			want: []snapshot.Dependency{{Key: "PLAIN_SERVERS", Source: src(1, ""), Port: 9200, Target: ip("10.0.0.14")}},
		},
		{
			name: "only the not network items: none",
			env:  `NULL_SERVERS='["sync://", "null://null"]'`,
			want: []snapshot.Dependency{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := newContext(t, map[string]string{
				HostsFile:       "10.0.0.11 es1.internal\n",
				"/srv/app/.env": tt.env + "\n",
			})
			ctx.Config.Collectors.Envfiles.AllowKeys = append(ctx.Config.Collectors.Envfiles.AllowKeys, "*_SERVERS")
			apps, err := collect(t, ctx, snapshot.App{Path: "/srv/app"})
			require.NoError(t, err)
			assert.Equal(t, tt.want, apps[0].Dependencies)
		})
	}
}

func TestCollect_CollectionDeniedKey(t *testing.T) {
	ctx, _ := newContext(t, map[string]string{
		"/srv/app/.env": `SECRET_SERVERS='["10.0.0.1:80"]'` + "\n",
	})
	ctx.Config.Collectors.Envfiles.AllowKeys = append(ctx.Config.Collectors.Envfiles.AllowKeys, "*_SERVERS")
	apps, err := collect(t, ctx, snapshot.App{Path: "/srv/app"})
	require.NoError(t, err)
	assert.Equal(t, []snapshot.Dependency{}, apps[0].Dependencies, "the key is denied before its value is decoded")
}

func TestLooksLikeCollection(t *testing.T) {
	for raw, want := range map[string]bool{
		`[1]`: true, ` {"a":1}`: true, "- a\n- b": true, "a: b\nc: d": true,
		"redis://db:6379": false, "localhost:9200": false, "a: b": false, "-1": false,
	} {
		assert.Equal(t, want, looksLikeCollection(raw), raw)
	}
}

func TestNetworkShaped(t *testing.T) {
	for raw, want := range map[string]bool{
		"redis://db": true, "mysql:host=db": true, "host=db port=5432": true, "/run/redis.sock": true,
		"10.0.0.1": true, "[::1]:80": true, "db:3306": true, "db.internal": true,
		"db": false, "a1b2c3d4": false, "": false, "a b": false,
	} {
		assert.Equal(t, want, networkShaped(raw), raw)
	}
}

func TestParseCollection_AliasBomb(t *testing.T) {
	bomb := "[&a [x, x, x, x, x, x, x, x, x, x], &b [*a, *a, *a, *a, *a, *a, *a, *a, *a, *a], &c [*b, *b, *b, *b, *b, *b, *b, *b, *b, *b], [*c, *c, *c, *c, *c, *c, *c, *c, *c, *c], [*c, *c, *c, *c, *c, *c, *c, *c, *c, *c]]"
	items, ok := parseCollection(value{value: bomb})
	require.True(t, ok)
	n := 0
	for _, it := range items {
		n += len(it.values)
	}
	assert.LessOrEqual(t, n, maxLeaves)
}

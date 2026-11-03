package envfiles

import (
	"fmt"
	"strings"
	"testing"

	"github.com/alexandreh2ag/lacis/agent/config"
	"github.com/alexandreh2ag/lacis/agent/fakefs"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// raws returns the raw values of leaves, a reference prefixed with "~".
func raws(values map[string]value) map[string]string {
	out := map[string]string{}
	for k, v := range values {
		if v.interp {
			out[k] = "~" + v.raw
		} else {
			out[k] = v.value
		}
	}
	return out
}

func TestParsePHPConfig(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    map[string]string
	}{
		{
			name: "laminas db and literals",
			content: `<?php
declare(strict_types=1);

use Laminas\Db\Adapter\AdapterInterface;

// a comment with return [ 'x' => 1 ]
/* return ['y' => 2]; */
# return ['z' => 3];
return [
    'db' => [
        'driver'   => 'Pdo_Mysql',
        "hostname" => "db.internal",
        'port'     => 3306,
        'ratio'    => -1.5,
        'enabled'  => TRUE,
        'off'      => false,
        'none'     => null,
        'escaped'  => 'it\'s \\ \n',
        "dq"       => "a\tb\"c\$d",
    ],
    'list' => array('a', "b", 7 => 'c', 'd'),
];`,
			want: map[string]string{
				"db.driver": "Pdo_Mysql", "db.hostname": "db.internal", "db.port": "3306", "db.ratio": "-1.5",
				"db.enabled": "true", "db.off": "false", "db.none": "", "db.escaped": `it's \ \n`, "db.dq": "a\tb\"c$d",
				"list.0": "a", "list.1": "b", "list.7": "c", "list.8": "d",
			},
		},
		{
			name: "environment references",
			content: `<?php return [
    'a' => getenv('DB_HOST'),
    'b' => \getenv("DB_HOST"),
    'c' => $_ENV['REDIS_URL'],
    'd' => $_SERVER["REDIS_URL"],
    'e' => env('DB_PORT', 3306),
    'f' => env('DB_HOST'),
    'g' => env('X', null),
    'h' => getenv('DB_HOST') ?: 'localhost',
    'i' => $_ENV['DB_HOST'] ?? 'localhost',
    'j' => 'mysql://' . getenv('DB_HOST') . ':3306',
    'k' => (int) getenv('DB_PORT'),
    'l' => 'lit' ?? 'other',
    'm' => '%env(resolve:DATABASE_URL)%',
    'n' => ('a' . 'b'),
];`,
			want: map[string]string{
				"a": "~${DB_HOST}", "b": "~${DB_HOST}", "c": "~${REDIS_URL}", "d": "~${REDIS_URL}", "e": "~${DB_PORT:-3306}",
				"f": "~${DB_HOST}", "g": "~${X:-}", "h": "~${DB_HOST:-localhost}", "i": "~${DB_HOST:-localhost}",
				"j": "~mysql://${DB_HOST}:3306", "k": "~${DB_PORT}", "l": "lit", "m": "~${DATABASE_URL}", "n": "ab",
			},
		},
		{
			name: "unparsed values are skipped",
			content: `<?php
$base = ['x' => 1];
function f() { return ['nope' => 1]; }
return [
    'const'    => PDO::ATTR_ERRMODE,
    PDO::MYSQL_ATTR_INIT_COMMAND => 'SET NAMES utf8',
    Foo::class => ['factory' => 'Bar'],
    'dir'      => __DIR__ . '/../data',
    'var'      => $base['x'],
    'call'     => sprintf('%s:%d', 'a', 1),
    'closure'  => function ($c) { return new Foo(['a' => 1]); },
    'arrow'    => fn() => 1,
    'interp'   => "host-$name",
    'brace'    => "host-{$name}",
    'ternary'  => getenv('A') ? 'x' : 'y',
    'math'     => 1 + 2,
    'bad'      => getenv($name),
    'baddef'   => env('A', getenv('B')),
    'heredoc'  => <<<EOT
    a, b ] ) ;
    EOT,
    'nowdoc'   => <<<'EOT'
    x
    EOT,
    'mixed'    => '$x' . getenv('A'),
    'num'      => 12abc,
    'after'    => 'kept',
];`,
			want: map[string]string{"after": "kept"},
		},
		{
			name:    "not an array",
			content: `<?php $config = ['a' => 'b']; return $config;`,
			want:    map[string]string{},
		},
		{
			name:    "no return",
			content: `<?php $a = ['a' => 'b'];`,
			want:    map[string]string{},
		},
		{
			name:    "a variable named return is not the statement",
			content: `<?php $return = 1; return ['a' => 'b'];`,
			want:    map[string]string{"a": "b"},
		},
		{
			name:    "truncated",
			content: `<?php return ['a' => 'b', 'c' => ['d' => 'e'`,
			want:    map[string]string{},
		},
		{
			name:    "malformed array",
			content: `<?php return ['a' => 'b' 'c', 'x' => 'y', 'k' 'v' => 'w'];`,
			want:    map[string]string{"x": "y"},
		},
		{
			name:    "unterminated string",
			content: `<?php return ['a' => 'b`,
			want:    map[string]string{},
		},
		{
			name:    "too deep",
			content: "<?php return " + strings.Repeat("[", maxPHPDepth+2) + strings.Repeat("]", maxPHPDepth+2) + ";",
			want:    map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, raws(parsePHPConfig("/c.php", tt.content)))
		})
	}
}

func TestParsePHPConfig_Lines(t *testing.T) {
	got := parsePHPConfig("/c.php", "<?php\nreturn [\n  'a' => 'x',\n\n  'b' => [\n    'c' => 1,\n  ],\n];\n")
	assert.Equal(t, 3, got["a"].line)
	assert.Equal(t, 6, got["b.c"].line)
	assert.Equal(t, "/c.php", got["b.c"].file)
}

func TestParseSource(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		content string
		want    map[string]string
		wantErr string
	}{
		{
			name:   "yaml",
			format: "yaml",
			content: `doctrine:
  dbal:
    url: '%env(resolve:DATABASE_URL)%'
    port: 5432
    nothing: ~
    const: !php/const PDO::ATTR_ERRMODE
defaults: &defaults
  host: cache.internal
cache:
  <<: *defaults
  servers: [10.0.0.1, 10.0.0.2]
  alias: *defaults
mixed: 'http://%env(HOST)%:%env(int:PORT)%/x'
transformed: '%env(base64:SECRET)%'
json: '%env(json:file:CFG)%'
fallback: '%env(default::REDIS_URL)%'
dollar: '$x %env(A)%'
plain: '$literal'
---
second: doc
`,
			want: map[string]string{
				"doctrine.dbal.url": "~${DATABASE_URL}", "doctrine.dbal.port": "5432", "defaults.host": "cache.internal",
				"cache.host": "cache.internal", "cache.servers.0": "10.0.0.1", "cache.servers.1": "10.0.0.2",
				"cache.alias.host": "cache.internal", "mixed": "~http://${HOST}:${PORT}/x", "fallback": "~${REDIS_URL}",
				"plain": "$literal", "second": "doc",
			},
		},
		{name: "invalid yaml", format: "yaml", content: "a: [", want: map[string]string{}, wantErr: "/c"},
		{
			name:    "json",
			format:  "json",
			content: `{"database": {"host": "10.0.0.1", "port": 5432, "ssl": true, "none": null, "replicas": ["10.0.0.2"]}, "url": "%env(API_URL)%"}`,
			want: map[string]string{
				"database.host": "10.0.0.1", "database.port": "5432", "database.ssl": "true", "database.replicas.0": "10.0.0.2",
				"url": "~${API_URL}",
			},
		},
		{name: "invalid json", format: "json", content: "{", want: map[string]string{}, wantErr: "/c"},
		{
			name:   "ini",
			format: "ini",
			content: `top = 1
[database]
host = "10.0.0.1" ; primary
port = 3306
[cache : database]
servers[main] = 10.0.0.2
redis.url = '%env(REDIS_URL)%'
[] = ignored
`,
			want: map[string]string{
				"top": "1", "database.host": "10.0.0.1", "database.port": "3306", "cache.servers.main": "10.0.0.2",
				"cache.redis.url": "~${REDIS_URL}",
			},
		},
		{name: "php", format: "php", content: "<?php return ['a' => 'b'];", want: map[string]string{"a": "b"}},
		{name: "unknown", format: "toml", content: "", wantErr: "unknown format"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSource(tt.format, "/c", []byte(tt.content))
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			if tt.want != nil {
				assert.Equal(t, tt.want, raws(got))
			}
		})
	}
}

func TestParseYAMLConfig_AliasBomb(t *testing.T) {
	var b strings.Builder
	b.WriteString("a0: &a0 [x, x, x, x, x, x, x, x, x, x]\n")
	for i := 1; i < 8; i++ {
		fmt.Fprintf(&b, "a%d: &a%d [*a%d, *a%d, *a%d, *a%d, *a%d, *a%d, *a%d, *a%d, *a%d, *a%d]\n", i, i, i-1, i-1, i-1, i-1, i-1, i-1, i-1, i-1, i-1, i-1)
	}
	got, err := parseYAMLConfig("/bomb.yaml", []byte(b.String()))
	assert.ErrorIs(t, err, errTooManyLeaves)
	assert.LessOrEqual(t, len(got), maxLeaves)
}

func TestSymfonyEnvName(t *testing.T) {
	tests := map[string]struct {
		name string
		ok   bool
	}{
		"X":                     {"X", true},
		"resolve:X":             {"X", true},
		"int:trim:string:X":     {"X", true},
		"default::X":            {"X", true},
		"default:param:int:X":   {"X", true},
		"base64:X":              {"", false},
		"key:path:json:X":       {"", false},
		"resolve:not a name":    {"not a name", false},
		"":                      {"", false},
		"enum:App\\Status:X":    {"", false},
		"query_string:url:DSN":  {"", false},
		"require:CONFIG_PHP":    {"", false},
		"bool:not:FEATURE_FLAG": {"FEATURE_FLAG", true},
	}
	for inner, want := range tests {
		name, ok := symfonyEnvName(inner)
		assert.Equal(t, want.ok, ok, inner)
		if want.ok {
			assert.Equal(t, want.name, name, inner)
		}
	}
}

func TestCollect_DeclarativeSources(t *testing.T) {
	ctx, k := newContext(t, map[string]string{
		"/srv/app/.env":             "DATABASE_URL=postgresql://u:p@10.0.0.1:5432/app\nREDIS_HOST=10.0.0.9\n",
		"/srv/app/config/app.yaml":  "doctrine:\n  dbal:\n    url: '%env(resolve:DATABASE_URL)%'\n    password: '%env(DB_PASSWORD)%'\nsearch:\n  host: 10.0.0.2\n  port: 9200\nbroken:\n  url: '%env(MISSING_URL)%'\n",
		"/srv/app/config/db.php":    "<?php return ['cache' => ['redis_url' => 'redis://' . getenv('REDIS_HOST') . ':6380'], 'secret_url' => 'http://10.0.0.66'];",
		"/srv/app/config/app.ini":   "[mail]\nsmtp_host = 10.0.0.3\nsmtp_port = 2525\n",
		"/srv/app/config/svc.json":  `{"queue": {"dsn": "amqp://10.0.0.4"}}`,
		"/srv/app/config/bad.json":  `{`,
		"/srv/app/config/b.yaml":    "api:\n  url: http://10.0.0.5\n",
		"/srv/app/config/c.yaml":    "api:\n  url: http://10.0.0.6\n",
		"/srv/other/config/app.php": "<?php return ['x_url' => 'http://10.0.0.7'];",
	})
	k.EXPECT().LookupHost(gomock.Any(), gomock.Any()).Times(0)
	ctx.Config.Collectors.Apps.Frameworks = []config.FrameworkDetector{
		{Name: "custom", Config: []config.FrameworkSource{
			{Path: ".env", Format: "env"},
			{Path: "config/app.yaml", Format: "yaml"},
			{Path: "config/db.php", Format: "php"},
			{Path: "config/app.ini", Format: "ini"},
			{Path: "config/svc.json", Format: "json"},
			{Path: "config/bad.json", Format: "json"},
			{Path: "config/[bc].yaml", Format: "yaml"},
			{Path: "config/missing.yaml", Format: "yaml"},
		}},
		// A detector with structured sources alone still replaces the
		// built-in loader.
		{Name: "only-php", Config: []config.FrameworkSource{{Path: "config/app.php", Format: "php"}}},
	}
	apps, err := collect(t, ctx,
		snapshot.App{Path: "/srv/app", Framework: &snapshot.Framework{Name: "custom"}},
		snapshot.App{Path: "/srv/other", Framework: &snapshot.Framework{Name: "only-php"}},
	)
	assert.ErrorContains(t, err, "/srv/app/config/bad.json")
	got := map[string]string{}
	for _, d := range apps[0].Dependencies {
		if d.Unparsed {
			got[d.Key] = "unparsed " + d.Source.KeyPath
			continue
		}
		got[d.Key] = fmt.Sprintf("%s://%s:%d %s@%s:%d", d.Scheme, d.Target.Value, d.Port, d.Source.KeyPath, d.Source.File, d.Source.Line)
	}
	assert.Equal(t, map[string]string{
		"DATABASE_URL":      "postgresql://10.0.0.1:5432 @/srv/app/.env:1",
		"REDIS_HOST":        "://10.0.0.9:0 @/srv/app/.env:2",
		"DOCTRINE_DBAL_URL": "postgresql://10.0.0.1:5432 doctrine.dbal.url@/srv/app/config/app.yaml:3",
		"SEARCH_HOST":       "://10.0.0.2:9200 search.host@/srv/app/config/app.yaml:6",
		"BROKEN_URL":        "unparsed broken.url",
		"CACHE_REDIS_URL":   "redis://10.0.0.9:6380 cache.redis_url@/srv/app/config/db.php:1",
		"MAIL_SMTP_HOST":    "://10.0.0.3:2525 mail.smtp_host@/srv/app/config/app.ini:2",
		"QUEUE_DSN":         "amqp://10.0.0.4:5672 queue.dsn@/srv/app/config/svc.json:0",
		"API_URL":           "http://10.0.0.6:80 api.url@/srv/app/config/c.yaml:2",
	}, got, "flattened keys, filtered like env keys; the last file wins")
	require.Len(t, apps[1].Dependencies, 1)
	assert.Equal(t, "X_URL", apps[1].Dependencies[0].Key)
}

func TestCollect_Laminas(t *testing.T) {
	ctx, k := newContext(t, map[string]string{
		HostsFile:       "10.0.1.20 db.internal\n",
		"/srv/lam/.env": "DB_HOST=db.internal\n",
		"/srv/lam/config/autoload/global.php": `<?php
return [
    'db' => [
        'driver'   => 'Pdo_Mysql',
        'hostname' => getenv('DB_HOST'),
        'port'     => 3307,
        'database' => 'shop',
        'password' => 'secret',
    ],
    'caches' => ['redis' => ['adapter' => 'redis', 'options' => ['server' => ['host' => '10.0.1.30', 'port' => 6379]]]],
];`,
		"/srv/lam/config/autoload/doctrine.global.php": `<?php
return ['doctrine' => ['connection' => ['orm_default' => [
    'driverClass' => \Doctrine\DBAL\Driver\PDO\MySQL\Driver::class,
    'params' => ['host' => '10.0.1.40', 'port' => '3306', 'dbname' => 'old', 'url' => getenv('DOCTRINE_URL')],
]]]];`,
		"/srv/lam/config/autoload/local.php":          `<?php return ['db' => ['database' => 'shop_local']];`,
		"/srv/lam/config/autoload/doctrine.local.php": `<?php return ['doctrine' => ['connection' => ['orm_default' => ['params' => ['dbname' => 'new']]]]];`,
		"/srv/lam/config/autoload/z.local.php":        `<?php return ['api' => ['url' => 'http://10.0.1.50:8080/v1']];`,
		"/srv/lam/config/autoload/ignored.php":        `<?php return ['ignored' => ['host' => '10.0.1.99']];`,
		"/srv/zf/config/autoload/global.php":          `<?php return ['db' => ['dsn' => 'pgsql:host=10.0.2.1;port=5433;dbname=zf']];`,
	})
	k.EXPECT().LookupHost(gomock.Any(), gomock.Any()).Times(0)
	apps, err := collect(t, ctx,
		snapshot.App{Path: "/srv/lam", Framework: &snapshot.Framework{Name: "laminas"}},
		snapshot.App{Path: "/srv/zf", Framework: &snapshot.Framework{Name: "zend"}},
	)
	require.NoError(t, err)
	got := map[string]string{}
	for _, d := range apps[0].Dependencies {
		if d.Unparsed {
			got[d.Key] = "unparsed"
			continue
		}
		got[d.Key] = fmt.Sprintf("%s://%s:%d/%s %s:%d", d.Scheme, d.Target.Value, d.Port, d.Path, d.Source.File, d.Source.Line)
	}
	dir := "/srv/lam/config/autoload/"
	assert.Equal(t, map[string]string{
		"DB_HOST":                                "://db.internal:0/ /srv/lam/.env:1",
		"db":                                     "mysql://db.internal:3307/shop_local " + dir + "global.php:5",
		"caches.redis.options.server":            "://10.0.1.30:6379/ " + dir + "global.php:10",
		"doctrine.connection.orm_default.params": "unparsed",
		"api":                                    "http://10.0.1.50:8080/ " + dir + "z.local.php:1",
	}, got, "local over global, getenv resolved against the env, the unresolved url unparsed")
	require.Len(t, apps[1].Dependencies, 1)
	assert.Equal(t, "pgsql", apps[1].Dependencies[0].Scheme)
	assert.Equal(t, 5433, apps[1].Dependencies[0].Port)
	assert.Equal(t, "zf", apps[1].Dependencies[0].Path)
}

func TestCollect_LaminasLauncherEnv(t *testing.T) {
	// A getenv() reference is resolved once the env of the launchers is
	// merged: the environment of the process, such as an FPM pool.
	ctx, _ := newContext(t, map[string]string{
		"/srv/lam/config/autoload/global.php": `<?php return ['db' => ['hostname' => getenv('DB_HOST') ?: '10.0.1.1']];`,
	})
	apps, err := collect(t, ctx, snapshot.App{Path: "/srv/lam", Framework: &snapshot.Framework{Name: "laminas"}})
	require.NoError(t, err)
	require.Len(t, apps[0].Dependencies, 1)
	assert.Equal(t, "10.0.1.1", apps[0].Dependencies[0].Target.Value, "the default when unset")
}

func TestCollect_CapistranoLinks(t *testing.T) {
	ctx, k := newContext(t, map[string]string{
		"/srv/cap/shared/.env":       "REDIS_URL=redis://10.0.3.1:6379\n",
		"/srv/cap/shared/.env.local": "API_URL=http://10.0.3.2\n",
	})
	k.EXPECT().LookupHost(gomock.Any(), gomock.Any()).Times(0)
	f := ctx.Fs.(*fakefs.FS)
	f.Link("/srv/cap/releases/20261101/.env", "../../shared/.env")
	f.Link("/srv/cap/releases/20261101/.env.local", "/srv/cap/shared/.env.local")
	f.Link("/srv/cap/releases/20261101/.env.dev", ".env.dev")
	apps, err := collect(t, ctx, snapshot.App{Path: "/srv/cap/releases/20261101", Framework: &snapshot.Framework{Name: "symfony"}})
	assert.ErrorContains(t, err, "symbolic link loop")
	var keys []string
	for _, d := range apps[0].Dependencies {
		keys = append(keys, d.Key+"="+d.Target.Value)
	}
	assert.Equal(t, []string{"API_URL=10.0.3.2", "REDIS_URL=10.0.3.1"}, keys, "the shared files are read through their links")
}

// FuzzParsePHPConfig checks that the PHP parser never panics nor loops,
// whatever the file.
func FuzzParsePHPConfig(f *testing.F) {
	for _, seed := range []string{
		"<?php return ['a' => ['b' => getenv('X') ?: 'd', 'c' => array(1, 2)], 'e' => 'x' . env('Y', 'z')];",
		"<?php return [fn() => 1, 'a' => function () { return [1]; }, Foo::class => <<<EOT\n]\nEOT];",
		"<?php return [(int) $_ENV['A'], (('b')), \"x$y\", -0x1F, 1_000, 'q' => ",
		"<?php /* return */ # x\n return array(array(array(",
		"<?php return ['a' => getenv(, 'b' => env('X', getenv('Y')), $_SERVER[ => 1];",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, content string) {
		for key, v := range parsePHPConfig("/f.php", content) {
			if key == "" || v.line < 1 {
				t.Fatalf("key %q at line %d", key, v.line)
			}
		}
	})
}

package config

import (
	"reflect"
	"testing"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fullYAML = `
listen:
  address: 0.0.0.0:8443
  public_url: https://lacis.example.com
  base_path: /lacis
  tls:
    cert_file: /etc/lacis/tls/server.crt
    key_file: /etc/lacis/tls/server.key
database:
  dsn: postgres://lacis:change-me@db:5432/lacis?sslmode=verify-full
  max_open_conns: 10
  migrate_on_start: false
auth:
  session:
    secret: change-me-32-caracteres-minimum-xx
    ttl: 8h
  local:
    enabled: true
    bootstrap_admin_username: admin
    bootstrap_admin_password: change-me
  oidc:
    enabled: true
    issuer: https://sso.example.com/realms/infra
    client_id: lacis
    client_secret: change-me
    admin_groups: [lacis-admins]
agent_api:
  max_body_compressed: 2MB
  max_body_decompressed: 1GB
graphql:
  introspection: true
linking:
  cdn_ranges: [173.245.48.0/20]
graph:
  max_nodes: 300
defaults:
  stale_after: 3d
  inactive_after: 10d12h
workers:
  schedules:
    purge_inactive_hosts: "0 4 * * *"
  params:
    purge_inactive_hosts_after: 90d
log:
  level: debug
  format: json
`

func validConfig() *Config {
	cfg := DefaultConfig()
	cfg.Database.DSN = "postgres://lacis@db/lacis"
	cfg.Auth.Session.Secret = "0123456789abcdef0123456789abcdef"
	return cfg
}

func newValidator() *validator.Validate {
	return validator.New(validator.WithRequiredStructEnabled())
}

func TestFindFile(t *testing.T) {
	tests := []struct {
		name        string
		files       []string
		path        string
		want        string
		wantIgnored []string
		wantErr     bool
	}{
		{name: "yaml in default dir", files: []string{"/etc/lacis/server.yaml"}, want: "/etc/lacis/server.yaml"},
		{name: "yml in default dir", files: []string{"/etc/lacis/server.yml"}, want: "/etc/lacis/server.yml"},
		{
			name:        "yaml wins over yml",
			files:       []string{"/etc/lacis/server.yml", "/etc/lacis/server.yaml"},
			want:        "/etc/lacis/server.yaml",
			wantIgnored: []string{"/etc/lacis/server.yml"},
		},
		{name: "explicit file", files: []string{"/opt/custom.yml"}, path: "/opt/custom.yml", want: "/opt/custom.yml"},
		{name: "explicit dir", files: []string{"/opt/lacis/server.yml"}, path: "/opt/lacis", want: "/opt/lacis/server.yml"},
		{name: "no file", files: []string{"/etc/lacis/agent.yaml"}, want: ""},
		{name: "no dir", path: "/nowhere", want: ""},
		{name: "missing explicit file", path: "/opt/missing.yml", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			for _, f := range tt.files {
				require.NoError(t, afero.WriteFile(fs, f, []byte("{}"), 0o600))
			}
			got, ignored, err := FindFile(fs, tt.path, Name)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantIgnored, ignored)
		})
	}
}

func TestLoad_File(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/etc/lacis/server.yml", []byte(fullYAML), 0o600))

	cfg, err := Load(fs, "/etc/lacis/server.yml")
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0:8443", cfg.Listen.Address)
	assert.Equal(t, "/lacis/", cfg.Listen.Prefix())
	assert.True(t, cfg.Listen.TLS.Enabled())
	assert.Equal(t, 10, cfg.Database.MaxOpenConns)
	assert.False(t, cfg.Database.MigrateOnStart)
	assert.Equal(t, 8*time.Hour, cfg.Auth.Session.TTL)
	assert.Equal(t, []string{"lacis-admins"}, cfg.Auth.OIDC.AdminGroups)
	// Keys absent from the file keep their default.
	assert.Equal(t, []string{"openid", "profile"}, cfg.Auth.OIDC.Scopes)
	assert.Equal(t, "preferred_username", cfg.Auth.OIDC.UsernameClaim)
	assert.Equal(t, 2*MB, cfg.AgentAPI.MaxBodyCompressed)
	assert.Equal(t, GB, cfg.AgentAPI.MaxBodyDecompressed)
	assert.Equal(t, []int{1}, cfg.AgentAPI.AcceptedSchemaVersions)
	assert.Equal(t, IconsConfig{MaxSize: 256 * KB, MinSide: 16, MaxSide: 512}, cfg.Icons)
	assert.Equal(t, []string{"173.245.48.0/20"}, cfg.Linking.CDNRanges)
	assert.Equal(t, 300, cfg.Graph.MaxNodes)
	assert.Equal(t, 72*time.Hour, cfg.Defaults.StaleAfter)
	assert.Equal(t, 252*time.Hour, cfg.Defaults.InactiveAfter)
	assert.Equal(t, 30*day, cfg.Defaults.SnapshotRetention)
	assert.Equal(t, "0 4 * * *", cfg.Workers.Schedules["purge_inactive_hosts"])
	assert.Equal(t, "15 3 * * *", cfg.Workers.Schedules["purge_snapshots"])
	assert.Equal(t, 90*day, cfg.Workers.Params.PurgeInactiveHostsAfter)
	assert.Equal(t, LogConfig{Level: "debug", Format: "json"}, cfg.Log)
	assert.NoError(t, cfg.Validate(newValidator()))
}

func TestLoad_Environment(t *testing.T) {
	t.Setenv("LACIS_DATABASE_DSN", "postgres://env@db/lacis")
	t.Setenv("LACIS_AUTH_SESSION_SECRET", "env-secret-env-secret-env-secret!")
	t.Setenv("LACIS_LISTEN_BASE_PATH", "/infra/")
	t.Setenv("LACIS_AGENT_API_MAX_BODY_COMPRESSED", "1MB")
	t.Setenv("LACIS_DEFAULTS_STALE_AFTER", "2d")
	t.Setenv("LACIS_WORKERS_SCHEDULES_PURGE_SNAPSHOTS", "0 2 * * *")
	t.Setenv("LACIS_AUTH_OIDC_ADMIN_GROUPS", "admins,ops")

	cfg, err := Load(afero.NewMemMapFs(), "")
	require.NoError(t, err)

	assert.Equal(t, "postgres://env@db/lacis", cfg.Database.DSN)
	assert.Equal(t, "env-secret-env-secret-env-secret!", cfg.Auth.Session.Secret)
	assert.Equal(t, "/infra/", cfg.Listen.Prefix())
	assert.Equal(t, MB, cfg.AgentAPI.MaxBodyCompressed)
	assert.Equal(t, 2*day, cfg.Defaults.StaleAfter)
	assert.Equal(t, "0 2 * * *", cfg.Workers.Schedules["purge_snapshots"])
	assert.Equal(t, "45 3 * * *", cfg.Workers.Schedules["purge_expired_tokens"])
	assert.Equal(t, []string{"admins", "ops"}, cfg.Auth.OIDC.AdminGroups)
	assert.NoError(t, cfg.Validate(newValidator()))
}

func TestLoad_Errors(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/bad.yaml", []byte("listen: [unclosed"), 0o600))
	require.NoError(t, afero.WriteFile(fs, "/bad-size.yaml", []byte("agent_api: {max_body_compressed: 5 parsecs}"), 0o600))
	require.NoError(t, afero.WriteFile(fs, "/bad-duration.yaml", []byte("defaults: {stale_after: soon}"), 0o600))

	_, err := Load(fs, "/missing.yaml")
	assert.ErrorContains(t, err, "read config")
	_, err = Load(fs, "/bad.yaml")
	assert.ErrorContains(t, err, "read config")
	_, err = Load(fs, "/bad-size.yaml")
	assert.ErrorIs(t, err, ErrInvalidSize)
	_, err = Load(fs, "/bad-duration.yaml")
	assert.ErrorIs(t, err, ErrInvalidDuration)
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(c *Config)
		want   []string
	}{
		{name: "valid", mutate: func(*Config) {}},
		{name: "missing dsn", mutate: func(c *Config) { c.Database.DSN = "" }, want: []string{"Config.Database.DSN", `"required"`}},
		{name: "short secret", mutate: func(c *Config) { c.Auth.Session.Secret = "short" }, want: []string{"Config.Auth.Session.Secret", `"min"`}},
		{name: "relative base path", mutate: func(c *Config) { c.Listen.BasePath = "lacis" }, want: []string{"Config.Listen.BasePath"}},
		{name: "short bootstrap password", mutate: func(c *Config) {
			c.Auth.Local.BootstrapAdminUsername, c.Auth.Local.BootstrapAdminPassword = "admin", "admin"
		}, want: []string{"Config.Auth.Local.BootstrapAdminPassword", `"min"`}},
		{name: "half tls", mutate: func(c *Config) { c.Listen.TLS.CertFile = "/c.crt" }, want: []string{"Config.Listen.TLS.KeyFile", `"required_with"`}},
		{name: "oidc without issuer", mutate: func(c *Config) { c.Auth.OIDC.Enabled = true }, want: []string{"Config.Auth.OIDC.Issuer", "Config.Auth.OIDC.ClientID",
			"Config.Listen.PublicURL: required when auth.oidc is enabled"}},
		{name: "oidc", mutate: func(c *Config) {
			c.Listen.PublicURL = "https://lacis.acme.example"
			c.Auth.OIDC = OIDCConfig{Enabled: true, Issuer: "https://sso.acme.example/realms/acme", ClientID: "lacis", ClientSecret: "s", UsernameClaim: "sub"}
		}},
		{name: "oidc without username claim", mutate: func(c *Config) {
			c.Listen.PublicURL = "https://lacis.acme.example"
			c.Auth.OIDC = OIDCConfig{Enabled: true, Issuer: "https://sso.acme.example/realms/acme", ClientID: "lacis", ClientSecret: "s"}
		}, want: []string{"Config.Auth.OIDC.UsernameClaim"}},
		{name: "unknown task", mutate: func(c *Config) { c.Workers.Schedules["purge_everything"] = "@daily" }, want: []string{"Config.Workers.Schedules.purge_everything: unknown task"}},
		{name: "invalid cron", mutate: func(c *Config) { c.Workers.Schedules["purge_snapshots"] = "every day" }, want: []string{"Config.Workers.Schedules.purge_snapshots: invalid cron"}},
		{name: "task disabled and descriptor", mutate: func(c *Config) {
			c.Workers.Schedules["purge_snapshots"] = ""
			c.Workers.Schedules["purge_inactive_hosts"] = "@weekly"
		}},
		{name: "icon sides swapped", mutate: func(c *Config) { c.Icons.MaxSide = 8 }, want: []string{"Config.Icons.MaxSide", `"gtefield"`}},
		{name: "icon weight too small", mutate: func(c *Config) { c.Icons.MaxSize = 100 }, want: []string{"Config.Icons.MaxSize", `"min"`}},
		{name: "decompressed smaller", mutate: func(c *Config) { c.AgentAPI.MaxBodyDecompressed = KB }, want: []string{"Config.AgentAPI.MaxBodyDecompressed"}},
		{name: "inactive before stale", mutate: func(c *Config) { c.Defaults.InactiveAfter = day }, want: []string{"Config.Defaults.InactiveAfter", `"gtfield"`}},
		{name: "invalid cdn range", mutate: func(c *Config) { c.Linking.CDNRanges = []string{"cloudflare"} }, want: []string{"Config.Linking.CDNRanges[0]", `"cidr"`}},
		{name: "negative history", mutate: func(c *Config) { c.History.MaxEvents = -1 }, want: []string{"Config.History.MaxEvents", `"min"`}},
		{name: "invalid log format", mutate: func(c *Config) { c.Log.Format = "xml" }, want: []string{"Config.Log.Format"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)
			err := cfg.Validate(newValidator())
			if len(tt.want) == 0 {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, w := range tt.want {
				assert.Contains(t, err.Error(), w)
			}
		})
	}
}

func TestConfig_ValidateInvalidValidation(t *testing.T) {
	var cfg *Config
	assert.Error(t, cfg.Validate(newValidator()))
}

func TestListenConfig_Prefix(t *testing.T) {
	for in, want := range map[string]string{"/": "/", "": "/", "/lacis": "/lacis/", "/lacis/": "/lacis/", "/a/b": "/a/b/"} {
		assert.Equal(t, want, ListenConfig{BasePath: in}.Prefix(), in)
	}
}

func TestParseByteSize(t *testing.T) {
	tests := map[string]ByteSize{
		"512": 512, "512B": 512, "64KB": 64 * KB, "64kb": 64 * KB, "5MB": 5 * MB, "5 MB": 5 * MB,
		"5MiB": 5 * MB, "5M": 5 * MB, "1GB": GB, "2Gi": 2 * GB,
	}
	for in, want := range tests {
		got, err := ParseByteSize(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"", "MB", "-1MB", "5TB", "1.5MB", "99999999999999999999"} {
		_, err := ParseByteSize(in)
		assert.ErrorIs(t, err, ErrInvalidSize, in)
	}
}

func TestParseDuration(t *testing.T) {
	tests := map[string]time.Duration{
		"7d": 7 * day, "1d12h": 36 * time.Hour, "30m": 30 * time.Minute, "0d": 0, " 2h ": 2 * time.Hour,
	}
	for in, want := range tests {
		got, err := ParseDuration(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"", "d", "7days", "1dxx", "99999999999999999999d", "soon"} {
		_, err := ParseDuration(in)
		assert.ErrorIs(t, err, ErrInvalidDuration, in)
	}
}

func TestKeys(t *testing.T) {
	keys := Keys(reflect.ValueOf(*DefaultConfig()), "")
	assert.Contains(t, keys, "database.dsn")
	assert.Contains(t, keys, "listen.tls.cert_file")
	assert.Contains(t, keys, "workers.schedules.purge_snapshots")
	assert.Contains(t, keys, "auth.oidc.admin_groups")
	assert.NotContains(t, keys, "workers.schedules")
}

// Package config holds the server configuration, read from server.yaml or
// server.yml and overridden by LACIS_* environment variables.
package config

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/alexandreh2ag/lacis/cdn"
	"github.com/go-playground/validator/v10"
	"github.com/robfig/cron/v3"
)

const (
	// Name is the base name of the configuration file.
	Name = "server"
	// EnvPrefix prefixes the environment variables, e.g. LACIS_DATABASE_DSN.
	EnvPrefix = "LACIS"

	DefaultDir = "/etc/lacis"

	LogFormatText = "text"
	LogFormatJSON = "json"

	day = 24 * time.Hour
)

type Config struct {
	Listen   ListenConfig   `mapstructure:"listen"`
	Database DatabaseConfig `mapstructure:"database"`
	Auth     AuthConfig     `mapstructure:"auth"`
	AgentAPI AgentAPIConfig `mapstructure:"agent_api"`
	Icons    IconsConfig    `mapstructure:"icons"`
	GraphQL  GraphQLConfig  `mapstructure:"graphql"`
	Linking  LinkingConfig  `mapstructure:"linking"`
	Graph    GraphConfig    `mapstructure:"graph"`
	History  HistoryConfig  `mapstructure:"history"`
	Defaults DefaultsConfig `mapstructure:"defaults"`
	Workers  WorkersConfig  `mapstructure:"workers"`
	Metrics  MetricsConfig  `mapstructure:"metrics"`
	MCP      MCPConfig      `mapstructure:"mcp"`
	Log      LogConfig      `mapstructure:"log"`
}

type ListenConfig struct {
	Address   string `mapstructure:"address" validate:"required"`
	PublicURL string `mapstructure:"public_url" validate:"omitempty,url"`
	// BasePath prefixes every route, e.g. /lacis. It is changed without
	// rebuilding the front.
	BasePath string    `mapstructure:"base_path" validate:"required,startswith=/"`
	TLS      TLSConfig `mapstructure:"tls"`
}

// Prefix returns the base path with a leading and a trailing slash, the form
// the front expects: "/" or "/lacis/".
func (l ListenConfig) Prefix() string {
	p := "/" + strings.Trim(l.BasePath, "/") + "/"
	if p == "//" {
		return "/"
	}
	return p
}

// PublicBase is the public URL without its trailing slash: the base the
// URLs given to the clients start with.
func (l ListenConfig) PublicBase() string {
	return strings.TrimSuffix(l.PublicURL, "/")
}

// MCPURL is the URL of the MCP endpoint.
func (l ListenConfig) MCPURL() string {
	return l.PublicBase() + "/mcp"
}

type TLSConfig struct {
	CertFile string `mapstructure:"cert_file" validate:"required_with=KeyFile"`
	KeyFile  string `mapstructure:"key_file" validate:"required_with=CertFile"`
}

// Enabled reports whether the server terminates TLS itself.
func (t TLSConfig) Enabled() bool {
	return t.CertFile != "" && t.KeyFile != ""
}

type DatabaseConfig struct {
	DSN            string `mapstructure:"dsn" validate:"required"`
	MaxOpenConns   int    `mapstructure:"max_open_conns" validate:"min=1"`
	MigrateOnStart bool   `mapstructure:"migrate_on_start"`
}

type AuthConfig struct {
	Session SessionConfig `mapstructure:"session"`
	Local   LocalConfig   `mapstructure:"local"`
	OIDC    OIDCConfig    `mapstructure:"oidc"`
}

type SessionConfig struct {
	Secret string `mapstructure:"secret" validate:"required,min=32"`
	// TTL is how long a session lives unused; each use pushes it back.
	TTL time.Duration `mapstructure:"ttl" validate:"min=1m"`
	// MaxLifetime ends a session that long after the login, used or not:
	// the user logs in again, which reads their OIDC groups again.
	MaxLifetime time.Duration `mapstructure:"max_lifetime" validate:"min=1m"`
	// CookieSecure sends the session cookie over HTTPS only. Set it to
	// false only to reach the server in plain HTTP, e.g. in development;
	// the cookie stays Secure when the server serves TLS or its public URL
	// is HTTPS.
	CookieSecure bool `mapstructure:"cookie_secure"`
}

type LocalConfig struct {
	Enabled                bool   `mapstructure:"enabled"`
	BootstrapAdminUsername string `mapstructure:"bootstrap_admin_username"`
	BootstrapAdminPassword string `mapstructure:"bootstrap_admin_password" validate:"required_with=BootstrapAdminUsername,omitempty,min=8"`
}

type OIDCConfig struct {
	Enabled      bool     `mapstructure:"enabled"`
	Issuer       string   `mapstructure:"issuer" validate:"required_if=Enabled true,omitempty,url"`
	ClientID     string   `mapstructure:"client_id" validate:"required_if=Enabled true"`
	ClientSecret string   `mapstructure:"client_secret" validate:"required_if=Enabled true"`
	Scopes       []string `mapstructure:"scopes"`
	GroupsClaim  string   `mapstructure:"groups_claim"`
	// UsernameClaim is the claim giving the username of a user created at
	// its first login.
	UsernameClaim string `mapstructure:"username_claim" validate:"required_if=Enabled true"`
}

type AgentAPIConfig struct {
	// MinInterval is the least time between two snapshots of an agent: a
	// snapshot sooner gets 429. 0 accepts them all.
	MinInterval            time.Duration `mapstructure:"min_interval" validate:"min=0"`
	MaxBodyCompressed      ByteSize      `mapstructure:"max_body_compressed" validate:"min=1"`
	MaxBodyDecompressed    ByteSize      `mapstructure:"max_body_decompressed" validate:"gtefield=MaxBodyCompressed"`
	AcceptedSchemaVersions []int         `mapstructure:"accepted_schema_versions" validate:"required,min=1,dive,min=1"`
}

// IconsConfig bounds the logos uploaded for the technologies: PNG, JPEG,
// WebP or SVG.
type IconsConfig struct {
	MaxSize ByteSize `mapstructure:"max_size" validate:"min=1024"`
	// MinSide and MaxSide bound each side of a raster logo, in pixels.
	MinSide int `mapstructure:"min_side" validate:"min=1"`
	MaxSide int `mapstructure:"max_side" validate:"gtefield=MinSide"`
}

type GraphQLConfig struct {
	Introspection bool `mapstructure:"introspection"`
	MaxDepth      int  `mapstructure:"max_depth" validate:"min=1"`
	MaxComplexity int  `mapstructure:"max_complexity" validate:"min=1"`
}

type LinkingConfig struct {
	// CDNs are CDNs known by name (cdn.Names), their ranges built in: a
	// target resolving into them can only be linked through a route, and
	// the link names the CDN.
	CDNs []string `mapstructure:"cdns"`
	// CDNRanges are the IP ranges of a CDN not known by name, as CDNs.
	CDNRanges []string `mapstructure:"cdn_ranges" validate:"dive,cidr"`
	// HTTPPorts are the ports HTTP is served on: a name called without
	// scheme on one of them may be routed by a webserver, and such a port
	// of a host nobody is known to listen on leads to its webserver.
	HTTPPorts []int `mapstructure:"http_ports" validate:"min=1,dive,min=1,max=65535"`
}

type GraphConfig struct {
	// MaxNodes is the size above which the graph is not drawn.
	MaxNodes int `mapstructure:"max_nodes" validate:"min=1"`
}

type HistoryConfig struct {
	// MaxEvents is the number of change events kept per namespace, the
	// latest; the task purge_history deletes the older ones. 0 keeps all.
	MaxEvents int `mapstructure:"max_events" validate:"min=0"`
}

// DefaultsConfig is applied to new namespaces.
type DefaultsConfig struct {
	StaleAfter        time.Duration `mapstructure:"stale_after" validate:"min=1h"`
	InactiveAfter     time.Duration `mapstructure:"inactive_after" validate:"gtfield=StaleAfter"`
	SnapshotRetention time.Duration `mapstructure:"snapshot_retention" validate:"min=1h"`
}

// TaskNames are the scheduled tasks workers.schedules gives a schedule
// to; the queue package runs them.
var TaskNames = []string{"purge_snapshots", "purge_removed_entities", "purge_expired_tokens", "detect_token_conflicts", "purge_history", "purge_inactive_hosts"}

type WorkersConfig struct {
	// Enabled runs the workers and the scheduler in the start process;
	// false leaves them to "lacis-server worker".
	Enabled     bool `mapstructure:"enabled"`
	Concurrency int  `mapstructure:"concurrency" validate:"min=1"`
	// Schedules gives a cron schedule (five fields, or @daily…) to a task
	// of TaskNames; empty leaves it to the runs asked by hand.
	Schedules map[string]string `mapstructure:"schedules"`
	Retention RetentionConfig   `mapstructure:"retention"`
	Params    WorkersParams     `mapstructure:"params"`
}

// RetentionConfig is how long River keeps finished jobs.
type RetentionConfig struct {
	Completed time.Duration `mapstructure:"completed" validate:"min=1m"`
	Cancelled time.Duration `mapstructure:"cancelled" validate:"min=1m"`
	Discarded time.Duration `mapstructure:"discarded" validate:"min=1m"`
}

type WorkersParams struct {
	PurgeRemovedEntitiesAfter time.Duration `mapstructure:"purge_removed_entities_after" validate:"min=1h"`
	PurgeInactiveHostsAfter   time.Duration `mapstructure:"purge_inactive_hosts_after" validate:"min=1h"`
}

// MCPConfig is the MCP endpoint, through which an assistant such as Claude
// reads the inventory, in the name of a user who authorized it by OAuth.
type MCPConfig struct {
	Enabled bool `mapstructure:"enabled"`
	// AccessTokenTTL is how long an access token serves; the client gets
	// another with its refresh token.
	AccessTokenTTL time.Duration `mapstructure:"access_token_ttl" validate:"min=1m"`
	// GrantLifetime ends an authorization that long after it was given:
	// the user authorizes again, which reads their OIDC groups again.
	GrantLifetime time.Duration `mapstructure:"grant_lifetime" validate:"min=1h"`
}

// MetricsConfig is reserved for a metrics endpoint that does not exist
// yet: Validate refuses enabled, rather than accept a setting that would
// silently do nothing.
type MetricsConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type LogConfig struct {
	Level  string `mapstructure:"level" validate:"oneof=debug info warn error DEBUG INFO WARN ERROR"`
	Format string `mapstructure:"format" validate:"oneof=text json"`
}

// DefaultConfig returns the configuration used for every key that the file
// and the environment leave unset.
func DefaultConfig() *Config {
	return &Config{
		Listen: ListenConfig{
			Address:  "0.0.0.0:8080",
			BasePath: "/",
		},
		Database: DatabaseConfig{
			MaxOpenConns:   20,
			MigrateOnStart: true,
		},
		Auth: AuthConfig{
			Session: SessionConfig{TTL: 12 * time.Hour, MaxLifetime: 12 * time.Hour, CookieSecure: true},
			Local:   LocalConfig{Enabled: true},
			OIDC: OIDCConfig{
				Scopes:        []string{"openid", "profile"},
				GroupsClaim:   "groups",
				UsernameClaim: "preferred_username",
			},
		},
		AgentAPI: AgentAPIConfig{
			MinInterval:            30 * time.Second,
			MaxBodyCompressed:      5 * MB,
			MaxBodyDecompressed:    10 * MB,
			AcceptedSchemaVersions: []int{1},
		},
		Icons: IconsConfig{
			MaxSize: 256 * KB,
			MinSide: 16,
			MaxSide: 512,
		},
		GraphQL: GraphQLConfig{
			MaxDepth:      10,
			MaxComplexity: 1000,
		},
		Linking: LinkingConfig{CDNs: []string{}, CDNRanges: []string{}, HTTPPorts: []int{80, 443}},
		Graph:   GraphConfig{MaxNodes: 500},
		History: HistoryConfig{MaxEvents: 100000},
		Defaults: DefaultsConfig{
			StaleAfter:        7 * day,
			InactiveAfter:     30 * day,
			SnapshotRetention: 30 * day,
		},
		Workers: WorkersConfig{
			Enabled:     true,
			Concurrency: 4,
			Schedules: map[string]string{
				"purge_snapshots":        "15 3 * * *",
				"purge_removed_entities": "30 3 * * *",
				"purge_expired_tokens":   "45 3 * * *",
				"detect_token_conflicts": "0 * * * *",
				"purge_history":          "20 * * * *",
				"purge_inactive_hosts":   "",
			},
			Retention: RetentionConfig{
				Completed: 24 * time.Hour,
				Cancelled: 24 * time.Hour,
				Discarded: 7 * day,
			},
			Params: WorkersParams{
				PurgeRemovedEntitiesAfter: 90 * day,
				PurgeInactiveHostsAfter:   180 * day,
			},
		},
		MCP: MCPConfig{AccessTokenTTL: time.Hour, GrantLifetime: 30 * day},
		Log: LogConfig{
			Level:  "info",
			Format: LogFormatText,
		},
	}
}

// Validate checks the configuration and returns all the errors found.
func (c *Config) Validate(v *validator.Validate) error {
	var errs []error
	if err := v.Struct(c); err != nil {
		var verrs validator.ValidationErrors
		if !errors.As(err, &verrs) {
			return err
		}
		for _, e := range verrs {
			errs = append(errs, fmt.Errorf("%s: failed on %q", e.Namespace(), e.Tag()))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(c.Workers.Schedules)) {
		spec := c.Workers.Schedules[name]
		if !slices.Contains(TaskNames, name) {
			errs = append(errs, fmt.Errorf("Config.Workers.Schedules.%s: unknown task, one of %s", name, strings.Join(TaskNames, ", ")))
		} else if _, err := cron.ParseStandard(spec); spec != "" && err != nil {
			errs = append(errs, fmt.Errorf("Config.Workers.Schedules.%s: invalid cron schedule %q: %w", name, spec, err))
		}
	}
	for i, name := range c.Linking.CDNs {
		if _, ok := cdn.Known(name); !ok {
			errs = append(errs, fmt.Errorf("Config.Linking.CDNs[%d]: unknown CDN %q, one of %s; list its ranges in linking.cdn_ranges", i, name, strings.Join(cdn.Names(), ", ")))
		}
	}
	// The provider sends the browser back to the public URL.
	if c.Auth.OIDC.Enabled && c.Listen.PublicURL == "" {
		errs = append(errs, errors.New("Config.Listen.PublicURL: required when auth.oidc is enabled, for the callback of the provider"))
	}
	// The clients reach the MCP endpoint and the OAuth one at the public
	// URL, which the metadata give.
	if c.MCP.Enabled && c.Listen.PublicURL == "" {
		errs = append(errs, errors.New("Config.Listen.PublicURL: required when mcp is enabled, for the OAuth metadata"))
	}
	if c.Metrics.Enabled {
		errs = append(errs, errors.New("Config.Metrics.Enabled: not implemented, the server has no metrics endpoint yet; remove metrics.enabled"))
	}
	return errors.Join(errs...)
}

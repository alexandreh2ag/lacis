// Package config holds the agent configuration, read from agent.yaml or
// agent.yml and overridden by LACIS_* environment variables.
package config

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"time"

	"github.com/alexandreh2ag/lacis/common/labels"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/afero"
)

const (
	// Name is the base name of the configuration file.
	Name = "agent"
	// EnvPrefix prefixes the environment variables, e.g. LACIS_LABELS_ENV.
	EnvPrefix = "LACIS"

	DefaultDir      = "/etc/lacis"
	DefaultStateDir = "/var/lib/lacis"
)

type Config struct {
	Server     ServerConfig     `mapstructure:"server"`
	Auth       AuthConfig       `mapstructure:"auth"`
	Hostname   string           `mapstructure:"hostname" validate:"omitempty,hostname_rfc1123"`
	Labels     labels.Labels    `mapstructure:"labels"`
	Interval   time.Duration    `mapstructure:"interval" validate:"min=1m"`
	Jitter     time.Duration    `mapstructure:"jitter" validate:"min=0"`
	Collectors CollectorsConfig `mapstructure:"collectors"`
}

type ServerConfig struct {
	// URL is the base URL of lacis-server, base path included. HTTPS is
	// mandatory.
	URL    string `mapstructure:"url" validate:"required,url,startswith=https://"`
	CAFile string `mapstructure:"ca_file" validate:"omitempty,startswith=/"`
	// Insecure disables the verification of the server certificate. HTTPS is
	// still required; meant for development only.
	Insecure bool `mapstructure:"insecure"`
}

// TLSConfig returns the TLS configuration used to reach the server: the
// system roots plus CAFile when set, no verification when Insecure is true.
func (s ServerConfig) TLSConfig(fs afero.Fs) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: s.Insecure}
	if s.CAFile == "" {
		return cfg, nil
	}
	pem, err := afero.ReadFile(fs, s.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read CA file: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("CA file %s: no PEM certificate found", s.CAFile)
	}
	cfg.RootCAs = pool
	return cfg, nil
}

type AuthConfig struct {
	// EnrollmentToken is only used at the first start, when StateDir holds
	// no identity yet.
	EnrollmentToken string `mapstructure:"enrollment_token"`
	StateDir        string `mapstructure:"state_dir" validate:"required,startswith=/"`
}

type CollectorsConfig struct {
	Timeout  time.Duration  `mapstructure:"timeout" validate:"min=1s"`
	Disabled []string       `mapstructure:"disabled"`
	Apps     AppsConfig     `mapstructure:"apps"`
	Envfiles EnvfilesConfig `mapstructure:"envfiles"`
	Traefik  TraefikConfig  `mapstructure:"traefik"`
	Docker   DockerConfig   `mapstructure:"docker"`
}

type AppsConfig struct {
	ScanPaths   []string            `mapstructure:"scan_paths"`
	MaxDepth    int                 `mapstructure:"max_depth" validate:"min=1"`
	MaxFiles    int                 `mapstructure:"max_files" validate:"min=1"`
	Exclude     []string            `mapstructure:"exclude"`
	ExtraConfig []ExtraConfig       `mapstructure:"extra_config" validate:"dive"`
	Frameworks  []FrameworkDetector `mapstructure:"frameworks" validate:"dive"`
}

// ExtraConfig adds config directories to the apps whose path matches App.
type ExtraConfig struct {
	App   string   `mapstructure:"app" validate:"required"`
	Paths []string `mapstructure:"paths" validate:"required,min=1"`
}

// FrameworkDetector is a declarative framework detector. A detector named
// like a built-in one replaces it.
type FrameworkDetector struct {
	Name   string            `mapstructure:"name" validate:"required"`
	Detect FrameworkDetect   `mapstructure:"detect"`
	Config []FrameworkSource `mapstructure:"config" validate:"dive"`
}

type FrameworkDetect struct {
	ComposerPackage string `mapstructure:"composer_package"`
	NpmPackage      string `mapstructure:"npm_package"`
	File            string `mapstructure:"file"`
}

type FrameworkSource struct {
	// Path is relative to the app root; globs are accepted.
	Path   string `mapstructure:"path" validate:"required"`
	Format string `mapstructure:"format" validate:"required,oneof=env php ini yaml json"`
}

type EnvfilesConfig struct {
	AllowKeys []string `mapstructure:"allow_keys"`
	DenyKeys  []string `mapstructure:"deny_keys"`
}

type TraefikConfig struct {
	APIURL string `mapstructure:"api_url" validate:"omitempty,url"`
}

type DockerConfig struct {
	Socket string `mapstructure:"socket" validate:"omitempty,startswith=/"`
}

// DefaultConfig returns the configuration used for every key that the file
// and the environment leave unset.
func DefaultConfig() *Config {
	return &Config{
		Auth: AuthConfig{
			StateDir: DefaultStateDir,
		},
		Interval: time.Hour,
		Jitter:   5 * time.Minute,
		Collectors: CollectorsConfig{
			Timeout:  30 * time.Second,
			Disabled: []string{},
			Apps: AppsConfig{
				ScanPaths: []string{"/var/www"},
				MaxDepth:  4,
				MaxFiles:  5000,
				Exclude: []string{
					"vendor", "node_modules", "var/cache", "var/log", "log", "logs", "media", "uploads",
					"public/media", "public/uploads", "storage", ".git/objects",
				},
				ExtraConfig: []ExtraConfig{},
				Frameworks:  []FrameworkDetector{},
			},
			Envfiles: EnvfilesConfig{
				AllowKeys: []string{"*_URL", "*_DSN", "*_HOST", "*_PORT", "APP_ENV", "NODE_ENV"},
				DenyKeys:  []string{"*SECRET*", "*PASSWORD*", "*TOKEN*", "*KEY*"},
			},
			Docker: DockerConfig{
				Socket: "/var/run/docker.sock",
			},
		},
	}
}

// Validate checks the configuration and returns all the errors found.
func (c *Config) Validate(v *validator.Validate) error {
	var errs []error
	if err := v.Struct(c); err != nil {
		var verrs validator.ValidationErrors
		if errors.As(err, &verrs) {
			for _, e := range verrs {
				errs = append(errs, fmt.Errorf("%s: failed on %q", e.Namespace(), e.Tag()))
			}
		} else {
			errs = append(errs, err)
		}
	}
	if err := c.Labels.Validate(); err != nil {
		for _, e := range err.(interface{ Unwrap() []error }).Unwrap() {
			errs = append(errs, fmt.Errorf("labels: %w", e))
		}
	}
	return errors.Join(errs...)
}

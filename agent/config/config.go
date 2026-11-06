// Package config holds the agent configuration, read from agent.yaml or
// agent.yml and overridden by LACIS_* environment variables.
package config

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
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
	// URL is the base URL of lacis-server, base path included. HTTP is
	// accepted, for a server without TLS, but the agent then warns: the
	// tokens travel in clear.
	URL    string `mapstructure:"url" validate:"required,http_url"`
	CAFile string `mapstructure:"ca_file" validate:"omitempty,startswith=/"`
	// CAOnly trusts CAFile only, not the system roots.
	CAOnly bool `mapstructure:"ca_only"`
	// CertFile and KeyFile are the client certificate presented to the
	// server (mutual TLS), both or none.
	CertFile string `mapstructure:"cert_file" validate:"omitempty,startswith=/"`
	KeyFile  string `mapstructure:"key_file" validate:"omitempty,startswith=/"`
	// Insecure disables the verification of the server certificate; meant
	// for development only.
	Insecure bool `mapstructure:"insecure"`
}

// TLSConfig returns the TLS configuration used to reach the server: the
// system roots plus CAFile when set (CAFile only with CAOnly), the client
// certificate when set, no verification when Insecure is true.
func (s ServerConfig) TLSConfig(fs afero.Fs) (*tls.Config, error) {
	cfg, err := tlsConfig(fs, s.CAFile, s.CAOnly, s.Insecure)
	if err != nil {
		return nil, err
	}
	if s.CertFile == "" && s.KeyFile == "" {
		return cfg, nil
	}
	certPEM, err := afero.ReadFile(fs, s.CertFile)
	if err != nil {
		return nil, fmt.Errorf("read client certificate: %w", err)
	}
	keyPEM, err := afero.ReadFile(fs, s.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("read client key: %w", err)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("client certificate %s: %w", s.CertFile, err)
	}
	cfg.Certificates = []tls.Certificate{cert}
	return cfg, nil
}

// tlsConfig trusts the system roots plus caFile when set, caFile only when
// caOnly is true.
func tlsConfig(fs afero.Fs, caFile string, caOnly, insecure bool) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecure}
	if caFile == "" {
		return cfg, nil
	}
	pem, err := afero.ReadFile(fs, caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !caOnly {
		if system, err := x509.SystemCertPool(); err == nil {
			pool = system
		}
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("CA file %s: no PEM certificate found", caFile)
	}
	cfg.RootCAs = pool
	return cfg, nil
}

type AuthConfig struct {
	// EnrollmentToken is only used at the first start, when StateDir holds
	// no identity yet. EnrollmentTokenFile holds it instead, so that it is
	// not left in agent.yaml.
	EnrollmentToken     string `mapstructure:"enrollment_token"`
	EnrollmentTokenFile string `mapstructure:"enrollment_token_file" validate:"omitempty,startswith=/"`
	StateDir            string `mapstructure:"state_dir" validate:"required,startswith=/"`
}

// Token returns the enrollment token, read from EnrollmentTokenFile when
// set; empty when there is none.
func (a AuthConfig) Token(fs afero.Fs) (string, error) {
	if a.EnrollmentTokenFile == "" {
		return a.EnrollmentToken, nil
	}
	b, err := afero.ReadFile(fs, a.EnrollmentTokenFile)
	if err != nil {
		return "", fmt.Errorf("read enrollment token: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

type CollectorsConfig struct {
	Timeout  time.Duration  `mapstructure:"timeout" validate:"min=1s"`
	Disabled []string       `mapstructure:"disabled"`
	Apps     AppsConfig     `mapstructure:"apps"`
	Envfiles EnvfilesConfig `mapstructure:"envfiles"`
	Traefik  TraefikConfig  `mapstructure:"traefik"`
	Docker   DockerConfig   `mapstructure:"docker"`
}

// CommandMode tells how much of a launcher command is sent to the server.
type CommandMode string

const (
	// CommandPath sends the program path alone (argv[0]): the arguments of a
	// command often hold a password, such as mysqldump -pSECRET.
	CommandPath CommandMode = "path"
	// CommandFull sends the whole command, arguments included.
	CommandFull CommandMode = "full"
)

type AppsConfig struct {
	// Commands is path (the default) or full; see CommandMode.
	Commands  CommandMode `mapstructure:"commands" validate:"oneof=path full"`
	ScanPaths []string    `mapstructure:"scan_paths"`
	MaxDepth  int         `mapstructure:"max_depth" validate:"min=1"`
	MaxFiles  int         `mapstructure:"max_files" validate:"min=1"`
	// GitMaxDepth is how many parent directories of the resolved app root
	// are searched for the .git of the app when the root holds none, as for
	// an app in a subdirectory of its repository; 0, the default, searches
	// the root only.
	GitMaxDepth int      `mapstructure:"git_max_depth" validate:"min=0"`
	Exclude     []string `mapstructure:"exclude"`
	// ExtraConfig adds configuration files to the apps of a path, read
	// after those of their framework.
	ExtraConfig []ExtraConfig       `mapstructure:"extra_config" validate:"dive"`
	Frameworks  []FrameworkDetector `mapstructure:"frameworks" validate:"dive"`
}

// ExtraConfig adds configuration files to the apps whose path, as seen
// or resolved, matches the glob App: a configuration kept out of the app
// root, such as /etc/acme/shop.env, or one its framework loader does not
// read. The files are read after those of the framework, in order, the last
// winning; the environment of the launchers still comes last.
type ExtraConfig struct {
	App string `mapstructure:"app" validate:"required,startswith=/"`
	// Config are the files: a path relative to the app root, or absolute;
	// globs are accepted.
	Config []FrameworkSource `mapstructure:"config" validate:"required,min=1,dive"`
}

// ExtraConfigFormats are the formats an extra configuration file can have.
var ExtraConfigFormats = SourceFormats

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
	Path string `mapstructure:"path" validate:"required"`
	// Format is the format of the file: env (a .env), or php (a file
	// returning an array), ini, yaml, json, whose nested keys are flattened
	// into env keys (doctrine.dbal.url gives DOCTRINE_DBAL_URL).
	Format string `mapstructure:"format" validate:"required"`
}

// SourceFormats are the formats a declared framework source can have.
var SourceFormats = []string{"env", "php", "ini", "yaml", "json"}

type EnvfilesConfig struct {
	AllowKeys []string `mapstructure:"allow_keys"`
	DenyKeys  []string `mapstructure:"deny_keys"`
}

type TraefikConfig struct {
	APIURL string `mapstructure:"api_url" validate:"omitempty,url"`
	// Username and Password authenticate to the API with basic auth.
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	// CAFile is added to the system roots to verify an HTTPS API (the only
	// root with CAOnly).
	CAFile string `mapstructure:"ca_file" validate:"omitempty,startswith=/"`
	CAOnly bool   `mapstructure:"ca_only"`
	// Insecure disables the verification of the API certificate.
	Insecure bool `mapstructure:"insecure"`
}

// TLSConfig returns the TLS configuration used to reach the Traefik API.
func (t TraefikConfig) TLSConfig(fs afero.Fs) (*tls.Config, error) {
	return tlsConfig(fs, t.CAFile, t.CAOnly, t.Insecure)
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
				Commands:  CommandPath,
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
				DenyKeys: []string{
					"*SECRET*", "*PASS*", "*PWD*", "*TOKEN*", "*KEY*", "*CRED*", "*AUTH*", "*PRIVATE*",
					"*SALT*", "*COOKIE*", "*WEBHOOK*",
				},
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
	errs = append(errs, c.validateRules()...)
	if err := c.Labels.Validate(); err != nil {
		for _, e := range err.(interface{ Unwrap() []error }).Unwrap() {
			errs = append(errs, fmt.Errorf("labels: %w", e))
		}
	}
	return errors.Join(errs...)
}

// validateRules checks what the struct tags cannot express, and refuses
// what the agent would silently ignore.
func (c *Config) validateRules() []error {
	var errs []error
	s := c.Server
	if s.CAOnly && s.CAFile == "" {
		errs = append(errs, errors.New("server.ca_only: requires server.ca_file"))
	}
	if (s.CertFile == "") != (s.KeyFile == "") {
		errs = append(errs, errors.New("server.cert_file and server.key_file: set both or none"))
	}
	if c.Auth.EnrollmentToken != "" && c.Auth.EnrollmentTokenFile != "" {
		errs = append(errs, errors.New("auth.enrollment_token and auth.enrollment_token_file: set one only"))
	}
	t := c.Collectors.Traefik
	if t.CAOnly && t.CAFile == "" {
		errs = append(errs, errors.New("collectors.traefik.ca_only: requires collectors.traefik.ca_file"))
	}
	if t.Password != "" && t.Username == "" {
		errs = append(errs, errors.New("collectors.traefik.password: requires collectors.traefik.username"))
	}
	for i, x := range c.Collectors.Apps.ExtraConfig {
		if _, err := path.Match(x.App, ""); err != nil {
			errs = append(errs, fmt.Errorf("collectors.apps.extra_config[%d].app: %w", i, err))
		}
		for j, src := range x.Config {
			if src.Format != "" && !slices.Contains(ExtraConfigFormats, src.Format) {
				errs = append(errs, fmt.Errorf("collectors.apps.extra_config[%d].config[%d].format: %q is not supported, use one of %s",
					i, j, src.Format, strings.Join(ExtraConfigFormats, ", ")))
			}
		}
	}
	for i, d := range c.Collectors.Apps.Frameworks {
		for j, src := range d.Config {
			if src.Format != "" && !slices.Contains(SourceFormats, src.Format) {
				errs = append(errs, fmt.Errorf("collectors.apps.frameworks[%d].config[%d].format: %q is not supported, use one of %s",
					i, j, src.Format, strings.Join(SourceFormats, ", ")))
			}
		}
	}
	return errs
}

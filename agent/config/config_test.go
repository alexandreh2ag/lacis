package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/common/duration"
	"github.com/alexandreh2ag/lacis/common/labels"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fullYAML = `
server:
  url: https://lacis.example.com
  ca_file: /etc/lacis/ca.pem
  ca_only: true
  cert_file: /etc/lacis/agent.crt
  key_file: /etc/lacis/agent.key
  insecure: true
auth:
  enrollment_token: change-me
  state_dir: /var/lib/lacis
labels:
  namespace: acme
  project: site
  env: prod
  index: "1"
interval: 2h
jitter: 10m
collectors:
  timeout: 45s
  disabled: [docker]
  apps:
    scan_paths: ["/var/www", "/home/*/www"]
    max_depth: 3
    git_max_depth: 2
    max_files: 100
    exclude: [vendor]
    frameworks:
      - name: laravel
        detect:
          composer_package: laravel/framework
        config:
          - { path: ".env", format: env }
          - { path: ".env.local", format: env }
  envfiles:
    allow_keys: ["*_URL"]
    deny_keys: ["*SECRET*"]
  traefik:
    api_url: https://127.0.0.1:8080
    username: lacis
    password: change-me
    ca_file: /etc/lacis/traefik-ca.pem
`

func validConfig() *Config {
	cfg := DefaultConfig()
	cfg.Server.URL = "https://lacis.example.com"
	cfg.Labels = labels.Labels{Namespace: "acme", Project: "site", Env: "prod"}
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
		{name: "yaml in default dir", files: []string{"/etc/lacis/agent.yaml"}, want: "/etc/lacis/agent.yaml"},
		{name: "yml in default dir", files: []string{"/etc/lacis/agent.yml"}, want: "/etc/lacis/agent.yml"},
		{
			name:        "yaml wins over yml",
			files:       []string{"/etc/lacis/agent.yml", "/etc/lacis/agent.yaml"},
			want:        "/etc/lacis/agent.yaml",
			wantIgnored: []string{"/etc/lacis/agent.yml"},
		},
		{name: "explicit file", files: []string{"/opt/custom.yml"}, path: "/opt/custom.yml", want: "/opt/custom.yml"},
		{name: "explicit dir", files: []string{"/opt/lacis/agent.yml"}, path: "/opt/lacis", want: "/opt/lacis/agent.yml"},
		{name: "no file", files: []string{"/etc/lacis/server.yaml"}, want: ""},
		{name: "no dir", path: "/nowhere", want: ""},
		{name: "missing explicit file", path: "/opt/missing.yaml", wantErr: true},
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
	require.NoError(t, afero.WriteFile(fs, "/etc/lacis/agent.yml", []byte(fullYAML), 0o600))

	cfg, err := Load(fs, "/etc/lacis/agent.yml")
	require.NoError(t, err)

	assert.Equal(t, "https://lacis.example.com", cfg.Server.URL)
	assert.Equal(t, "/etc/lacis/ca.pem", cfg.Server.CAFile)
	assert.True(t, cfg.Server.CAOnly)
	assert.Equal(t, "/etc/lacis/agent.crt", cfg.Server.CertFile)
	assert.Equal(t, "/etc/lacis/agent.key", cfg.Server.KeyFile)
	assert.True(t, cfg.Server.Insecure)
	assert.Equal(t, "change-me", cfg.Auth.EnrollmentToken)
	assert.Equal(t, labels.Labels{Namespace: "acme", Project: "site", Env: "prod", Index: "1"}, cfg.Labels)
	assert.Equal(t, 2*time.Hour, cfg.Interval)
	assert.Equal(t, 10*time.Minute, cfg.Jitter)
	assert.Equal(t, 45*time.Second, cfg.Collectors.Timeout)
	assert.Equal(t, []string{"docker"}, cfg.Collectors.Disabled)
	require.Len(t, cfg.Collectors.Apps.Frameworks, 1)
	assert.Equal(t, FrameworkDetector{
		Name:   "laravel",
		Detect: FrameworkDetect{ComposerPackage: "laravel/framework"},
		Config: []FrameworkSource{{Path: ".env", Format: "env"}, {Path: ".env.local", Format: "env"}},
	}, cfg.Collectors.Apps.Frameworks[0])
	assert.Equal(t, []string{"*_URL"}, cfg.Collectors.Envfiles.AllowKeys)
	assert.Equal(t, TraefikConfig{APIURL: "https://127.0.0.1:8080", Username: "lacis", Password: "change-me", CAFile: "/etc/lacis/traefik-ca.pem"}, cfg.Collectors.Traefik)
	// Keys absent from the file keep their default.
	assert.Equal(t, "/var/run/docker.sock", cfg.Collectors.Docker.Socket)
	assert.Equal(t, CommandPath, cfg.Collectors.Apps.Commands, "only the program path by default")
	assert.Equal(t, 2, cfg.Collectors.Apps.GitMaxDepth)
	assert.NoError(t, cfg.Validate(newValidator()))
}

func TestLoad_Environment(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/etc/lacis/agent.yaml", []byte(fullYAML), 0o600))
	t.Setenv("LACIS_LABELS_ENV", "staging")
	t.Setenv("LACIS_SERVER_URL", "https://other.example.com/lacis")
	t.Setenv("LACIS_INTERVAL", "30m")
	t.Setenv("LACIS_COLLECTORS_DISABLED", "docker,apps")
	t.Setenv("LACIS_HOSTNAME", "custom-host")
	t.Setenv("LACIS_SERVER_INSECURE", "false")

	cfg, err := Load(fs, "/etc/lacis/agent.yaml")
	require.NoError(t, err)

	assert.Equal(t, "staging", cfg.Labels.Env)
	assert.Equal(t, "acme", cfg.Labels.Namespace)
	assert.Equal(t, "https://other.example.com/lacis", cfg.Server.URL)
	assert.Equal(t, 30*time.Minute, cfg.Interval)
	assert.Equal(t, []string{"docker", "apps"}, cfg.Collectors.Disabled)
	assert.Equal(t, "custom-host", cfg.Hostname)
	assert.False(t, cfg.Server.Insecure)
}

func TestLoad_EnvironmentOnly(t *testing.T) {
	t.Setenv("LACIS_LABELS_NAMESPACE", "acme")

	cfg, err := Load(afero.NewMemMapFs(), "")
	require.NoError(t, err)

	assert.Equal(t, "acme", cfg.Labels.Namespace)
	assert.Equal(t, DefaultConfig().Collectors, cfg.Collectors)
}

func TestLoad_Durations(t *testing.T) {
	tests := []struct {
		name, yaml string
		want       time.Duration
		wantErr    bool
	}{
		{name: "hours", yaml: "interval: 2h", want: 2 * time.Hour},
		{name: "days", yaml: "interval: 1d", want: 24 * time.Hour},
		{name: "days and hours", yaml: "interval: 1d12h", want: 36 * time.Hour},
		{name: "invalid", yaml: "interval: soon", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			require.NoError(t, afero.WriteFile(fs, "/agent.yaml", []byte(tt.yaml), 0o600))
			cfg, err := Load(fs, "/agent.yaml")
			if tt.wantErr {
				assert.ErrorIs(t, err, duration.ErrInvalid)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Interval)
		})
	}
}

func TestLoad_Errors(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/bad.yaml", []byte("labels: [unclosed"), 0o600))
	require.NoError(t, afero.WriteFile(fs, "/wrong-type.yaml", []byte("interval: {a: b}"), 0o600))

	_, err := Load(fs, "/missing.yaml")
	assert.ErrorContains(t, err, "read config")
	_, err = Load(fs, "/bad.yaml")
	assert.ErrorContains(t, err, "read config")
	_, err = Load(fs, "/wrong-type.yaml")
	assert.ErrorContains(t, err, "decode config")
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(c *Config)
		want   []string
	}{
		{name: "valid", mutate: func(*Config) {}},
		{name: "missing server url", mutate: func(c *Config) { c.Server.URL = "" }, want: []string{"Config.Server.URL", `"required"`}},
		{name: "http server url", mutate: func(c *Config) { c.Server.URL = "http://lacis.example.com" }},
		{name: "other scheme server url", mutate: func(c *Config) { c.Server.URL = "ftp://lacis.example.com" }, want: []string{"Config.Server.URL", `"http_url"`}},
		{name: "relative state dir", mutate: func(c *Config) { c.Auth.StateDir = "lacis" }, want: []string{"Config.Auth.StateDir"}},
		{name: "interval too short", mutate: func(c *Config) { c.Interval = time.Second }, want: []string{"Config.Interval", `"min"`}},
		{name: "invalid hostname", mutate: func(c *Config) { c.Hostname = "not a host" }, want: []string{"Config.Hostname"}},
		{name: "missing label", mutate: func(c *Config) { c.Labels.Project = "" }, want: []string{"labels: project: label is required"}},
		{name: "negative git max depth", mutate: func(c *Config) { c.Collectors.Apps.GitMaxDepth = -1 }, want: []string{"Config.Collectors.Apps.GitMaxDepth", `"min"`}},
		{name: "full commands", mutate: func(c *Config) { c.Collectors.Apps.Commands = CommandFull }},
		{name: "force keys", mutate: func(c *Config) { c.Collectors.Envfiles.ForceKeys = []string{"OAUTH_ISSUER_URL"} }},
		{name: "force key pattern", mutate: func(c *Config) { c.Collectors.Envfiles.ForceKeys = []string{"OK", "*_URL", ""} },
			want: []string{`force_keys[1]: "*_URL" is not an exact key name`, `force_keys[2]: "" is not an exact key name`}},
		{name: "invalid commands", mutate: func(c *Config) { c.Collectors.Apps.Commands = "args" }, want: []string{"Config.Collectors.Apps.Commands", `"oneof"`}},
		{
			name: "invalid framework format",
			mutate: func(c *Config) {
				c.Collectors.Apps.Frameworks = []FrameworkDetector{{Name: "x", Config: []FrameworkSource{{Path: ".env", Format: "toml"}}}}
			},
			want: []string{`collectors.apps.frameworks[0].config[0].format: "toml" is not supported, use one of env, php, ini, yaml, json`},
		},
		{
			name: "structured framework formats",
			mutate: func(c *Config) {
				c.Collectors.Apps.Frameworks = []FrameworkDetector{{Name: "x", Config: []FrameworkSource{
					{Path: ".env", Format: "env"}, {Path: "config.php", Format: "php"}, {Path: "app.ini", Format: "ini"},
					{Path: "config/*.yaml", Format: "yaml"}, {Path: "config.json", Format: "json"},
				}}}
			},
		},
		{
			name: "extra config format",
			mutate: func(c *Config) {
				c.Collectors.Apps.ExtraConfig = []ExtraConfig{{App: "/var/www/*", Config: []FrameworkSource{{Path: "/etc/app.env", Format: "env"}, {Path: "app.toml", Format: "toml"}}}}
			},
			want: []string{`collectors.apps.extra_config[0].config[1].format: "toml" is not supported, use one of env, php, ini, yaml, json`},
		},
		{
			name: "extra config bad glob",
			mutate: func(c *Config) {
				c.Collectors.Apps.ExtraConfig = []ExtraConfig{{App: "/var/www/[", Config: []FrameworkSource{{Path: ".env", Format: "env"}}}}
			},
			want: []string{"collectors.apps.extra_config[0].app: syntax error in pattern"},
		},
		{
			name: "extra config without files",
			mutate: func(c *Config) {
				c.Collectors.Apps.ExtraConfig = []ExtraConfig{{App: "/var/www/*"}}
			},
			want: []string{"Config.Collectors.Apps.ExtraConfig[0].Config"},
		},
		{name: "ca only without ca file", mutate: func(c *Config) { c.Server.CAOnly = true }, want: []string{"server.ca_only: requires server.ca_file"}},
		{name: "cert without key", mutate: func(c *Config) { c.Server.CertFile = "/etc/lacis/agent.crt" }, want: []string{"set both or none"}},
		{name: "key without cert", mutate: func(c *Config) { c.Server.KeyFile = "/etc/lacis/agent.key" }, want: []string{"set both or none"}},
		{
			name:   "token and token file",
			mutate: func(c *Config) { c.Auth.EnrollmentToken = "t"; c.Auth.EnrollmentTokenFile = "/etc/lacis/token" },
			want:   []string{"set one only"},
		},
		{name: "relative token file", mutate: func(c *Config) { c.Auth.EnrollmentTokenFile = "token" }, want: []string{"Config.Auth.EnrollmentTokenFile"}},
		{name: "traefik ca only without ca file", mutate: func(c *Config) { c.Collectors.Traefik.CAOnly = true }, want: []string{"collectors.traefik.ca_only"}},
		{name: "traefik password without username", mutate: func(c *Config) { c.Collectors.Traefik.Password = "p" }, want: []string{"collectors.traefik.password"}},
		{
			name:   "several errors",
			mutate: func(c *Config) { c.Server.URL = ""; c.Labels.Env = "" },
			want:   []string{"Config.Server.URL", "labels: env: label is required"},
		},
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

func TestAuthConfig_Token(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/etc/lacis/token", []byte("lacis_et_file\n"), 0o600))
	tests := []struct {
		name    string
		auth    AuthConfig
		want    string
		wantErr string
	}{
		{name: "none", auth: AuthConfig{}},
		{name: "inline", auth: AuthConfig{EnrollmentToken: "lacis_et_inline"}, want: "lacis_et_inline"},
		{name: "file", auth: AuthConfig{EnrollmentTokenFile: "/etc/lacis/token"}, want: "lacis_et_file"},
		{name: "missing file", auth: AuthConfig{EnrollmentTokenFile: "/etc/lacis/missing"}, wantErr: "read enrollment token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.auth.Token(fs)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestKeys(t *testing.T) {
	keys := Keys(reflect.TypeOf(Config{}), "")
	assert.Contains(t, keys, "server.url")
	assert.Contains(t, keys, "server.insecure")
	assert.Contains(t, keys, "labels.namespace")
	assert.Contains(t, keys, "interval")
	assert.Contains(t, keys, "collectors.apps.extra_config")
	assert.Contains(t, keys, "collectors.envfiles.deny_keys")
	assert.Contains(t, keys, "collectors.envfiles.force_keys")
	assert.NotContains(t, keys, "labels")
}

func testCAPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// testKeyPair returns a self-signed client certificate and its key.
func testKeyPair(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "agent"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func TestServerConfig_TLSConfig(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/etc/lacis/ca.pem", testCAPEM(t), 0o600))
	certPEM, keyPEM := testKeyPair(t)
	require.NoError(t, afero.WriteFile(fs, "/etc/lacis/agent.crt", certPEM, 0o600))
	require.NoError(t, afero.WriteFile(fs, "/etc/lacis/agent.key", keyPEM, 0o600))
	require.NoError(t, afero.WriteFile(fs, "/etc/lacis/empty.pem", []byte("nothing"), 0o600))

	tests := []struct {
		name         string
		server       ServerConfig
		wantInsecure bool
		wantRootCAs  bool
		// wantRoots is the number of roots, checked when set.
		wantRoots int
		wantCert  bool
		wantErr   string
	}{
		{name: "defaults", server: ServerConfig{}},
		{name: "insecure", server: ServerConfig{Insecure: true}, wantInsecure: true},
		{name: "ca file", server: ServerConfig{CAFile: "/etc/lacis/ca.pem"}, wantRootCAs: true},
		{name: "ca file and insecure", server: ServerConfig{CAFile: "/etc/lacis/ca.pem", Insecure: true}, wantInsecure: true, wantRootCAs: true},
		{name: "missing ca file", server: ServerConfig{CAFile: "/etc/lacis/missing.pem"}, wantErr: "read CA file"},
		{name: "ca file without certificate", server: ServerConfig{CAFile: "/etc/lacis/empty.pem"}, wantErr: "no PEM certificate found"},
		{name: "ca only", server: ServerConfig{CAFile: "/etc/lacis/ca.pem", CAOnly: true}, wantRootCAs: true, wantRoots: 1},
		{name: "client certificate", server: ServerConfig{CertFile: "/etc/lacis/agent.crt", KeyFile: "/etc/lacis/agent.key"}, wantCert: true},
		{name: "missing client certificate", server: ServerConfig{CertFile: "/etc/lacis/missing.crt", KeyFile: "/etc/lacis/agent.key"}, wantErr: "read client certificate"},
		{name: "missing client key", server: ServerConfig{CertFile: "/etc/lacis/agent.crt", KeyFile: "/etc/lacis/missing.key"}, wantErr: "read client key"},
		{name: "invalid client key", server: ServerConfig{CertFile: "/etc/lacis/agent.crt", KeyFile: "/etc/lacis/ca.pem"}, wantErr: "client certificate /etc/lacis/agent.crt"},
		{name: "invalid ca with client certificate", server: ServerConfig{CAFile: "/etc/lacis/empty.pem", CertFile: "/etc/lacis/agent.crt", KeyFile: "/etc/lacis/agent.key"}, wantErr: "no PEM"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.server.TLSConfig(fs)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, uint16(tls.VersionTLS12), got.MinVersion)
			assert.Equal(t, tt.wantInsecure, got.InsecureSkipVerify)
			assert.Equal(t, tt.wantRootCAs, got.RootCAs != nil)
			if tt.wantRoots > 0 {
				assert.Len(t, got.RootCAs.Subjects(), tt.wantRoots) //nolint:staticcheck // only the CA file is trusted
			}
			assert.Equal(t, tt.wantCert, len(got.Certificates) == 1)
		})
	}
}

func TestTraefikConfig_TLSConfig(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/etc/lacis/traefik-ca.pem", testCAPEM(t), 0o600))
	got, err := TraefikConfig{CAFile: "/etc/lacis/traefik-ca.pem", CAOnly: true}.TLSConfig(fs)
	require.NoError(t, err)
	assert.Len(t, got.RootCAs.Subjects(), 1) //nolint:staticcheck // only the CA file is trusted
	got, err = TraefikConfig{Insecure: true}.TLSConfig(fs)
	require.NoError(t, err)
	assert.True(t, got.InsecureSkipVerify)
	assert.Nil(t, got.RootCAs)
}

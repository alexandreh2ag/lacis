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
    max_files: 100
    exclude: [vendor]
    extra_config:
      - app: "/var/www/site-api*"
        paths: ["/etc/site-api/autoload"]
    frameworks:
      - name: laravel
        detect:
          composer_package: laravel/framework
        config:
          - { path: ".env", format: env }
          - { path: "config/database.php", format: php }
  envfiles:
    allow_keys: ["*_URL"]
    deny_keys: ["*SECRET*"]
  traefik:
    api_url: http://127.0.0.1:8080
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
	assert.True(t, cfg.Server.Insecure)
	assert.Equal(t, "change-me", cfg.Auth.EnrollmentToken)
	assert.Equal(t, labels.Labels{Namespace: "acme", Project: "site", Env: "prod", Index: "1"}, cfg.Labels)
	assert.Equal(t, 2*time.Hour, cfg.Interval)
	assert.Equal(t, 10*time.Minute, cfg.Jitter)
	assert.Equal(t, 45*time.Second, cfg.Collectors.Timeout)
	assert.Equal(t, []string{"docker"}, cfg.Collectors.Disabled)
	assert.Equal(t, []ExtraConfig{{App: "/var/www/site-api*", Paths: []string{"/etc/site-api/autoload"}}}, cfg.Collectors.Apps.ExtraConfig)
	require.Len(t, cfg.Collectors.Apps.Frameworks, 1)
	assert.Equal(t, FrameworkDetector{
		Name:   "laravel",
		Detect: FrameworkDetect{ComposerPackage: "laravel/framework"},
		Config: []FrameworkSource{{Path: ".env", Format: "env"}, {Path: "config/database.php", Format: "php"}},
	}, cfg.Collectors.Apps.Frameworks[0])
	assert.Equal(t, []string{"*_URL"}, cfg.Collectors.Envfiles.AllowKeys)
	assert.Equal(t, "http://127.0.0.1:8080", cfg.Collectors.Traefik.APIURL)
	// Keys absent from the file keep their default.
	assert.Equal(t, "/var/run/docker.sock", cfg.Collectors.Docker.Socket)
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
		{name: "http server url", mutate: func(c *Config) { c.Server.URL = "http://lacis.example.com" }, want: []string{"Config.Server.URL", `"startswith"`}},
		{name: "relative state dir", mutate: func(c *Config) { c.Auth.StateDir = "lacis" }, want: []string{"Config.Auth.StateDir"}},
		{name: "interval too short", mutate: func(c *Config) { c.Interval = time.Second }, want: []string{"Config.Interval", `"min"`}},
		{name: "invalid hostname", mutate: func(c *Config) { c.Hostname = "not a host" }, want: []string{"Config.Hostname"}},
		{name: "missing label", mutate: func(c *Config) { c.Labels.Project = "" }, want: []string{"labels: project: label is required"}},
		{
			name: "invalid framework format",
			mutate: func(c *Config) {
				c.Collectors.Apps.Frameworks = []FrameworkDetector{{Name: "x", Config: []FrameworkSource{{Path: ".env", Format: "toml"}}}}
			},
			want: []string{"Config.Collectors.Apps.Frameworks[0].Config[0].Format", `"oneof"`},
		},
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

func TestKeys(t *testing.T) {
	keys := Keys(reflect.TypeOf(Config{}), "")
	assert.Contains(t, keys, "server.url")
	assert.Contains(t, keys, "server.insecure")
	assert.Contains(t, keys, "labels.namespace")
	assert.Contains(t, keys, "interval")
	assert.Contains(t, keys, "collectors.apps.extra_config")
	assert.Contains(t, keys, "collectors.envfiles.deny_keys")
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

func TestServerConfig_TLSConfig(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/etc/lacis/ca.pem", testCAPEM(t), 0o600))
	require.NoError(t, afero.WriteFile(fs, "/etc/lacis/empty.pem", []byte("nothing"), 0o600))

	tests := []struct {
		name         string
		server       ServerConfig
		wantInsecure bool
		wantRootCAs  bool
		wantErr      string
	}{
		{name: "defaults", server: ServerConfig{}},
		{name: "insecure", server: ServerConfig{Insecure: true}, wantInsecure: true},
		{name: "ca file", server: ServerConfig{CAFile: "/etc/lacis/ca.pem"}, wantRootCAs: true},
		{name: "ca file and insecure", server: ServerConfig{CAFile: "/etc/lacis/ca.pem", Insecure: true}, wantInsecure: true, wantRootCAs: true},
		{name: "missing ca file", server: ServerConfig{CAFile: "/etc/lacis/missing.pem"}, wantErr: "read CA file"},
		{name: "ca file without certificate", server: ServerConfig{CAFile: "/etc/lacis/empty.pem"}, wantErr: "no PEM certificate found"},
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
		})
	}
}

package cli

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/service"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/alexandreh2ag/lacis/common/version"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validYAML = `
server:
  url: https://lacis.example.com
labels:
  namespace: acme
  project: site
  env: prod
`

func run(ctx *context.Context, args ...string) (string, error) {
	cmd := GetRootCmd(ctx)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestVersionCmd(t *testing.T) {
	ctx := context.TestContext(nil)
	out, err := run(ctx, "version")
	require.NoError(t, err)
	assert.Equal(t, version.GetFormattedVersion()+"\n", out)
}

func TestCollectCmd(t *testing.T) {
	logs := &bytes.Buffer{}
	ctx := context.TestContext(logs)
	require.NoError(t, afero.WriteFile(ctx.Fs, "/etc/lacis/agent.yaml", []byte(validYAML), 0o600))
	require.NoError(t, afero.WriteFile(ctx.Fs, "/etc/lacis/agent.yml", []byte("{}"), 0o600))
	require.NoError(t, afero.WriteFile(ctx.Fs, service.HostnameFile, []byte("acme-site-prod-1\n"), 0o644))

	out, err := run(ctx, "collect", "-l", "debug")
	require.NoError(t, err)

	s := &snapshot.Snapshot{}
	require.NoError(t, json.Unmarshal([]byte(out), s))
	assert.Equal(t, "acme-site-prod-1", s.Host.Hostname)
	assert.Equal(t, "acme", s.Namespace)
	assert.Equal(t, slog.LevelDebug, ctx.LogLevel.Level())
	assert.Contains(t, logs.String(), "config file ignored")
	assert.Contains(t, logs.String(), "using config file")
}

func TestCollectCmd_CustomConfigPath(t *testing.T) {
	ctx := context.TestContext(nil)
	require.NoError(t, afero.WriteFile(ctx.Fs, "/opt/lacis.yml", []byte(validYAML), 0o600))
	require.NoError(t, afero.WriteFile(ctx.Fs, service.HostnameFile, []byte("host\n"), 0o644))

	_, err := run(ctx, "collect", "-c", "/opt/lacis.yml")
	assert.NoError(t, err)
}

func TestCollectCmd_InsecureWarning(t *testing.T) {
	logs := &bytes.Buffer{}
	ctx := context.TestContext(logs)
	require.NoError(t, afero.WriteFile(ctx.Fs, "/etc/lacis/agent.yaml", []byte("server: {url: https://lacis.example.com, insecure: true}\nlabels: {namespace: acme, project: site, env: prod}\n"), 0o600))
	require.NoError(t, afero.WriteFile(ctx.Fs, service.HostnameFile, []byte("host\n"), 0o644))

	_, err := run(ctx, "collect")
	require.NoError(t, err)
	assert.Contains(t, logs.String(), "server certificate verification is disabled")
}

func TestCollectCmd_Errors(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		args  []string
		want  string
	}{
		{name: "invalid log level", args: []string{"collect", "-l", "loud"}, want: `invalid log level "loud"`},
		{name: "missing explicit file", args: []string{"collect", "-c", "/missing.yaml"}, want: "config file /missing.yaml"},
		{name: "unreadable file", files: map[string]string{"/etc/lacis/agent.yaml": "labels: [x"}, args: []string{"collect"}, want: "read config"},
		{name: "no config at all", args: []string{"collect", "-l", "debug"}, want: "invalid configuration"},
		{name: "missing label", files: map[string]string{"/etc/lacis/agent.yaml": "server: {url: https://x.example.com}\nlabels: {namespace: a, project: b}"}, args: []string{"collect"}, want: "env: label is required"},
		{name: "no hostname", files: map[string]string{"/etc/lacis/agent.yaml": validYAML}, args: []string{"collect"}, want: "unable to determine the hostname"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.TestContext(nil)
			for path, content := range tt.files {
				require.NoError(t, afero.WriteFile(ctx.Fs, path, []byte(content), 0o600))
			}
			_, err := run(ctx, tt.args...)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

package cli

import (
	"bytes"
	stdContext "context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/common/version"
	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/context"
	lacisHttp "github.com/alexandreh2ag/lacis/http"
	mockService "github.com/alexandreh2ag/lacis/mocks/service"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const validYAML = `
listen:
  address: 127.0.0.1:0
database:
  dsn: postgres://lacis@db/lacis
auth:
  session:
    secret: 0123456789abcdef0123456789abcdef
`

func run(ctx *context.Context, args ...string) (string, error) {
	return runCmd(GetRootCmd(ctx), args...)
}

func runCmd(cmd *cobra.Command, args ...string) (string, error) {
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestVersionCmd(t *testing.T) {
	out, err := run(context.TestContext(nil), "version")
	require.NoError(t, err)
	assert.Equal(t, version.GetFormattedVersion()+"\n", out)
}

func TestValidateCmd(t *testing.T) {
	logs := &bytes.Buffer{}
	ctx := context.TestContext(logs)
	require.NoError(t, afero.WriteFile(ctx.Fs, "/etc/lacis/server.yml", []byte(validYAML), 0o600))
	require.NoError(t, afero.WriteFile(ctx.Fs, "/etc/lacis/server.yaml", []byte(validYAML+"log: {format: json}"), 0o600))

	out, err := run(ctx, "validate", "-l", "debug")
	require.NoError(t, err)
	assert.Contains(t, out, "configuration is valid")
	assert.Equal(t, slog.LevelDebug, ctx.LogLevel.Level())
	assert.Equal(t, "json", ctx.Config.Log.Format)
	assert.Contains(t, logs.String(), `"msg":"config file ignored, another one has priority"`)
	assert.Contains(t, logs.String(), `"msg":"using config file"`)
}

func TestValidateCmd_EnvironmentOnly(t *testing.T) {
	t.Setenv("LACIS_DATABASE_DSN", "postgres://lacis@db/lacis")
	t.Setenv("LACIS_AUTH_SESSION_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("LACIS_LOG_LEVEL", "debug")
	logs := &bytes.Buffer{}
	ctx := context.TestContext(logs)

	_, err := run(ctx, "validate")
	require.NoError(t, err)
	assert.Equal(t, slog.LevelDebug, ctx.LogLevel.Level())
	assert.Contains(t, logs.String(), "no config file found")
}

func TestCommands_Errors(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		args  []string
		want  string
	}{
		{name: "validate invalid", args: []string{"validate"}, want: "Config.Database.DSN"},
		{name: "start invalid", args: []string{"start"}, want: "invalid configuration"},
		{name: "invalid log level", files: map[string]string{"/etc/lacis/server.yaml": validYAML}, args: []string{"validate", "-l", "loud"}, want: `invalid log level "loud"`},
		{name: "missing explicit file", args: []string{"validate", "-c", "/missing.yml"}, want: "config file /missing.yml"},
		{name: "unreadable file", files: map[string]string{"/etc/lacis/server.yaml": "listen: [x"}, args: []string{"validate"}, want: "read config"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.TestContext(nil)
			for p, c := range tt.files {
				require.NoError(t, afero.WriteFile(ctx.Fs, p, []byte(c), 0o600))
			}
			_, err := run(ctx, tt.args...)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

// withServices makes start use services, and reports whether they were
// released.
func withServices(t *testing.T, services *lacisHttp.Services, err error) *bool {
	released := false
	previous := NewServices
	NewServices = func(stdContext.Context, *context.Context) (*lacisHttp.Services, func(), error) {
		return services, func() { released = true }, err
	}
	t.Cleanup(func() { NewServices = previous })
	return &released
}

func TestStartCmd(t *testing.T) {
	released := withServices(t, &lacisHttp.Services{}, nil)
	ctx := context.TestContext(nil)
	require.NoError(t, afero.WriteFile(ctx.Fs, "/etc/lacis/server.yaml", []byte(strings.Replace(validYAML, "address: 127.0.0.1:0", "address: 127.0.0.1:0\n  base_path: /lacis", 1)), 0o600))

	addrs := make(chan net.Addr, 1)
	cmd := GetRootCmd(ctx)
	for _, c := range cmd.Commands() {
		if c.Name() == "start" {
			c.RunE = GetStartRunFn(ctx, func(a net.Addr) { addrs <- a })
		}
	}
	errs := make(chan error, 1)
	go func() {
		_, err := runCmd(cmd, "start")
		errs <- err
	}()

	var addr net.Addr
	select {
	case addr = <-addrs:
	case err := <-errs:
		t.Fatalf("start failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start in time")
	}
	resp, err := http.Get("http://" + addr.String() + "/lacis/healthz")
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	ctx.Signal() <- syscall.SIGTERM
	select {
	case err := <-errs:
		assert.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("server did not stop")
	}
	assert.True(t, *released)
}

func TestStartCmd_ServicesFail(t *testing.T) {
	withServices(t, nil, errors.New("database down"))
	ctx := context.TestContext(nil)
	require.NoError(t, afero.WriteFile(ctx.Fs, "/etc/lacis/server.yaml", []byte(validYAML), 0o600))
	_, err := run(ctx, "start")
	assert.ErrorContains(t, err, "database down")
}

func TestNewServices_Errors(t *testing.T) {
	tests := []struct {
		name    string
		migrate bool
		want    string
	}{
		{name: "migration fails", migrate: true, want: "connect"},
		{name: "open fails", want: "open database"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.TestContext(nil)
			ctx.Config.Database.DSN = "postgres://nobody@127.0.0.1:1/none?connect_timeout=1"
			ctx.Config.Database.MigrateOnStart = tt.migrate
			_, _, err := NewServices(t.Context(), ctx)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestBootstrapAdmin(t *testing.T) {
	tests := []struct {
		name     string
		disabled bool
		username string
		created  bool
		err      error
		wantLog  bool
		wantErr  string
	}{
		{name: "no username"},
		{name: "local auth disabled", disabled: true, username: "admin"},
		{name: "created", username: "admin", created: true, wantLog: true},
		{name: "already there", username: "admin"},
		{name: "fails", username: "admin", err: errors.New("database down"), wantErr: "bootstrap admin: database down"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := &bytes.Buffer{}
			ctx := context.TestContext(logs)
			ctx.Config.Auth.Local = config.LocalConfig{Enabled: !tt.disabled, BootstrapAdminUsername: tt.username, BootstrapAdminPassword: "s3cret-password"}
			users := mockService.NewMockUserService(gomock.NewController(t))
			if tt.username != "" && !tt.disabled {
				users.EXPECT().Bootstrap(gomock.Any(), tt.username, "s3cret-password").Return(tt.created, tt.err)
			}
			err := bootstrapAdmin(t.Context(), ctx, users)
			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantLog, strings.Contains(logs.String(), "bootstrap admin created"))
		})
	}
}

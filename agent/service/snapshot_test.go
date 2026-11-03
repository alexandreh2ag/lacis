package service

import (
	stdContext "context"
	"runtime"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/agent/context"
	mockClock "github.com/alexandreh2ag/lacis/agent/mocks/common/clock"
	"github.com/alexandreh2ag/lacis/common/labels"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestHostname(t *testing.T) {
	tests := []struct {
		name     string
		override string
		file     string
		want     string
		wantErr  bool
	}{
		{name: "from proc", file: "acme-site-prod-1\n", want: "acme-site-prod-1"},
		{name: "override", override: "custom", file: "machine\n", want: "custom"},
		{name: "missing file", wantErr: true},
		{name: "empty file", file: "\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.TestContext(nil)
			ctx.Config.Hostname = tt.override
			if tt.file != "" {
				require.NoError(t, afero.WriteFile(ctx.Fs, HostnameFile, []byte(tt.file), 0o644))
			}
			got, err := Hostname(ctx)
			if tt.wantErr {
				assert.ErrorIs(t, err, ErrNoHostname)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCollect(t *testing.T) {
	ctx := context.TestContext(nil)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.FixedZone("CEST", 2*3600))
	clock := mockClock.NewMockClock(gomock.NewController(t))
	clock.EXPECT().Now().Return(now)
	clock.EXPECT().Now().Return(now.Add(1500 * time.Millisecond)).AnyTimes()
	ctx.Clock = clock
	ctx.Config.Labels = labels.Labels{Namespace: "acme", Project: "site", Env: "prod"}
	require.NoError(t, afero.WriteFile(ctx.Fs, HostnameFile, []byte("acme-site-prod-1\n"), 0o644))
	require.NoError(t, afero.WriteFile(ctx.Fs, "/etc/os-release", []byte("ID=debian\nVERSION_ID=12\n"), 0o644))

	s, err := Collect(t.Context(), ctx)
	require.NoError(t, err)

	assert.Equal(t, snapshot.SchemaVersion, s.SchemaVersion)
	assert.Equal(t, "acme", s.Namespace)
	assert.Equal(t, "acme-site-prod-1", s.Host.Hostname)
	assert.Equal(t, "debian", s.Host.OS.ID, "filled by the host collector")
	assert.Equal(t, now.UTC(), s.CollectedAt)
	assert.Equal(t, int64(1500), s.DurationMs)
	assert.Equal(t, runtime.GOOS, s.Agent.OS)
	var names []string
	for _, r := range s.Collectors {
		names = append(names, r.Name)
	}
	assert.Equal(t, []string{"apps", "docker", "envfiles", "host", "runtimes", "services", "webservers"}, names, "every registered collector runs, sorted by name")
	assert.NoError(t, s.Validate())
}

func TestCollect_Errors(t *testing.T) {
	t.Run("no hostname", func(t *testing.T) {
		_, err := Collect(t.Context(), context.TestContext(nil))
		assert.ErrorIs(t, err, ErrNoHostname)
	})
	t.Run("unreadable os-release", func(t *testing.T) {
		ctx := context.TestContext(nil)
		require.NoError(t, afero.WriteFile(ctx.Fs, HostnameFile, []byte("h\n"), 0o644))
		ctx.Fs = failOpenFs{Fs: ctx.Fs, path: "/etc/os-release"}
		_, err := Collect(t.Context(), ctx)
		assert.ErrorContains(t, err, "read /etc/os-release")
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx := context.TestContext(nil)
		require.NoError(t, afero.WriteFile(ctx.Fs, HostnameFile, []byte("h\n"), 0o644))
		stdCtx, cancel := stdContext.WithCancel(t.Context())
		cancel()
		_, err := Collect(stdCtx, ctx)
		assert.ErrorIs(t, err, stdContext.Canceled)
	})
}

// failOpenFs fails to open path.
type failOpenFs struct {
	afero.Fs
	path string
}

func (f failOpenFs) Open(name string) (afero.File, error) {
	if name == f.path {
		return nil, assert.AnError
	}
	return f.Fs.Open(name)
}

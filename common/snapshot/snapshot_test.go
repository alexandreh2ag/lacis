package snapshot

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/common/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadFixture(t *testing.T) *Snapshot {
	t.Helper()
	b, err := os.ReadFile("testdata/snapshot.json")
	require.NoError(t, err)
	s := &Snapshot{}
	require.NoError(t, json.Unmarshal(b, s))
	return s
}

func TestFixture_Decode(t *testing.T) {
	s := loadFixture(t)

	assert.Equal(t, 1, s.SchemaVersion)
	assert.Equal(t, "acme", s.Namespace)
	assert.Equal(t, labels.Labels{Namespace: "acme", Project: "client", Env: "staging", Index: "1"}, s.Host.Labels)
	require.Len(t, s.Apps, 1)
	app := s.Apps[0]
	assert.Equal(t, AppRoleWeb, app.Role)
	assert.Equal(t, CodeSourceDeployInfo, app.Code.Source)
	require.Len(t, app.Dependencies, 4)
	assert.Equal(t, &Target{Kind: "name", Value: "redis.internal", Resolved: []string{"10.0.1.21"}, Resolver: ResolverHosts}, app.Dependencies[1].Target)
	assert.True(t, app.Dependencies[3].Unparsed)
	assert.Equal(t, CollectorStatusError, s.Collectors[2].Status)
	assert.NoError(t, s.Validate())
}

func TestNew(t *testing.T) {
	l := labels.Labels{Namespace: "acme", Project: "site", Env: "prod"}
	s := New(AgentInfo{Version: "1.0.0", OS: "linux", Arch: "amd64"}, l, "acme-site-prod-1")
	s.CollectedAt = time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)

	assert.Equal(t, SchemaVersion, s.SchemaVersion)
	assert.Equal(t, "acme", s.Namespace)
	assert.NoError(t, s.Validate())

	b, err := json.Marshal(s)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"apps":[]`)
	assert.Contains(t, string(b), `"collector_errors":[]`)
}

func TestIsAccepted(t *testing.T) {
	assert.True(t, IsAccepted(SchemaVersion))
	assert.False(t, IsAccepted(0))
	assert.False(t, IsAccepted(SchemaVersion+1))
}

func TestValidate_Errors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(s *Snapshot)
		wantErr error
		want    string
	}{
		{name: "schema version", mutate: func(s *Snapshot) { s.SchemaVersion = 99 }, wantErr: ErrSchemaVersion, want: "99"},
		{name: "collected_at", mutate: func(s *Snapshot) { s.CollectedAt = time.Time{} }, wantErr: ErrInvalid, want: "collected_at"},
		{name: "hostname", mutate: func(s *Snapshot) { s.Host.Hostname = "" }, wantErr: ErrInvalid, want: "host.hostname"},
		{name: "labels", mutate: func(s *Snapshot) { s.Host.Labels.Env = "" }, wantErr: ErrInvalid, want: "env: label is required"},
		{name: "namespace mismatch", mutate: func(s *Snapshot) { s.Namespace = "other" }, wantErr: ErrInvalid, want: `namespace "other" differs`},
		{name: "app path", mutate: func(s *Snapshot) { s.Apps[0].Path = "" }, wantErr: ErrInvalid, want: "apps[0].path"},
		{name: "app role", mutate: func(s *Snapshot) { s.Apps[0].Role = "cron" }, wantErr: ErrInvalid, want: `apps[0].role "cron"`},
		{name: "launcher role", mutate: func(s *Snapshot) { s.Apps[0].Launchers = []Launcher{{Type: "supervisor", Name: "x"}} }, wantErr: ErrInvalid, want: `apps[0].launchers[0].role ""`},
		{name: "dependency key", mutate: func(s *Snapshot) { s.Apps[0].Dependencies[0].Key = "" }, wantErr: ErrInvalid, want: "dependencies[0].key"},
		{name: "dependency target", mutate: func(s *Snapshot) { s.Apps[0].Dependencies[0].Target = nil }, wantErr: ErrInvalid, want: "dependencies[0].target is required"},
		{name: "dependency kind", mutate: func(s *Snapshot) { s.Apps[0].Dependencies[0].Target.Kind = "url" }, wantErr: ErrInvalid, want: `target.kind "url"`},
		{name: "dependency value", mutate: func(s *Snapshot) { s.Apps[0].Dependencies[0].Target.Value = "" }, wantErr: ErrInvalid, want: "target.value"},
		{name: "collector status", mutate: func(s *Snapshot) { s.Collectors[0].Status = "done" }, wantErr: ErrInvalid, want: `collectors[0].status "done"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := loadFixture(t)
			tt.mutate(s)
			err := s.Validate()
			assert.ErrorIs(t, err, tt.wantErr)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestHash(t *testing.T) {
	s := loadFixture(t)
	ref, err := s.Hash()
	require.NoError(t, err)
	assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, ref)

	t.Run("ignores volatile values", func(t *testing.T) {
		v := loadFixture(t)
		v.CollectedAt = v.CollectedAt.Add(time.Hour)
		v.DurationMs = 1
		v.Collectors[0].DurationMs = 999
		h, err := v.Hash()
		require.NoError(t, err)
		assert.Equal(t, ref, h)
	})

	t.Run("does not modify the snapshot", func(t *testing.T) {
		v := loadFixture(t)
		_, err := v.Hash()
		require.NoError(t, err)
		assert.Equal(t, loadFixture(t), v)
	})

	t.Run("changes with content", func(t *testing.T) {
		v := loadFixture(t)
		v.Runtimes[0].Version = "8.2.25"
		h, err := v.Hash()
		require.NoError(t, err)
		assert.NotEqual(t, ref, h)
	})

	t.Run("stable across a JSON round trip", func(t *testing.T) {
		b, err := json.Marshal(s)
		require.NoError(t, err)
		v := &Snapshot{}
		require.NoError(t, json.Unmarshal(b, v))
		h, err := v.Hash()
		require.NoError(t, err)
		assert.Equal(t, ref, h)
	})
}

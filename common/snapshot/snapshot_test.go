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
	require.Len(t, s.Containers, 2)
	assert.Equal(t, DockerContainer{ID: "3f2a9c1b7d4e", Name: "acme-minio-1", Image: "minio/minio:RELEASE.2025-04-22T22-12-26Z",
		Version: "RELEASE.2025-04-22T22-12-26Z", State: "running", Ports: []string{"0.0.0.0:9000->9000/tcp"},
		Networks: []string{"acme_default"}, ComposeProject: "acme", ComposeService: "minio"}, s.Containers[0])
	assert.NoError(t, s.Validate())

	s.Apps[0].Listen = []string{"0.0.0.0:3000", "[::]:3000"}
	s.Webservers[0].Listen = []string{"0.0.0.0:80"}
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
	assert.Contains(t, string(b), `"containers":[]`)
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
		{name: "app listen", mutate: func(s *Snapshot) { s.Apps[0].Listen = []string{"0.0.0.0:3000", ":8080"} }, wantErr: ErrInvalid, want: `apps[0].listen[1] ":8080"`},
		{name: "app listen port", mutate: func(s *Snapshot) { s.Apps[0].Listen = []string{"[::]:http"} }, wantErr: ErrInvalid, want: `apps[0].listen[0]`},
		{name: "webserver listen", mutate: func(s *Snapshot) { s.Webservers[0].Listen = []string{"0.0.0.0:99999"} }, wantErr: ErrInvalid, want: `webservers[0].listen[0]`},
		{name: "dependency target", mutate: func(s *Snapshot) { s.Apps[0].Dependencies[0].Target = nil }, wantErr: ErrInvalid, want: "dependencies[0].target is required"},
		{name: "dependency kind", mutate: func(s *Snapshot) { s.Apps[0].Dependencies[0].Target.Kind = "url" }, wantErr: ErrInvalid, want: `target.kind "url"`},
		{name: "dependency value", mutate: func(s *Snapshot) { s.Apps[0].Dependencies[0].Target.Value = "" }, wantErr: ErrInvalid, want: "target.value"},
		{name: "collector status", mutate: func(s *Snapshot) { s.Collectors[0].Status = "done" }, wantErr: ErrInvalid, want: `collectors[0].status "done"`},
		{name: "container name", mutate: func(s *Snapshot) { s.Containers[1].Name = "" }, wantErr: ErrInvalid, want: "containers[1].name is required"},
		{name: "too many containers", mutate: func(s *Snapshot) {
			s.Containers = make([]DockerContainer, MaxContainers+1)
			for i := range s.Containers {
				s.Containers[i].Name = "c"
			}
		}, wantErr: ErrInvalid, want: "5001 containers, 5000 at most"},
		{name: "too many apps", mutate: func(s *Snapshot) {
			for len(s.Apps) <= MaxApps {
				s.Apps = append(s.Apps, s.Apps[0])
			}
		}, wantErr: ErrInvalid, want: "2001 apps, 2000 at most"},
		{name: "too many domains", mutate: func(s *Snapshot) {
			s.Webservers = append(s.Webservers, Webserver{Type: "nginx", Vhosts: []Vhost{{ServerNames: make([]string, MaxDomains+1)}}})
		}, wantErr: ErrInvalid, want: "domains served, 20000 at most"},
		{name: "too many regex domains", mutate: func(s *Snapshot) {
			names := make([]string, MaxRegexDomains+1)
			for i := range names {
				names[i] = "~^a"
			}
			s.Webservers = append(s.Webservers, Webserver{Type: "nginx", Vhosts: []Vhost{{ServerNames: names}}})
		}, wantErr: ErrInvalid, want: "regex domains served, 500 at most"},
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

func TestCountDomains(t *testing.T) {
	tests := []struct {
		name             string
		webservers       []Webserver
		domains, regexes int
	}{
		{name: "none"},
		{name: "vhosts", webservers: []Webserver{{Vhosts: []Vhost{{ServerNames: []string{"a.example", "~^b"}}, {ServerNames: []string{"c.example"}}}}},
			domains: 3, regexes: 1},
		{name: "traefik rules", webservers: []Webserver{{Routers: []Router{
			{Rule: "Host(`a.example`) || Host(`b.example`, `c.example`)"},
			{Rule: "HostRegexp(`^[a-z]+\\.example$`) && PathPrefix(`/api`)"},
			{Rule: "HostSNI(`*`)"},
		}}}, domains: 5, regexes: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Snapshot{Webservers: tt.webservers}
			domains, regexes := s.countDomains()
			assert.Equal(t, tt.domains, domains)
			assert.Equal(t, tt.regexes, regexes)
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
		v.CollectorErrors = append(v.CollectorErrors, CollectorError{Collector: "apps", Error: "timeout after 2m0s"})
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

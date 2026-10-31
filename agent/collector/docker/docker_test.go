package docker

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const containersJSON = `[
  {"Id": "aaaaaaaaaaaa1111", "Names": ["/pg"], "Image": "postgres:16.4-bookworm",
   "Ports": [{"IP": "127.0.0.1", "PrivatePort": 5432, "PublicPort": 5433, "Type": "tcp"}],
   "HostConfig": {"NetworkMode": "app_default"}, "NetworkSettings": {"Networks": {"app_default": {"IPAddress": "172.18.0.2"}}}},
  {"Id": "bbbbbbbbbbbb2222", "Names": ["/cache"], "Image": "redis",
   "Ports": [{"IP": "0.0.0.0", "PrivatePort": 6379, "PublicPort": 6379, "Type": "tcp"}, {"IP": "::", "PrivatePort": 6379, "PublicPort": 6379, "Type": "tcp"}, {"PrivatePort": 6380, "Type": "tcp"}],
   "HostConfig": {"NetworkMode": "bridge"}, "NetworkSettings": {"Networks": {"bridge": {}, "app_default": {}}}},
  {"Id": "cccccccccccc3333", "Names": ["/db"], "Image": "docker.io/bitnami/mariadb@sha256:abc",
   "Labels": {"org.opencontainers.image.version": "11.4.2"},
   "HostConfig": {"NetworkMode": "host"}, "NetworkSettings": {"Networks": {}}},
  {"Id": "dddddddddddd4444", "Names": ["/search"], "Image": "docker.elastic.co/elasticsearch/elasticsearch:latest",
   "Ports": [{"IP": "0.0.0.0", "PrivatePort": 9300, "PublicPort": 9300, "Type": "udp"}]},
  {"Id": "eeeeeeeeeeee5555", "Names": ["/front"], "Image": "registry.example.com/acme/front:1.4.0"}
]`

const networksJSON = `[
  {"Id": "1111aaaa2222bbbb", "Name": "bridge", "Driver": "bridge", "Options": {"com.docker.network.bridge.name": "docker0"}},
  {"Id": "d09684cac390ffff", "Name": "app_default", "Driver": "bridge"},
  {"Id": "9999", "Name": "host", "Driver": "host"}
]`

// serve answers the Docker API on a unix socket and returns its path.
func serve(t *testing.T, handler http.HandlerFunc) string {
	dir, err := os.MkdirTemp("", "dk") // short: a socket path is limited
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "docker.sock")
	l, err := net.Listen("unix", socket)
	require.NoError(t, err)
	srv := &httptest.Server{Listener: l, Config: &http.Server{Handler: handler}}
	srv.Start()
	t.Cleanup(srv.Close)
	return socket
}

func api(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/networks":
			_, _ = w.Write([]byte(networksJSON))
		case "/containers/json":
			_, _ = w.Write([]byte(containersJSON))
		case "/containers/bbbbbbbbbbbb2222/json":
			_, _ = w.Write([]byte(`{"Config": {"Env": ["PATH=/usr/bin", "REDIS_VERSION=7.4.1"]}}`))
		case "/containers/cccccccccccc3333/json":
			_, _ = w.Write([]byte(`{"Config": {"Env": ["MARIADB_VERSION=1:11.4.2+maria~ubu2404"]}}`))
		case "/containers/aaaaaaaaaaaa1111/json":
			_, _ = w.Write([]byte(`{"Config": {"Env": ["PG_VERSION=17.2-1.pgdg120+1"]}}`))
		case "/containers/dddddddddddd4444/json":
			_, _ = w.Write([]byte(`{"Config": {"Env": []}}`))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}
}

func newContext(t *testing.T, socket string) *context.Context {
	ctx := context.TestContext(nil)
	ctx.Config.Collectors.Docker.Socket = socket
	require.NoError(t, afero.WriteFile(ctx.Fs, socket, nil, 0o600))
	return ctx
}

func TestCollect(t *testing.T) {
	ctx := newContext(t, serve(t, api(t)))
	out := &snapshot.Snapshot{
		Host: snapshot.Host{Interfaces: []snapshot.Interface{
			{Name: "eth0", IPs: []string{"10.0.1.12/24"}},
			{Name: "docker0", IPs: []string{"172.17.0.1/16"}},
			{Name: "br-d09684cac390", IPs: []string{"172.18.0.1/16"}},
			{Name: "br-0123456789ab", IPs: []string{"172.30.0.1/16"}},
			{Name: "veth1a2b3c"},
		}},
		Services: []snapshot.Service{{Type: "nginx"}},
	}
	c := &Collector{}
	require.True(t, c.Applicable(ctx, &system.System{}))
	require.NoError(t, c.Collect(t.Context(), ctx, &system.System{}, out))

	assert.Equal(t, []snapshot.Interface{
		{Name: "eth0", IPs: []string{"10.0.1.12/24"}},
		{Name: "br-0123456789ab", IPs: []string{"172.30.0.1/16"}},
	}, out.Host.Interfaces, "the bridges of the docker networks and the veth removed; another bridge kept")
	assert.Equal(t, []snapshot.Service{
		{Type: "nginx"},
		{Type: "postgresql", Version: "16.4", Instance: "pg", Listen: []string{"127.0.0.1:5433"},
			Container: &snapshot.Container{ID: "aaaaaaaaaaaa", Name: "pg", Image: "postgres:16.4-bookworm", Ports: []string{"127.0.0.1:5433->5432/tcp"}, Networks: []string{"app_default"}}},
		{Type: "redis", Version: "7.4.1", Instance: "cache", Listen: []string{"0.0.0.0:6379", "[::]:6379"},
			Container: &snapshot.Container{ID: "bbbbbbbbbbbb", Name: "cache", Image: "redis",
				Ports: []string{"0.0.0.0:6379->6379/tcp", "6380/tcp", "[::]:6379->6379/tcp"}, Networks: []string{"app_default", "bridge"}}},
		{Type: "mariadb", Version: "11.4.2", Instance: "db",
			Container: &snapshot.Container{ID: "cccccccccccc", Name: "db", Image: "docker.io/bitnami/mariadb@sha256:abc", Networks: []string{"host"}}},
		{Type: "elasticsearch", Instance: "search",
			Container: &snapshot.Container{ID: "dddddddddddd", Name: "search", Image: "docker.elastic.co/elasticsearch/elasticsearch:latest", Ports: []string{"0.0.0.0:9300->9300/udp"}}},
	}, out.Services, "the front image is no service; the version is unknown for the latest elasticsearch")
}

func TestCollect_Errors(t *testing.T) {
	t.Run("api errors", func(t *testing.T) {
		ctx := newContext(t, serve(t, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "permission denied", http.StatusForbidden)
		}))
		err := (&Collector{}).Collect(t.Context(), ctx, &system.System{}, &snapshot.Snapshot{})
		assert.ErrorContains(t, err, "GET /networks: 403 Forbidden")
		assert.ErrorContains(t, err, "GET /containers/json: 403 Forbidden")
	})
	t.Run("inspect fails", func(t *testing.T) {
		ctx := newContext(t, serve(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/networks":
				_, _ = w.Write([]byte(`[]`))
			case "/containers/json":
				_, _ = w.Write([]byte(`[{"Id": "x", "Image": "redis:latest"}]`))
			default:
				_, _ = w.Write([]byte(`{`))
			}
		}))
		out := &snapshot.Snapshot{}
		err := (&Collector{}).Collect(t.Context(), ctx, &system.System{}, out)
		assert.ErrorContains(t, err, "GET /containers/x/json")
		require.Len(t, out.Services, 1, "still reported")
		assert.Equal(t, "x", out.Services[0].Instance, "no name: the id")
	})
	t.Run("socket gone", func(t *testing.T) {
		ctx := newContext(t, filepath.Join(t.TempDir(), "gone.sock"))
		err := (&Collector{}).Collect(t.Context(), ctx, &system.System{}, &snapshot.Snapshot{})
		assert.Error(t, err)
	})
}

func TestApplicable(t *testing.T) {
	ctx := context.TestContext(nil)
	ctx.Config.Collectors.Docker.Socket = "/var/run/docker.sock"
	assert.False(t, (&Collector{}).Applicable(ctx, nil))
	ctx.Config.Collectors.Docker.Socket = ""
	assert.False(t, (&Collector{}).Applicable(ctx, nil))
	assert.Equal(t, Name, (&Collector{}).Name())
}

func TestImages(t *testing.T) {
	for image, want := range map[string][2]string{
		"postgres:16":                       {"postgresql", "16"},
		"postgis/postgis:15-3.4":            {"postgresql", "15"},
		"bitnami/redis:7.2.5-debian-12-r0":  {"redis", "7.2.5"},
		"quay.io/keycloak/keycloak:25.0.6":  {"keycloak", "25.0.6"},
		"clickhouse/clickhouse-server:24.8": {"clickhouse", "24.8"},
		"localhost:5000/mongo:v7.0":         {"mongodb", "7.0"},
		"rabbitmq:management":               {"rabbitmq", ""},
		"nginx:1.27":                        {"", "1.27"},
		"mysql@sha256:0123":                 {"mysql", ""},
	} {
		assert.Equal(t, want[0], serviceType(image), image)
		assert.Equal(t, want[1], tagVersion(image), image)
	}
}

func TestVersion(t *testing.T) {
	assert.Equal(t, "17.2", version("17", "17.2", ""), "the variables refine the tag")
	assert.Equal(t, "16.4", version("16.4", "17.2", ""), "the tag wins when they differ")
	assert.Equal(t, "7.4.1", version("", "7.4.1", "x"))
	assert.Equal(t, "11.4.2", version("", "", "11.4.2"))
	assert.Equal(t, "", version("", "", ""))
}

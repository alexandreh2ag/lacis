package webservers

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/fakefs"
	mockExec "github.com/alexandreh2ag/lacis/agent/mocks/exec"
	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// newContext returns a host running the given processes (pid → exe and
// cmdline), with the packages and mocked executor.
func newContext(t *testing.T, processes map[int][]string, parents map[int]int, packages string) (*context.Context, *mockExec.MockExecutor) {
	f := fakefs.New()
	for pid, args := range processes {
		dir := fmt.Sprintf("/proc/%d/", pid)
		f.Write(t, dir+"status", fmt.Sprintf("Name:\tx\nPPid:\t%d\n", parents[pid]))
		f.Write(t, dir+"cmdline", strings.Join(args[1:], "\x00")+"\x00")
		f.Link(dir+"exe", args[0])
	}
	f.Write(t, system.DpkgQueryCommand, "")
	f.Write(t, "/etc/os-release", "ID=debian\n")
	ctx := context.TestContext(nil)
	ctx.Fs = f
	e := mockExec.NewMockExecutor(gomock.NewController(t))
	e.EXPECT().Run(gomock.Any(), system.DpkgQueryCommand, gomock.Any()).Return([]byte(packages), nil).AnyTimes()
	ctx.Executor = e
	return ctx, e
}

func collect(t *testing.T, ctx *context.Context) (*snapshot.Snapshot, bool, error) {
	sys, err := system.Load(ctx)
	require.NoError(t, err)
	c := &Collector{}
	out := &snapshot.Snapshot{}
	if !c.Applicable(ctx, sys) {
		return out, false, nil
	}
	return out, true, c.Collect(t.Context(), ctx, sys, out)
}

func TestCollect_Nginx(t *testing.T) {
	dump, err := os.ReadFile("testdata/nginx-T.txt")
	require.NoError(t, err)
	ctx, e := newContext(t, map[int][]string{
		144: {"/usr/sbin/nginx", "nginx: master process /usr/sbin/nginx -g daemon on; master_process on;"},
		145: {"/usr/sbin/nginx", "nginx: worker process"},
	}, map[int]int{144: 1, 145: 144}, "ii \tnginx\t1.26.3-3+deb13u1\n")
	e.EXPECT().Run(gomock.Any(), "/usr/sbin/nginx", "-T").Return(dump, nil)

	out, applicable, err := collect(t, ctx)
	require.NoError(t, err)
	assert.True(t, applicable)
	assert.Equal(t, []snapshot.Webserver{{
		Type:    "nginx",
		Version: "1.26.3",
		Vhosts: []snapshot.Vhost{
			// conf.d is included before sites-enabled.
			{Listen: []string{"localhost:80"}, ConfigFile: "/etc/nginx/conf.d/status.conf"},
			{
				ServerNames: []string{"staging.client.fr", "www.staging.client.fr"},
				Listen:      []string{"0.0.0.0:443", "[::]:443"},
				Root:        "/var/www/client/current/public",
				Upstreams:   []string{"unix:/run/php/php8.2-fpm.sock"},
				ConfigFile:  "/etc/nginx/sites-enabled/client.conf",
			},
			{ServerNames: []string{"staging.client.fr"}, Listen: []string{"0.0.0.0:80"}, ConfigFile: "/etc/nginx/sites-enabled/client.conf"},
			{
				ServerNames: []string{"front.client.fr"},
				Listen:      []string{"127.0.0.1:8080"},
				Root:        "/var/www/front/current/dist",
				Upstreams:   []string{"http://127.0.0.1:8000", "http://front_nodes", "unix:/run/front.sock", "http://${backend}$request_uri", "http://127.0.0.1:3010"},
				ConfigFile:  "/etc/nginx/sites-enabled/front",
			},
			{Listen: []string{"0.0.0.0:8081"}, Upstreams: []string{"127.0.0.1:3031"}, ConfigFile: "/etc/nginx/sites-enabled/default"},
		},
		Upstreams: []snapshot.Upstream{{Name: "front_nodes", Servers: []string{"10.0.1.31:3000", "10.0.1.32:3000"}}},
	}}, out.Webservers)
}

func TestCollect_NginxCustomConfig(t *testing.T) {
	ctx, e := newContext(t, map[int][]string{
		10: {"/opt/nginx/sbin/nginx", "nginx: master process /opt/nginx/sbin/nginx -c /opt/nginx/conf/nginx.conf"},
	}, map[int]int{10: 1}, "")
	e.EXPECT().Run(gomock.Any(), "/opt/nginx/sbin/nginx", "-T", "-c", "/opt/nginx/conf/nginx.conf").
		Return([]byte("# configuration file /opt/nginx/conf/nginx.conf:\nhttp { server { listen 8000; } }\n"), nil)

	out, _, err := collect(t, ctx)
	require.NoError(t, err)
	assert.Equal(t, []snapshot.Webserver{{Type: "nginx", Vhosts: []snapshot.Vhost{{Listen: []string{"0.0.0.0:8000"}, ConfigFile: "/opt/nginx/conf/nginx.conf"}}}}, out.Webservers)
}

func TestCollect_Errors(t *testing.T) {
	tests := []struct {
		name string
		dump string
		err  error
		want string
	}{
		{name: "nginx -T fails", err: errors.New("exit status 1: [emerg] unknown directive"), want: "nginx: exit status 1"},
		{name: "no marker", dump: "http {}\n", want: "nginx: no configuration file"},
		{name: "broken config", dump: "# configuration file /etc/nginx/nginx.conf:\nhttp {\n", want: `nginx: /etc/nginx/nginx.conf: missing "}"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, e := newContext(t, map[int][]string{1: {"/usr/sbin/nginx", "nginx: master process /usr/sbin/nginx"}}, nil, "")
			e.EXPECT().Run(gomock.Any(), "/usr/sbin/nginx", "-T").Return([]byte(tt.dump), tt.err)
			out, _, err := collect(t, ctx)
			assert.ErrorContains(t, err, tt.want)
			require.Len(t, out.Webservers, 1, "still reported")
			assert.Equal(t, "nginx", out.Webservers[0].Type)
		})
	}
}

func TestApplicable(t *testing.T) {
	ctx, _ := newContext(t, map[int][]string{1: {"/sbin/init", "init"}}, nil, "")
	_, applicable, _ := collect(t, ctx)
	assert.False(t, applicable)

	ctx = context.TestContext(nil)
	ctx.Fs = fakefs.New()
	_, applicable, err := collect(t, ctx)
	assert.True(t, applicable, "processes unreadable: reported")
	assert.ErrorContains(t, err, "list processes")
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []directive
		wantErr string
	}{
		{
			name:    "quotes, escapes and comments",
			content: `a "b c" 'd\'e' f\ g; # comment` + "\nh { i ${j}k; }",
			want: []directive{
				{Name: "a", Args: []string{"b c", "d'e", `f\ g`}, File: "f"},
				{Name: "h", Block: []directive{{Name: "i", Args: []string{"${j}k"}, File: "f"}}, File: "f"},
			},
		},
		{name: "empty block", content: "events {}", want: []directive{{Name: "events", Block: []directive{}, File: "f"}}},
		{name: "unterminated quote", content: `a "b;`, wantErr: "unterminated quoted string"},
		{name: "unterminated directive", content: "a b", wantErr: `directive "a" not terminated`},
		{name: "directive closed by brace", content: "h { a b }", wantErr: `directive "a" not terminated`},
		{name: "stray brace", content: "}", wantErr: `unexpected "}"`},
		{name: "stray semicolon", content: ";", wantErr: `unexpected ";"`},
		{name: "missing brace", content: "h { a;", wantErr: `missing "}"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parse("f", tt.content)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseDump_Includes(t *testing.T) {
	t.Run("relative and missing", func(t *testing.T) {
		got, err := parseDump(strings.NewReader("# configuration file /etc/nginx/nginx.conf:\ninclude conf.d/*.conf;\ninclude /nowhere/*;\n# configuration file /etc/nginx/conf.d/a.conf:\na;\n"))
		require.NoError(t, err)
		assert.Equal(t, []directive{{Name: "a", File: "/etc/nginx/conf.d/a.conf"}}, got)
	})
	t.Run("loop", func(t *testing.T) {
		_, err := parseDump(strings.NewReader("# configuration file /etc/nginx/nginx.conf:\ninclude /etc/nginx/nginx.conf;\n"))
		assert.ErrorContains(t, err, "includes nested too deep")
	})
	t.Run("broken included file", func(t *testing.T) {
		_, err := parseDump(strings.NewReader("# configuration file /etc/nginx/nginx.conf:\nhttp { include x.conf; }\n# configuration file /etc/nginx/x.conf:\n}\n"))
		assert.ErrorContains(t, err, "/etc/nginx/x.conf")
	})
}

func TestNginxListen(t *testing.T) {
	for in, want := range map[string]string{
		"80": "0.0.0.0:80", "*:80": "0.0.0.0:80", "[::]:443": "[::]:443", "127.0.0.1:8080": "127.0.0.1:8080",
		"localhost": "localhost:80", "unix:/run/nginx.sock": "unix:/run/nginx.sock",
	} {
		assert.Equal(t, want, nginxListen(in), in)
	}
}

func TestPassTarget(t *testing.T) {
	tests := []struct{ name, target, want string }{
		{"fastcgi_pass", "127.0.0.1:9000", "127.0.0.1:9000"},
		{"proxy_pass", "https://api.client.fr/v1/", "https://api.client.fr"},
		{"proxy_pass", "backend", "backend"},
		{"proxy_pass", "http://unix:/run/a.sock", "unix:/run/a.sock"},
		{"proxy_pass", "http://%zz", "http://%zz"},
		{"proxy_pass", "https://127.0.0.1$request_uri", "https://127.0.0.1"},
		{"proxy_pass", "http://127.0.0.1:$port", "http://127.0.0.1:$port"},
		{"proxy_pass", "http://$backend/path", "http://$backend/path"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, passTarget(tt.name, tt.target), tt.target)
	}
}

func TestName(t *testing.T) {
	assert.Equal(t, Name, (&Collector{}).Name())
}

func TestSetVariables(t *testing.T) {
	directives, err := parse("f", `set $port 3000; set $dyn $host; location / { set ${port} 4000; set $api api.local; } set bad;`)
	require.NoError(t, err)
	vars := setVariables(directives)
	assert.Equal(t, map[string]string{"port": "3000", "api": "api.local"}, vars)
	assert.Equal(t, "http://127.0.0.1:3000/x$uri", expand("http://127.0.0.1:${port}/x$uri", vars))
}

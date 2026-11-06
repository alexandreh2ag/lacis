package webservers

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/exec"
	"github.com/alexandreh2ag/lacis/agent/fakefs"
	"github.com/alexandreh2ag/lacis/agent/fsutil"
	mockExec "github.com/alexandreh2ag/lacis/agent/mocks/exec"
	"github.com/alexandreh2ag/lacis/agent/netaddr"
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
		f.Write(t, dir+"status", fmt.Sprintf("Name:\tx\nPPid:\t%d\nUid:\t0\t0\t0\t0\nGid:\t0\t0\t0\t0\n", parents[pid]))
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

// dumpFiles splits a dump of nginx -T into its files: each one starts
// with "# configuration file <path>:".
func dumpFiles(t *testing.T, dump string) map[string]string {
	t.Helper()
	files := map[string]string{}
	var current string
	for line := range strings.Lines(dump) {
		if name, ok := strings.CutPrefix(strings.TrimSuffix(line, "\n"), "# configuration file "); ok && strings.HasSuffix(name, ":") {
			current = strings.TrimSuffix(name, ":")
			files[current] = ""
			continue
		}
		require.NotEmpty(t, current, "content before the first file")
		files[current] += line
	}
	return files
}

func TestCollect_Nginx(t *testing.T) {
	dump, err := os.ReadFile("testdata/nginx-T.txt")
	require.NoError(t, err)
	ctx, _ := newContext(t, map[int][]string{
		144: {"/usr/sbin/nginx", "nginx: master process /usr/sbin/nginx -g daemon on; master_process on;"},
		145: {"/usr/sbin/nginx", "nginx: worker process"},
	}, map[int]int{144: 1, 145: 144}, "ii \tnginx\t1.26.3-3+deb13u1\n")
	f := ctx.Fs.(*fakefs.FS)
	f.Files(t, dumpFiles(t, string(dump)))
	// Directories matched by an include are skipped.
	require.NoError(t, f.MkdirAll("/etc/nginx/sites-enabled/old", 0o755))

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
				ServerNames: []string{"staging.client.example.com", "www.staging.client.example.com"},
				Listen:      []string{"0.0.0.0:443", "[::]:443"},
				Root:        "/var/www/client/current/public",
				Upstreams:   []string{"unix:/run/php/php8.2-fpm.sock"},
				Locations:   []snapshot.Location{{Upstreams: []string{"unix:/run/php/php8.2-fpm.sock"}}},
				ConfigFile:  "/etc/nginx/sites-enabled/client.conf",
			},
			{ServerNames: []string{"staging.client.example.com"}, Listen: []string{"0.0.0.0:80"}, ConfigFile: "/etc/nginx/sites-enabled/client.conf"},
			{Listen: []string{"0.0.0.0:8081"}, Upstreams: []string{"127.0.0.1:3031"},
				Locations: []snapshot.Location{{Upstreams: []string{"127.0.0.1:3031"}}}, ConfigFile: "/etc/nginx/sites-enabled/default"},
			{
				ServerNames: []string{"front.client.example.com"},
				Listen:      []string{"127.0.0.1:8080"},
				Root:        "/var/www/front/current/dist",
				Upstreams:   []string{"http://127.0.0.1:8000", "http://front_nodes", "unix:/run/front.sock", "http://${backend}$request_uri", "http://127.0.0.1:3010"},
				Locations: []snapshot.Location{
					{Path: "/api/", Upstreams: []string{"http://127.0.0.1:8000"}},
					{Upstreams: []string{"http://front_nodes"}},
					{Path: "/ws", Upstreams: []string{"unix:/run/front.sock"}},
					{Path: "/dyn", Upstreams: []string{"http://${backend}$request_uri"}},
					{Path: "/simu", Upstreams: []string{"http://127.0.0.1:3010"}},
				},
				ConfigFile: "/etc/nginx/sites-enabled/front",
			},
		},
		Upstreams: []snapshot.Upstream{{Name: "front_nodes", Servers: []string{"10.0.1.31:3000", "10.0.1.32:3000"}}},
	}}, out.Webservers)
}

func TestLocationPath(t *testing.T) {
	tests := map[string][]string{
		"/api/":          {"/api/"},
		"":               {"/"},
		"=/health":       {"=", "/health"},
		"~^/index\\.php": {"~", `^/index\.php`},
		"~(?i)\\.jpg$":   {"~*", `\.jpg$`},
		"/static":        {"^~", "/static"},
		"=/glued":        {"=/glued"},
	}
	for want, args := range tests {
		assert.Equal(t, want, locationPath(args), "%v", args)
	}
	assert.Equal(t, "", locationPath([]string{"@app"}), "a named location serves any path")
}

func TestPassedPath(t *testing.T) {
	assert.Equal(t, "", passedPath(`~\.php$`, true), "FastCGI under a regexp: any path")
	assert.Equal(t, "/legacy/", passedPath("/legacy/", true), "FastCGI under a prefix keeps it")
	assert.Equal(t, `~^/(a|b)`, passedPath(`~^/(a|b)`, false))
}

func TestNginxMainConfig(t *testing.T) {
	tests := []struct {
		name  string
		exe   string
		title string
		files []string
		want  string
	}{
		{name: "-c", exe: "/opt/nginx/sbin/nginx", title: "nginx: master process /opt/nginx/sbin/nginx -c /opt/nginx/conf/nginx.conf", want: "/opt/nginx/conf/nginx.conf"},
		{name: "relative -c", exe: "/opt/nginx/sbin/nginx", title: "nginx: master process /opt/nginx/sbin/nginx -c conf/main.conf", want: "/opt/nginx/conf/main.conf"},
		{name: "relative -c, -p", exe: "/usr/sbin/nginx", title: "nginx: master process nginx -p /srv/ng/ -c conf/main.conf", want: "/srv/ng/conf/main.conf"},
		{name: "-p", exe: "/usr/sbin/nginx", title: "nginx: master process nginx -p /srv/ng", want: "/srv/ng/conf/nginx.conf"},
		{name: "default", exe: "/usr/sbin/nginx", title: "nginx: master process /usr/sbin/nginx", files: []string{NginxConfig}, want: NginxConfig},
		{name: "prefix of the binary", exe: "/usr/local/openresty/nginx/sbin/nginx", title: "nginx: master process openresty", files: []string{"/usr/local/openresty/nginx/conf/nginx.conf"}, want: "/usr/local/openresty/nginx/conf/nginx.conf"},
		{name: "nothing found", exe: "/opt/x/nginx", title: "nginx: master process", want: NginxConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := fakefs.New()
			for _, file := range tt.files {
				f.Write(t, file, "")
			}
			assert.Equal(t, tt.want, nginxMainConfig(f, system.Process{Exe: tt.exe, Cmdline: []string{tt.title}}))
		})
	}
}

func TestCollect_NginxUser(t *testing.T) {
	// An nginx run by a user: its files are read as that user.
	ctx, e := newContext(t, map[int][]string{
		10: {"/home/bob/nginx/sbin/nginx", "nginx: master process /home/bob/nginx/sbin/nginx -c /home/bob/nginx/nginx.conf"},
	}, map[int]int{10: 1}, "")
	ctx.Fs.(*fakefs.FS).Write(t, fmt.Sprintf("/proc/%d/status", 10), "Name:\tnginx\nPPid:\t1\nUid:\t1000\t1000\t1000\t1000\nGid:\t1000\t1000\t1000\t1000\nGroups:\t33\n")
	e.EXPECT().ReadFileAs(gomock.Any(), exec.Credential{UID: 1000, GID: 1000, Groups: []uint32{33}}, "/home/bob/nginx/nginx.conf", fsutil.MaxConfigSize).
		Return([]byte("http { server { listen 8000; } }\n"), nil)

	out, _, err := collect(t, ctx)
	require.NoError(t, err)
	assert.Equal(t, []snapshot.Webserver{{Type: "nginx", Vhosts: []snapshot.Vhost{{Listen: []string{"0.0.0.0:8000"}, ConfigFile: "/home/bob/nginx/nginx.conf"}}}}, out.Webservers)
}

func TestCollect_Errors(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{name: "main file missing", want: "nginx: open /etc/nginx/nginx.conf"},
		{name: "broken config", files: map[string]string{NginxConfig: "http {\n"}, want: `nginx: /etc/nginx/nginx.conf: missing "}"`},
		{name: "broken included file", files: map[string]string{NginxConfig: "http { include x.conf; }\n", "/etc/nginx/x.conf": "}\n"}, want: "/etc/nginx/x.conf"},
		{name: "include loop", files: map[string]string{NginxConfig: "include /etc/nginx/nginx.conf;\n"}, want: "includes nested too deep"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := newContext(t, map[int][]string{1: {"/usr/sbin/nginx", "nginx: master process /usr/sbin/nginx"}}, nil, "")
			ctx.Fs.(*fakefs.FS).Files(t, tt.files)
			out, _, err := collect(t, ctx)
			assert.ErrorContains(t, err, tt.want)
			require.Len(t, out.Webservers, 1, "still reported")
			assert.Equal(t, "nginx", out.Webservers[0].Type)
		})
	}
}

func TestCollect_NginxIncludes(t *testing.T) {
	ctx, _ := newContext(t, map[int][]string{1: {"/usr/sbin/nginx", "nginx: master process /usr/sbin/nginx"}}, nil, "")
	f := ctx.Fs.(*fakefs.FS)
	f.Files(t, map[string]string{
		NginxConfig:                "http { include conf.d/*.conf; include /nowhere/*; include missing.conf; include big.conf; }\n",
		"/etc/nginx/conf.d/a.conf": "server { listen 81; }\n",
		"/etc/nginx/big.conf":      strings.Repeat("#", int(fsutil.MaxConfigSize)+1),
	})
	f.Link("/etc/nginx/conf.d/b.conf", "/dev/zero") // not followed by fakefs: missing

	out, _, err := collect(t, ctx)
	assert.ErrorContains(t, err, "include /etc/nginx/missing.conf not found")
	assert.ErrorIs(t, err, fsutil.ErrTooLarge)
	assert.Equal(t, []snapshot.Vhost{{Listen: []string{"0.0.0.0:81"}, ConfigFile: "/etc/nginx/conf.d/a.conf"}}, out.Webservers[0].Vhosts)
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

func TestNginxListen(t *testing.T) {
	for in, want := range map[string]string{
		"80": "0.0.0.0:80", "*:80": "0.0.0.0:80", "[::]:443": "[::]:443", "127.0.0.1:8080": "127.0.0.1:8080",
		"localhost": "localhost:80", "unix:/run/nginx.sock": "unix:/run/nginx.sock",
	} {
		assert.Equal(t, want, netaddr.Listen(in, "80"), in)
	}
}

func TestPassTarget(t *testing.T) {
	tests := []struct{ name, target, want string }{
		{"fastcgi_pass", "127.0.0.1:9000", "127.0.0.1:9000"},
		{"proxy_pass", "https://api.client.example.com/v1/", "https://api.client.example.com"},
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

package webservers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexandreh2ag/lacis/agent/fakefs"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var apacheFiles = map[string]string{
	"/etc/apache2/apache2.conf": `# Debian layout
ServerRoot "/etc/apache2"
<IfModule mpm_prefork_module>
	StartServers 5
</IfModule>
IncludeOptional mods-enabled/*.load
Include ports.conf
IncludeOptional conf-enabled/*.conf
IncludeOptional sites-enabled/*.conf
Include missing.conf
`,
	"/etc/apache2/ports.conf":                "Listen 80\n<IfModule ssl_module>\n\tListen 443\n</IfModule>\n",
	"/etc/apache2/mods-enabled/proxy.load":   "LoadModule proxy_module /usr/lib/apache2/modules/mod_proxy.so\n",
	"/etc/apache2/conf-enabled/charset.conf": "AddDefaultCharset UTF-8\n",
	"/etc/apache2/sites-enabled/000-default.conf": `<VirtualHost *:80>
	DocumentRoot /var/www/html
</VirtualHost>
`,
	"/etc/apache2/sites-enabled/client.conf": `<VirtualHost *:443 [::]:443>
	ServerName https://client.example.fr:443
	ServerAlias www.client.example.fr \
		admin.client.example.fr
	DocumentRoot "/var/www/client/current/public/"
	<FilesMatch "\.php$">
		SetHandler "proxy:unix:/run/php/php8.2-fpm.sock|fcgi://localhost"
	</FilesMatch>
	<Location /api>
		ProxyPass http://127.0.0.1:3000/api retry=0
	</Location>
	ProxyPass /static !
	ProxyPassMatch ^/legacy/(.*\.php)$ fcgi://127.0.0.1:9000/var/www/legacy/$1
	ProxyPass /ws unix:/run/ws.sock|ws://localhost/
</VirtualHost>
`,
	"/etc/apache2/sites-enabled/disabled.conf.bak": "<VirtualHost *:81>\n</VirtualHost>\n",
}

func TestCollect_Apache(t *testing.T) {
	ctx, _ := newContext(t, map[int][]string{
		10: {"/usr/sbin/apache2", "/usr/sbin/apache2", "-k", "start"},
		11: {"/usr/sbin/apache2", "/usr/sbin/apache2", "-k", "start"},
	}, map[int]int{10: 1, 11: 10}, "ii \tapache2\t2.4.62-1~deb12u2\n")
	ctx.Fs.(*fakefs.FS).Files(t, apacheFiles)

	out, applicable, err := collect(t, ctx)
	assert.True(t, applicable)
	assert.EqualError(t, err, "apache: /etc/apache2/apache2.conf: include /etc/apache2/missing.conf not found")
	assert.Equal(t, []snapshot.Webserver{{
		Type:    "apache",
		Version: "2.4.62",
		Vhosts: []snapshot.Vhost{
			{Listen: []string{"0.0.0.0:80"}, Root: "/var/www/html", ConfigFile: "/etc/apache2/sites-enabled/000-default.conf"},
			{
				ServerNames: []string{"client.example.fr", "www.client.example.fr", "admin.client.example.fr"},
				Listen:      []string{"0.0.0.0:443", "[::]:443"},
				Root:        "/var/www/client/current/public",
				Upstreams:   []string{"unix:/run/php/php8.2-fpm.sock", "http://127.0.0.1:3000", "127.0.0.1:9000", "unix:/run/ws.sock"},
				ConfigFile:  "/etc/apache2/sites-enabled/client.conf",
			},
		},
	}}, out.Webservers)
}

func TestCollect_ApacheRHEL(t *testing.T) {
	ctx, _ := newContext(t, map[int][]string{
		10: {"/usr/sbin/httpd", "/usr/sbin/httpd", "-DFOREGROUND", "-d", "/opt/httpd", "-f", "conf/main.conf"},
	}, map[int]int{10: 1}, "")
	ctx.Fs.(*fakefs.FS).Files(t, map[string]string{
		"/opt/httpd/conf/main.conf": "Include conf.d\n",
		"/opt/httpd/conf.d/a.conf":  "<VirtualHost 10.0.1.5>\nServerName a.local\n</VirtualHost>\n",
	})
	require.NoError(t, ctx.Fs.MkdirAll("/opt/httpd/conf.d/sub", 0o755))

	out, _, err := collect(t, ctx)
	require.NoError(t, err)
	assert.Equal(t, []snapshot.Vhost{{ServerNames: []string{"a.local"}, Listen: []string{"10.0.1.5:80"}, ConfigFile: "/opt/httpd/conf.d/a.conf"}}, out.Webservers[0].Vhosts)
}

func TestApache_Errors(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{name: "no main file", want: "apache: open /etc/apache2/apache2.conf"},
		{name: "unclosed section", files: map[string]string{"/etc/apache2/apache2.conf": "<VirtualHost *:80>\n"}, want: "missing </VirtualHost>"},
		{name: "stray close", files: map[string]string{"/etc/apache2/apache2.conf": "</VirtualHost>\n"}, want: `unexpected "</VirtualHost>"`},
		{name: "include loop", files: map[string]string{"/etc/apache2/apache2.conf": "Include apache2.conf\n"}, want: "includes nested too deep"},
		{name: "broken include", files: map[string]string{"/etc/apache2/apache2.conf": "Include a.conf\n", "/etc/apache2/a.conf": "</X>\n"}, want: "/etc/apache2/a.conf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := newContext(t, map[int][]string{10: {"/usr/sbin/apache2", "/usr/sbin/apache2"}}, nil, "")
			ctx.Fs.(*fakefs.FS).Files(t, tt.files)
			_, _, err := collect(t, ctx)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestApacheHelpers(t *testing.T) {
	assert.Equal(t, []string{"a", "b c", "d", "e"}, apacheFields(`a "b c"	d  e`))
	assert.Equal(t, []string{"a", "unterminated"}, apacheFields(`a "unterminated`))
	for in, want := range map[string]string{"*": "0.0.0.0:80", "_default_:443": "0.0.0.0:443", "[::1]": "[::1]:80", "10.0.0.1:8080": "10.0.0.1:8080"} {
		assert.Equal(t, want, apacheListen(in), in)
	}
}

const haproxyCfg = `global
	log /dev/log local0
defaults
	mode http # comment
	timeout connect 5s
frontend www
	bind *:80
	bind :443,ipv6@:::443 ssl crt /etc/haproxy/certs
	acl is_api hdr(host) -i api.client.fr
	use_backend api if is_api
	use_backend api if { path_beg /v2 }
	default_backend web
backend web
	balance roundrobin
	server web1 10.0.1.31:80 check
	server web2 10.0.1.32:80 check
backend api
	server api1 10.0.1.41:8000
listen galera
	bind 127.0.0.1:3306
	mode tcp
	server db1 10.0.1.51:3306 check
	server db2 10.0.1.52:3306 check backup
frontend stats
	bind /run/haproxy/admin.sock
	bind unix@/run/haproxy/other.sock
`

func TestCollect_HAProxy(t *testing.T) {
	ctx, _ := newContext(t, map[int][]string{
		20: {"/usr/sbin/haproxy", "/usr/sbin/haproxy", "-Ws", "-f", "/etc/haproxy/haproxy.cfg", "-f", "/etc/haproxy/conf.d", "-p", "/run/haproxy.pid"},
		21: {"/usr/sbin/haproxy", "/usr/sbin/haproxy", "-Ws"},
	}, map[int]int{20: 1, 21: 20}, "ii \thaproxy\t2.6.12-1+deb12u1\n")
	ctx.Fs.(*fakefs.FS).Files(t, map[string]string{
		"/etc/haproxy/haproxy.cfg":      haproxyCfg,
		"/etc/haproxy/conf.d/extra.cfg": "backend extra\n\tserver e 10.0.9.9\n",
		"/etc/haproxy/conf.d/README":    "frontend ignored\n",
	})

	out, _, err := collect(t, ctx)
	require.NoError(t, err)
	assert.Equal(t, []snapshot.Webserver{{
		Type:    "haproxy",
		Version: "2.6.12",
		Frontends: []snapshot.Frontend{
			{Name: "www", Mode: "http", Bind: []string{"0.0.0.0:80", "0.0.0.0:443", ":::443"}, DefaultBackend: "web", Backends: []string{"api"}},
			{Name: "galera", Mode: "tcp", Bind: []string{"127.0.0.1:3306"}, DefaultBackend: "galera"},
			{Name: "stats", Mode: "http", Bind: []string{"unix:/run/haproxy/admin.sock", "unix:/run/haproxy/other.sock"}},
		},
		Backends: []snapshot.Backend{
			{Name: "web", Mode: "http", Servers: []snapshot.BackendServer{{Name: "web1", Address: "10.0.1.31:80"}, {Name: "web2", Address: "10.0.1.32:80"}}},
			{Name: "api", Mode: "http", Servers: []snapshot.BackendServer{{Name: "api1", Address: "10.0.1.41:8000"}}},
			{Name: "galera", Mode: "tcp", Servers: []snapshot.BackendServer{{Name: "db1", Address: "10.0.1.51:3306"}, {Name: "db2", Address: "10.0.1.52:3306"}}},
			{Name: "extra", Servers: []snapshot.BackendServer{{Name: "e", Address: "10.0.9.9"}}},
		},
	}}, out.Webservers)
}

func TestCollect_HAProxyMissingConfig(t *testing.T) {
	ctx, _ := newContext(t, map[int][]string{20: {"/usr/sbin/haproxy", "/usr/sbin/haproxy"}}, nil, "")
	_, _, err := collect(t, ctx)
	assert.ErrorContains(t, err, "haproxy: open "+HAProxyConfig)
}

const traefikRaw = `{
  "routers": {
    "api@docker": {"entryPoints": ["websecure"], "service": "api", "rule": "Host(` + "`api.client.fr`" + `)", "tls": {"certResolver": "le"}},
    "dashboard@internal": {"entryPoints": ["traefik"], "service": "api@internal", "rule": "PathPrefix(` + "`/api`" + `)"},
    "plain": {"service": "front", "rule": "Host(` + "`front.client.fr`" + `)", "tls": null}
  },
  "services": {
    "api@docker": {"loadBalancer": {"servers": [{"url": "http://172.18.0.5:8000"}, {"url": "http://172.18.0.6:8000"}]}},
    "api@internal": {},
    "front": {"weighted": {}},
    "resizer@file": {"weighted": {"services": [{"name": "resizer_v1", "weight": 3}, {"name": "resizer_v2@docker", "weight": 1}]}},
    "mirror@file": {"mirroring": {"service": "main", "mirrors": [{"name": "shadow", "percent": 10}]}},
    "ha@file": {"failover": {"service": "main", "fallback": "backup"}}
  },
  "tcpRouters": {"pg@file": {"entryPoints": ["pg"], "service": "pg", "rule": "HostSNI(` + "`*`" + `)"}},
  "tcpServices": {"pg@file": {"loadBalancer": {"servers": [{"address": "10.0.1.20:5432"}]}}}
}`

func TestCollect_Traefik(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			_, _ = w.Write([]byte(`{"Version":"3.1.2","Codename":"comte"}`))
		case "/api/rawdata":
			_, _ = w.Write([]byte(traefikRaw))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(api.Close)
	ctx, _ := newContext(t, map[int][]string{30: {"/usr/local/bin/traefik", "traefik"}}, map[int]int{30: 1}, "")
	ctx.Config.Collectors.Traefik.APIURL = api.URL + "/"
	// Traefik in a container is read all the same, through its API.
	ctx.Fs.(*fakefs.FS).Link("/proc/self/ns/pid", "pid:[1]")
	ctx.Fs.(*fakefs.FS).Link("/proc/30/ns/pid", "pid:[2]")

	out, _, err := collect(t, ctx)
	require.NoError(t, err)
	assert.Equal(t, []snapshot.Webserver{{
		Type:    "traefik",
		Version: "3.1.2",
		Routers: []snapshot.Router{
			{Name: "api@docker", Rule: "Host(`api.client.fr`)", Entrypoints: []string{"websecure"}, Service: "api@docker", TLS: true},
			{Name: "dashboard@internal", Rule: "PathPrefix(`/api`)", Entrypoints: []string{"traefik"}, Service: "api@internal"},
			{Name: "plain", Rule: "Host(`front.client.fr`)", Service: "front"},
			{Name: "pg@file", Rule: "HostSNI(`*`)", Entrypoints: []string{"pg"}, Service: "pg@file"},
		},
		Services: []snapshot.ProxyService{
			{Name: "api@docker", Servers: []string{"http://172.18.0.5:8000", "http://172.18.0.6:8000"}},
			{Name: "api@internal", Servers: []string{}},
			{Name: "front", Servers: []string{}},
			{Name: "ha@file", Servers: []string{"main@file", "backup@file"}},
			{Name: "mirror@file", Servers: []string{"main@file", "shadow@file"}},
			{Name: "resizer@file", Servers: []string{"resizer_v1@file", "resizer_v2@docker"}},
			// TCP services come after the HTTP ones.
			{Name: "pg@file", Servers: []string{"10.0.1.20:5432"}},
		},
	}}, out.Webservers)
}

func TestCollect_TraefikErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{name: "no api", handler: func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }, want: "/api/version: 404 Not Found"},
		{name: "invalid json", handler: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/version" {
				_, _ = w.Write([]byte(`{"Version":"3.1.2"}`))
				return
			}
			_, _ = w.Write([]byte(`{`))
		}, want: "/api/rawdata: unexpected EOF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := httptest.NewServer(tt.handler)
			t.Cleanup(api.Close)
			ctx, _ := newContext(t, map[int][]string{30: {"/usr/local/bin/traefik", "traefik"}}, nil, "")
			ctx.Config.Collectors.Traefik.APIURL = api.URL
			_, _, err := collect(t, ctx)
			assert.ErrorContains(t, err, tt.want)
		})
	}
	t.Run("unreachable default", func(t *testing.T) {
		ctx, _ := newContext(t, map[int][]string{30: {"/usr/local/bin/traefik", "traefik"}}, nil, "")
		ctx.Config.Collectors.Traefik.APIURL = "http://127.0.0.1:1"
		_, _, err := collect(t, ctx)
		assert.ErrorContains(t, err, "connection refused")
	})
	t.Run("invalid url", func(t *testing.T) {
		assert.Error(t, getJSON(t.Context(), "://bad", nil))
	})
}

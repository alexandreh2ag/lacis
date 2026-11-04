package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"testing/fstest"

	"github.com/alexandreh2ag/lacis/config"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const index = `<!doctype html><html><head><base href="/" />
<script id="lacis-config" type="application/json">{"basePath":"/"}</script></head><body></body></html>`

func serve(t *testing.T, files fstest.MapFS, target string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	cfg := &config.Config{Listen: config.ListenConfig{BasePath: "/lacis", PublicURL: "https://lacis.acme.example/lacis/"}, MCP: config.MCPConfig{Enabled: true},
		Auth:  config.AuthConfig{Local: config.LocalConfig{Enabled: true}, OIDC: config.OIDCConfig{Enabled: true}},
		Icons: config.IconsConfig{MaxSize: 256 * config.KB, MinSide: 16, MaxSide: 512}}
	NewHandler(files, cfg, "1.2.0</script>").Register(e.Group("/lacis"))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

var files = fstest.MapFS{
	"index.html":          {Data: []byte(index)},
	"favicon.svg":         {Data: []byte("<svg/>")},
	"favicon.ico":         {Data: []byte("ico")},
	"assets/index-ab1.js": {Data: []byte("console.log(1)")},
}

func TestServe_Index(t *testing.T) {
	for _, target := range []string{"/lacis", "/lacis/", "/lacis/index.html", "/lacis/acme/graph", "/lacis/acme/hosts/42"} {
		rec := serve(t, files, target)
		require.Equal(t, http.StatusOK, rec.Code, target)
		body := rec.Body.String()
		assert.Contains(t, body, `<base href="/lacis/" />`, target)
		assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
		assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
		assert.Equal(t, "frame-ancestors 'none'", rec.Header().Get("Content-Security-Policy"))
		raw := regexp.MustCompile(`application/json">([^<]*)</script>`).FindStringSubmatch(body)
		require.Len(t, raw, 2, "the version cannot close the script")
		assert.Contains(t, raw[1], `"version":"1.2.0\u003c/script\u003e"`, "escaped in the JSON")
		var cfg Config
		require.NoError(t, json.Unmarshal([]byte(raw[1]), &cfg))
		assert.Equal(t, Config{BasePath: "/lacis/", Version: "1.2.0</script>", Auth: Auth{Local: true, OIDC: true},
			Icons: Icons{MaxSize: 262144, MinSide: 16, MaxSide: 512, Extensions: []string{".jpeg", ".jpg", ".png", ".svg", ".webp"}},
			MCP:   MCP{Enabled: true, URL: "https://lacis.acme.example/lacis/mcp"}}, cfg)
	}
}

func TestServe_Files(t *testing.T) {
	rec := serve(t, files, "/lacis/assets/index-ab1.js")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "console.log(1)", rec.Body.String())
	assert.Contains(t, rec.Header().Get("Cache-Control"), "immutable")

	rec = serve(t, files, "/lacis/favicon.svg")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("Cache-Control"))

	assert.Equal(t, http.StatusNotFound, serve(t, files, "/lacis/assets/gone.js").Code, "a missing asset is no page")
	assert.Equal(t, http.StatusNotFound, serve(t, files, "/lacis/logo.png").Code)
	assert.Equal(t, http.StatusOK, serve(t, files, "/lacis/../../etc/passwd").Code, "cleaned to a page of the front")
}

func TestRegisterRoot(t *testing.T) {
	tests := []struct {
		name   string
		files  fstest.MapFS
		prefix string
		want   int
	}{
		{name: "under a base path", files: files, prefix: "/lacis/", want: http.StatusOK},
		{name: "no favicon built", files: fstest.MapFS{"index.html": {Data: []byte(index)}}, prefix: "/lacis/", want: http.StatusNotFound},
		{name: "at the root, Register serves it", files: files, prefix: "/", want: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			NewHandler(tt.files, &config.Config{}, "1").RegisterRoot(e, tt.prefix)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
			assert.Equal(t, tt.want, rec.Code)
			if tt.want == http.StatusOK {
				assert.Equal(t, "ico", rec.Body.String())
			}
		})
	}
}

func TestServe_NotBuilt(t *testing.T) {
	rec := serve(t, fstest.MapFS{".gitkeep": {}}, "/lacis/")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "npm run build")
}

// Package ui serves the front: its files, and index.html for every other
// path so that the router of the front takes it, with the base path and the
// configuration of the server written in.
package ui

import (
	"bytes"
	"encoding/json"
	"html"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/icon"
	"github.com/labstack/echo/v5"
)

// Config is what the front reads of the server, in the lacis-config
// script of index.html.
type Config struct {
	BasePath string `json:"basePath"`
	Version  string `json:"version"`
	Auth     Auth   `json:"auth"`
	Icons    Icons  `json:"icons"`
	MCP      MCP    `json:"mcp"`
}

// MCP is the endpoint the assistants connect to; URL is empty when off.
type MCP struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
}

// Icons are the limits of an uploaded logo, told before the upload.
type Icons struct {
	MaxSize    int64    `json:"maxSize"`
	MinSide    int      `json:"minSide"`
	MaxSide    int      `json:"maxSide"`
	Extensions []string `json:"extensions"`
}

type Auth struct {
	Local bool `json:"local"`
	OIDC  bool `json:"oidc"`
}

var (
	baseTag   = regexp.MustCompile(`<base href="[^"]*"\s*/?>`)
	configTag = regexp.MustCompile(`(<script id="lacis-config" type="application/json">)[^<]*(</script>)`)
)

type Handler struct {
	files fs.FS
	index []byte
}

// NewHandler serves the front of files; without a readable index.html,
// the front was not built and every page says so.
func NewHandler(files fs.FS, cfg *config.Config, version string) *Handler {
	h := &Handler{files: files}
	raw, err := fs.ReadFile(files, "index.html")
	if err != nil {
		return h
	}
	listen := cfg.Listen
	mcp := MCP{Enabled: cfg.MCP.Enabled}
	if mcp.Enabled {
		mcp.URL = listen.MCPURL()
	}
	front, _ := json.Marshal(Config{BasePath: listen.Prefix(), Version: version, Auth: Auth{Local: cfg.Auth.Local.Enabled, OIDC: cfg.Auth.OIDC.Enabled},
		Icons: Icons{MaxSize: int64(cfg.Icons.MaxSize), MinSide: cfg.Icons.MinSide, MaxSide: cfg.Icons.MaxSide, Extensions: icon.Extensions()},
		MCP:   mcp})
	// The configuration is JSON in a script element: json.Marshal writes
	// < > & as \u003c \u003e \u0026, so no value can close the element.
	escaped := string(front)
	raw = baseTag.ReplaceAll(raw, []byte(`<base href="`+html.EscapeString(listen.Prefix())+`" />`))
	raw = configTag.ReplaceAllFunc(raw, func(m []byte) []byte {
		sub := configTag.FindSubmatch(m)
		return bytes.Join([][]byte{sub[1], []byte(escaped), sub[2]}, nil)
	})
	h.index = raw
	return h
}

// Register serves the front on GET of every path the other routes leave.
func (h *Handler) Register(g *echo.Group) {
	g.GET("", h.Serve)
	g.GET("/*", h.Serve)
}

func (h *Handler) Serve(c *echo.Context) error {
	if h.index == nil {
		return c.String(http.StatusServiceUnavailable, "The front is not built: run npm run build in web/.\n")
	}
	name := strings.TrimPrefix(path.Clean("/"+c.Param("*")), "/")
	if name != "" && name != "index.html" {
		if f, err := fs.Stat(h.files, name); err == nil && !f.IsDir() {
			// Vite names the assets by their hash: they never change.
			if strings.HasPrefix(name, "assets/") {
				c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			return c.FileFS(name, h.files)
		}
		// A missing asset is a 404, not the page.
		if strings.HasPrefix(name, "assets/") || path.Ext(name) != "" {
			return echo.ErrNotFound
		}
	}
	header := c.Response().Header()
	header.Set("Cache-Control", "no-cache")
	// No other site frames the front: a page such as the consent of an
	// assistant must not be clicked through a disguise.
	header.Set("X-Frame-Options", "DENY")
	header.Set("Content-Security-Policy", "frame-ancestors 'none'")
	return c.HTMLBlob(http.StatusOK, h.index)
}

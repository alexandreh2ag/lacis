// Package icon serves the logos uploaded for technologies, and takes their
// uploads.
package icon

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	lacisAuth "github.com/alexandreh2ag/lacis/auth"
	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/icon"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/labstack/echo/v5"
)

// FormField is the multipart field holding the file uploaded.
const FormField = "file"

// multipartOverhead is what a multipart body adds to its file.
const multipartOverhead = 64 << 10

// svgPolicy stops a logo opened by itself from running or loading anything.
const svgPolicy = "default-src 'none'; style-src 'unsafe-inline'; img-src data:; sandbox"

type Handler struct {
	logger *slog.Logger
	cfg    config.IconsConfig
	icons  service.TechIconService
}

func NewHandler(logger *slog.Logger, cfg config.IconsConfig, icons service.TechIconService) *Handler {
	return &Handler{logger: logger, cfg: cfg, icons: icons}
}

// Register adds the routes to the group reading the session: GET, PUT and
// DELETE icons/{tech}. PUT keeps another site from uploading through a
// form.
func (h *Handler) Register(g *echo.Group) {
	g.GET("/"+icon.Path+":tech", h.Get)
	g.PUT("/"+icon.Path+":tech", h.Put)
	g.DELETE("/"+icon.Path+":tech", h.Delete)
}

// ErrorResponse tells why a request is refused; Reason is too_big, format,
// dimension or unsafe for a file refused, tech for a technology misnamed.
type ErrorResponse struct {
	Error  string `json:"error"`
	Reason string `json:"reason,omitempty"`
}

// Get serves the logo of a technology to a user of the front. Asked with
// its version, it is cached for good; without, it is checked each time.
func (h *Handler) Get(c *echo.Context) error {
	if lacisAuth.FromContext(c.Request().Context()) == nil {
		return c.JSON(http.StatusUnauthorized, ErrorResponse{Error: "unauthorized"})
	}
	i, err := h.icons.Get(c.Request().Context(), c.Param("tech"))
	if errors.Is(err, service.ErrNotFound) || errors.Is(err, service.ErrInvalid) {
		return c.JSON(http.StatusNotFound, ErrorResponse{Error: "no logo for this technology"})
	}
	if err != nil {
		return err
	}
	version := icon.Version(i.UpdatedAt)
	header := c.Response().Header()
	header.Set("ETag", `"`+version+`"`)
	if c.QueryParam("v") == version {
		header.Set("Cache-Control", "private, max-age=31536000, immutable")
	} else {
		header.Set("Cache-Control", "private, no-cache")
	}
	if c.Request().Header.Get("If-None-Match") == `"`+version+`"` {
		return c.NoContent(http.StatusNotModified)
	}
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", svgPolicy)
	header.Set("Content-Length", strconv.Itoa(len(i.Content)))
	return c.Blob(http.StatusOK, i.ContentType, i.Content)
}

// authorize checks that the user may change the logos; when not, it
// answers and returns nil.
func (h *Handler) authorize(c *echo.Context) (*lacisAuth.Principal, error) {
	p := lacisAuth.FromContext(c.Request().Context())
	if p == nil {
		return nil, c.JSON(http.StatusUnauthorized, ErrorResponse{Error: "unauthorized"})
	}
	if !p.CanGlobal(model.ResourceIcons, model.ActionWrite) {
		return nil, c.JSON(http.StatusForbidden, ErrorResponse{Error: "forbidden: icons write is needed"})
	}
	return p, nil
}

// Put makes the file of the multipart body the logo of the technology.
func (h *Handler) Put(c *echo.Context) error {
	p, err := h.authorize(c)
	if p == nil {
		return err
	}
	limit := int64(h.cfg.MaxSize)
	r := c.Request()
	r.Body = http.MaxBytesReader(c.Response(), r.Body, limit+multipartOverhead)
	f, header, err := r.FormFile(FormField)
	if err != nil {
		if mbe := (*http.MaxBytesError)(nil); errors.As(err, &mbe) {
			return c.JSON(http.StatusRequestEntityTooLarge, ErrorResponse{Error: "the logo is too big", Reason: "too_big"})
		}
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "a multipart body with a " + FormField + " field is expected"})
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return err
	}
	i, err := h.icons.Set(r.Context(), p, c.Param("tech"), header.Filename, data)
	if err != nil {
		return h.refused(c, err)
	}
	return c.JSON(http.StatusOK, map[string]string{"tech": i.Tech, "url": icon.URL(i.Tech, i.UpdatedAt)})
}

// Delete removes the logo uploaded: the front shows its own again.
func (h *Handler) Delete(c *echo.Context) error {
	if p, err := h.authorize(c); p == nil {
		return err
	}
	if err := h.icons.Reset(c.Request().Context(), c.Param("tech")); err != nil {
		return h.refused(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// refused answers the refusal of the service.
func (h *Handler) refused(c *echo.Context, err error) error {
	for _, r := range []struct {
		err    error
		status int
		reason string
	}{
		{icon.ErrTooBig, http.StatusRequestEntityTooLarge, "too_big"},
		{icon.ErrFormat, http.StatusBadRequest, "format"},
		{icon.ErrDimension, http.StatusBadRequest, "dimension"},
		{icon.ErrUnsafe, http.StatusBadRequest, "unsafe"},
		{service.ErrInvalid, http.StatusBadRequest, "tech"},
	} {
		if errors.Is(err, r.err) {
			return c.JSON(r.status, ErrorResponse{Error: err.Error(), Reason: r.reason})
		}
	}
	return err
}

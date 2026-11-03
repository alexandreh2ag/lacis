package icon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	lacisAuth "github.com/alexandreh2ag/lacis/auth"
	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/icon"
	mockService "github.com/alexandreh2ag/lacis/mocks/service"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var at = time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)

func iconWriter() *lacisAuth.Principal {
	return &lacisAuth.Principal{User: model.User{Username: "admin"},
		Global: []model.GlobalPermission{{Resource: model.ResourceIcons, Action: model.ActionWrite}}}
}

func reader() *lacisAuth.Principal { return &lacisAuth.Principal{User: model.User{Username: "bob"}} }

type fixture struct {
	icons *mockService.MockTechIconService
	e     *echo.Echo
}

func newFixture(t *testing.T, p *lacisAuth.Principal) *fixture {
	f := &fixture{icons: mockService.NewMockTechIconService(gomock.NewController(t)), e: echo.New()}
	g := f.e.Group("/lacis", func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if p != nil {
				c.SetRequest(c.Request().WithContext(lacisAuth.WithPrincipal(c.Request().Context(), p)))
			}
			return next(c)
		}
	})
	NewHandler(nil, config.IconsConfig{MaxSize: 1024, MinSide: 16, MaxSide: 512}, f.icons).Register(g)
	return f
}

func (f *fixture) do(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	f.e.ServeHTTP(rec, req)
	return rec
}

// upload is a PUT of a multipart body holding the file in field.
func upload(t *testing.T, target, field, name string, data []byte) *http.Request {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	part, err := w.CreateFormFile(field, name)
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	req := httptest.NewRequest(http.MethodPut, target, &b)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func reason(t *testing.T, rec *httptest.ResponseRecorder) string {
	var r ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &r))
	return r.Reason
}

func TestGet(t *testing.T) {
	f := newFixture(t, reader())
	stored := &model.TechIcon{Tech: "traefik", ContentType: icon.ContentTypeSVG, Content: []byte("<svg/>"), UpdatedAt: at}
	version := icon.Version(at)
	f.icons.EXPECT().Get(gomock.Any(), "traefik").Return(stored, nil).Times(3)

	rec := f.do(httptest.NewRequest(http.MethodGet, "/lacis/icons/traefik?v="+version, nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "<svg/>", rec.Body.String())
	assert.Equal(t, icon.ContentTypeSVG, rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Cache-Control"), "immutable", "its version in the URL")
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "sandbox")

	rec = f.do(httptest.NewRequest(http.MethodGet, "/lacis/icons/traefik", nil))
	assert.Equal(t, "private, no-cache", rec.Header().Get("Cache-Control"), "without version, checked each time")

	req := httptest.NewRequest(http.MethodGet, "/lacis/icons/traefik", nil)
	req.Header.Set("If-None-Match", `"`+version+`"`)
	assert.Equal(t, http.StatusNotModified, f.do(req).Code)

	f.icons.EXPECT().Get(gomock.Any(), "redis").Return(nil, service.ErrNotFound)
	assert.Equal(t, http.StatusNotFound, f.do(httptest.NewRequest(http.MethodGet, "/lacis/icons/redis", nil)).Code)
	f.icons.EXPECT().Get(gomock.Any(), "x").Return(nil, errors.New("db down"))
	assert.Equal(t, http.StatusInternalServerError, f.do(httptest.NewRequest(http.MethodGet, "/lacis/icons/x", nil)).Code)

	assert.Equal(t, http.StatusUnauthorized, newFixture(t, nil).do(httptest.NewRequest(http.MethodGet, "/lacis/icons/traefik", nil)).Code)
}

func TestPut(t *testing.T) {
	p := iconWriter()
	f := newFixture(t, p)
	f.icons.EXPECT().Set(gomock.Any(), p, "traefik", "traefik.svg", []byte("<svg/>")).
		Return(&service.TechIconInfo{TechIcon: model.TechIcon{Tech: "traefik", UpdatedAt: at}}, nil)
	rec := f.do(upload(t, "/lacis/icons/traefik", FormField, "traefik.svg", []byte("<svg/>")))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"tech":"traefik","url":"icons/traefik?v=`+icon.Version(at)+`"}`, rec.Body.String())

	for _, tt := range []struct {
		err    error
		status int
		reason string
	}{
		{fmt.Errorf("%w: %w", service.ErrInvalid, icon.ErrTooBig), http.StatusRequestEntityTooLarge, "too_big"},
		{fmt.Errorf("%w: %w", service.ErrInvalid, icon.ErrFormat), http.StatusBadRequest, "format"},
		{fmt.Errorf("%w: %w", service.ErrInvalid, icon.ErrDimension), http.StatusBadRequest, "dimension"},
		{fmt.Errorf("%w: %w", service.ErrInvalid, icon.ErrUnsafe), http.StatusBadRequest, "unsafe"},
		{fmt.Errorf("%w: technology", service.ErrInvalid), http.StatusBadRequest, "tech"},
	} {
		f.icons.EXPECT().Set(gomock.Any(), p, "traefik", gomock.Any(), gomock.Any()).Return(nil, tt.err)
		rec = f.do(upload(t, "/lacis/icons/traefik", FormField, "traefik.png", []byte("png")))
		assert.Equal(t, tt.status, rec.Code, tt.err.Error())
		assert.Equal(t, tt.reason, reason(t, rec))
	}
	f.icons.EXPECT().Set(gomock.Any(), p, "traefik", gomock.Any(), gomock.Any()).Return(nil, errors.New("db down"))
	assert.Equal(t, http.StatusInternalServerError, f.do(upload(t, "/lacis/icons/traefik", FormField, "t.png", []byte("png"))).Code)

	rec = f.do(upload(t, "/lacis/icons/traefik", FormField, "big.png", bytes.Repeat([]byte("x"), 200<<10)))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, "the body is cut before it is read")
	assert.Equal(t, "too_big", reason(t, rec))
	assert.Equal(t, http.StatusBadRequest, f.do(upload(t, "/lacis/icons/traefik", "other", "t.png", []byte("png"))).Code)
	assert.Equal(t, http.StatusNotFound, f.do(httptest.NewRequest(http.MethodPost, "/lacis/icons/traefik", nil)).Code,
		"no form can upload: POST has no route")
}

func TestDeleteAndAccess(t *testing.T) {
	f := newFixture(t, iconWriter())
	f.icons.EXPECT().Reset(gomock.Any(), "traefik").Return(nil)
	assert.Equal(t, http.StatusNoContent, f.do(httptest.NewRequest(http.MethodDelete, "/lacis/icons/traefik", nil)).Code)
	f.icons.EXPECT().Reset(gomock.Any(), "a b").Return(fmt.Errorf("%w: technology", service.ErrInvalid))
	assert.Equal(t, http.StatusBadRequest, f.do(httptest.NewRequest(http.MethodDelete, "/lacis/icons/a%20b", nil)).Code)

	f = newFixture(t, reader())
	assert.Equal(t, http.StatusForbidden, f.do(httptest.NewRequest(http.MethodDelete, "/lacis/icons/traefik", nil)).Code)
	assert.Equal(t, http.StatusForbidden, f.do(upload(t, "/lacis/icons/traefik", FormField, "t.png", []byte("png"))).Code)
	f = newFixture(t, nil)
	assert.Equal(t, http.StatusUnauthorized, f.do(httptest.NewRequest(http.MethodDelete, "/lacis/icons/traefik", nil)).Code)
}

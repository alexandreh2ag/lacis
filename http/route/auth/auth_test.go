package auth

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	lacisAuth "github.com/alexandreh2ag/lacis/auth"
	"github.com/alexandreh2ag/lacis/config"
	mockService "github.com/alexandreh2ag/lacis/mocks/service"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var (
	discard = slog.New(slog.NewTextHandler(io.Discard, nil))
	userID  = uuid.MustParse("019a2400-0000-7000-8000-0000000000aa")
	expires = time.Date(2026, 10, 28, 13, 0, 0, 0, time.UTC)
)

// server serves the auth routes under /lacis, and /lacis/whoami telling
// the principal the middleware put in the context.
func server(svc service.AuthService, listen config.ListenConfig) *echo.Echo {
	e := echo.New()
	g := e.Group("/lacis", Session(discard, svc))
	NewHandler(discard, listen, svc).Register(g)
	g.GET("/whoami", func(c *echo.Context) error {
		p := lacisAuth.FromContext(c.Request().Context())
		if p == nil {
			return c.String(http.StatusOK, "anonymous")
		}
		return c.String(http.StatusOK, p.User.Username)
	})
	return e
}

func TestLogin(t *testing.T) {
	name := "Admin"
	principal := &lacisAuth.Principal{User: model.User{Base: model.Base{ID: userID}, Username: "admin", FirstName: &name}}
	tests := []struct {
		name        string
		contentType string
		body        string
		loginErr    error
		wantStatus  int
		wantBody    string
	}{
		{name: "logged in", wantStatus: http.StatusOK,
			wantBody: `{"id":"019a2400-0000-7000-8000-0000000000aa","username":"admin","first_name":"Admin"}`},
		{name: "not json", contentType: "application/x-www-form-urlencoded", wantStatus: http.StatusUnsupportedMediaType, wantBody: `{"error":"the body must be JSON"}`},
		{name: "invalid json", body: "{", wantStatus: http.StatusBadRequest, wantBody: `{"error":"invalid JSON body"}`},
		{name: "refused", loginErr: service.ErrUnauthorized, wantStatus: http.StatusUnauthorized, wantBody: `{"error":"invalid username or password"}`},
		{name: "too many", loginErr: service.ErrTooManyAttempts, wantStatus: http.StatusTooManyRequests, wantBody: `{"error":"too many attempts, try again later"}`},
		{name: "disabled", loginErr: service.ErrLocalAuthDisabled, wantStatus: http.StatusForbidden, wantBody: `{"error":"local authentication is disabled"}`},
		{name: "internal", loginErr: errors.New("db down"), wantStatus: http.StatusInternalServerError, wantBody: `{"error":"internal error"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := mockService.NewMockAuthService(gomock.NewController(t))
			body := tt.body
			if body == "" {
				body = `{"username":"admin","password":"correct horse"}`
			}
			if tt.wantStatus != http.StatusUnsupportedMediaType && tt.wantStatus != http.StatusBadRequest {
				svc.EXPECT().Login(gomock.Any(), service.LoginInput{Username: "admin", Password: "correct horse", IP: "192.0.2.1"}).
					Return(&service.Login{Token: "tok", ExpiresAt: expires}, tt.loginErr)
			}
			if tt.loginErr == nil && tt.wantStatus == http.StatusOK {
				svc.EXPECT().Authenticate(gomock.Any(), "tok").Return(principal, nil)
			}
			contentType := tt.contentType
			if contentType == "" {
				contentType = "application/json; charset=utf-8"
			}
			req := httptest.NewRequest(http.MethodPost, "/lacis/auth/login", strings.NewReader(body))
			req.Header.Set(echo.HeaderContentType, contentType)
			rec := httptest.NewRecorder()
			server(svc, config.ListenConfig{BasePath: "/lacis", PublicURL: "https://lacis.example.com"}).ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.JSONEq(t, tt.wantBody, rec.Body.String())
			if tt.wantStatus == http.StatusOK {
				cookie := rec.Result().Cookies()[0]
				assert.Equal(t, CookieName, cookie.Name)
				assert.Equal(t, "tok", cookie.Value)
				assert.Equal(t, "/lacis/", cookie.Path)
				assert.True(t, cookie.HttpOnly)
				assert.True(t, cookie.Secure)
				assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
				assert.Equal(t, expires, cookie.Expires)
			}
		})
	}
}

func TestLogin_SessionNotFound(t *testing.T) {
	svc := mockService.NewMockAuthService(gomock.NewController(t))
	svc.EXPECT().Login(gomock.Any(), gomock.Any()).Return(&service.Login{Token: "tok", ExpiresAt: expires}, nil)
	svc.EXPECT().Authenticate(gomock.Any(), "tok").Return(nil, errors.New("db down"))
	req := httptest.NewRequest(http.MethodPost, "/lacis/auth/login", strings.NewReader(`{}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	server(svc, config.ListenConfig{BasePath: "/lacis"}).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Empty(t, rec.Result().Cookies())
}

func TestLogout(t *testing.T) {
	tests := []struct {
		name       string
		cookie     string
		logoutErr  error
		wantStatus int
	}{
		{name: "logged out", cookie: "tok", wantStatus: http.StatusNoContent},
		{name: "no cookie", wantStatus: http.StatusNoContent},
		{name: "internal", cookie: "tok", logoutErr: errors.New("db down"), wantStatus: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := mockService.NewMockAuthService(gomock.NewController(t))
			req := httptest.NewRequest(http.MethodPost, "/lacis/auth/logout", nil)
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: CookieName, Value: tt.cookie})
				svc.EXPECT().Authenticate(gomock.Any(), tt.cookie).Return(nil, service.ErrUnauthorized)
				svc.EXPECT().Logout(gomock.Any(), tt.cookie).Return(tt.logoutErr)
			}
			rec := httptest.NewRecorder()
			server(svc, config.ListenConfig{BasePath: "/lacis"}).ServeHTTP(rec, req)
			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantStatus == http.StatusNoContent {
				cookie := rec.Result().Cookies()[0]
				assert.Empty(t, cookie.Value)
				assert.False(t, cookie.Secure, "plain HTTP")
				assert.True(t, cookie.Expires.Before(time.Now()))
			}
		})
	}
}

func TestSession(t *testing.T) {
	principal := &lacisAuth.Principal{User: model.User{Username: "admin"}}
	tests := []struct {
		name       string
		cookie     string
		authErr    error
		wantStatus int
		wantBody   string
	}{
		{name: "valid", cookie: "tok", wantStatus: http.StatusOK, wantBody: "admin"},
		{name: "no cookie", wantStatus: http.StatusOK, wantBody: "anonymous"},
		{name: "expired", cookie: "old", authErr: service.ErrUnauthorized, wantStatus: http.StatusOK, wantBody: "anonymous"},
		{name: "internal", cookie: "tok", authErr: errors.New("db down"), wantStatus: http.StatusInternalServerError, wantBody: `{"error":"internal error"}` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := mockService.NewMockAuthService(gomock.NewController(t))
			req := httptest.NewRequest(http.MethodGet, "/lacis/whoami", nil)
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: CookieName, Value: tt.cookie})
				p := principal
				if tt.authErr != nil {
					p = nil
				}
				svc.EXPECT().Authenticate(gomock.Any(), tt.cookie).Return(p, tt.authErr)
			}
			rec := httptest.NewRecorder()
			server(svc, config.ListenConfig{BasePath: "/lacis"}).ServeHTTP(rec, req)
			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, tt.wantBody, rec.Body.String())
		})
	}
}

func TestSecure(t *testing.T) {
	assert.False(t, Secure(config.ListenConfig{}))
	assert.True(t, Secure(config.ListenConfig{PublicURL: "https://lacis.example.com"}))
	assert.True(t, Secure(config.ListenConfig{TLS: config.TLSConfig{CertFile: "c", KeyFile: "k"}}))
	require.False(t, IsJSON(httptest.NewRequest(http.MethodPost, "/", nil)))
}

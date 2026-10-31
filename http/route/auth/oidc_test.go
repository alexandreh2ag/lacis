package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/auth/openid"
	"github.com/alexandreh2ag/lacis/config"
	mockOpenid "github.com/alexandreh2ag/lacis/mocks/auth/openid"
	mockClock "github.com/alexandreh2ag/lacis/mocks/common/clock"
	mockService "github.com/alexandreh2ag/lacis/mocks/service"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var (
	oidcListen = config.ListenConfig{BasePath: "/lacis", PublicURL: "https://lacis.acme.example/lacis"}
	oidcNow    = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	jane       = openid.Claims{Subject: "u-1", Username: "jane", Groups: []string{"devs"}}
)

type oidcFixture struct {
	e        *echo.Echo
	provider *mockOpenid.MockProvider
	auth     *mockService.MockAuthService
	now      time.Time
	// state, verifier and nonce are those the login gave the provider.
	state, verifier, nonce string
	flow                   []*http.Cookie
}

func newOIDCFixture(t *testing.T) *oidcFixture {
	ctrl := gomock.NewController(t)
	f := &oidcFixture{e: echo.New(), provider: mockOpenid.NewMockProvider(ctrl), auth: mockService.NewMockAuthService(ctrl), now: oidcNow}
	clk := mockClock.NewMockClock(ctrl)
	clk.EXPECT().Now().DoAndReturn(func() time.Time { return f.now }).AnyTimes()
	NewOIDCHandler(discard, oidcListen, "a-secret-of-32-characters-at-least", clk, f.provider, f.auth).Register(f.e.Group("/lacis"))
	return f
}

func (f *oidcFixture) get(target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Host = "lacis.acme.example"
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	f.e.ServeHTTP(rec, req)
	return rec
}

func cookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// login starts a flow and returns its cookie.
func (f *oidcFixture) login(t *testing.T, redirect string) *http.Cookie {
	f.provider.EXPECT().AuthURL(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, state, verifier, nonce string) (string, error) {
			f.state, f.verifier, f.nonce = state, verifier, nonce
			return "https://sso.acme.example/auth?state=" + state, nil
		})
	rec := f.get("/lacis/auth/oidc/login?redirect=" + url.QueryEscape(redirect))
	require.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "https://sso.acme.example/auth?state="+f.state, rec.Header().Get("Location"))
	c := cookie(rec, FlowCookieName)
	require.NotNil(t, c)
	assert.Equal(t, "/lacis/auth/oidc/", c.Path)
	assert.Equal(t, 600, c.MaxAge)
	assert.True(t, c.HttpOnly)
	assert.True(t, c.Secure, "the public URL is HTTPS")
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite, "the provider sends the browser back by a top-level GET")
	assert.Len(t, f.state, 43)
	assert.NotEqual(t, f.state, f.nonce)
	return c
}

func TestOIDC_LoginAndCallback(t *testing.T) {
	f := newOIDCFixture(t)
	flow := f.login(t, "/lacis/graph?level=host")

	f.provider.EXPECT().Exchange(gomock.Any(), "the-code", f.verifier, f.nonce).Return(&jane, nil)
	f.auth.EXPECT().LoginOIDC(gomock.Any(), jane).Return(&service.Login{Token: "tok", ExpiresAt: expires}, nil)
	rec := f.get("/lacis/auth/oidc/callback?code=the-code&state="+f.state, flow)
	require.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/lacis/graph?level=host", rec.Header().Get("Location"))
	session := cookie(rec, CookieName)
	require.NotNil(t, session)
	assert.Equal(t, "tok", session.Value)
	assert.Equal(t, "/lacis/", session.Path)
	assert.True(t, session.HttpOnly)
	assert.Equal(t, -1, cookie(rec, FlowCookieName).MaxAge, "the flow is used once")
}

func TestOIDC_CallbackRefused(t *testing.T) {
	tests := []struct {
		name string
		// prepare returns the query and cookies of the callback.
		prepare func(t *testing.T, f *oidcFixture) (string, []*http.Cookie)
		want    string
	}{
		{name: "no flow", want: ErrorOIDCExpired, prepare: func(*testing.T, *oidcFixture) (string, []*http.Cookie) {
			return "code=c&state=s", nil
		}},
		{name: "other state", want: ErrorOIDCExpired, prepare: func(t *testing.T, f *oidcFixture) (string, []*http.Cookie) {
			return "code=c&state=forged", []*http.Cookie{f.login(t, "")}
		}},
		{name: "tampered flow", want: ErrorOIDCExpired, prepare: func(t *testing.T, f *oidcFixture) (string, []*http.Cookie) {
			c := f.login(t, "")
			c.Value = "e30." + c.Value[len(c.Value)-43:]
			return "code=c&state=" + f.state, []*http.Cookie{c}
		}},
		{name: "unsigned flow", want: ErrorOIDCExpired, prepare: func(t *testing.T, f *oidcFixture) (string, []*http.Cookie) {
			return "code=c&state=s", []*http.Cookie{{Name: FlowCookieName, Value: "nodot"}}
		}},
		{name: "expired flow", want: ErrorOIDCExpired, prepare: func(t *testing.T, f *oidcFixture) (string, []*http.Cookie) {
			c := f.login(t, "")
			f.now = f.now.Add(FlowTTL)
			return "code=c&state=" + f.state, []*http.Cookie{c}
		}},
		{name: "denied at the provider", want: ErrorOIDCDenied, prepare: func(t *testing.T, f *oidcFixture) (string, []*http.Cookie) {
			return "error=access_denied&state=" + f.stateOf(t), f.flow
		}},
		{name: "exchange failed", want: ErrorOIDCFailed, prepare: func(t *testing.T, f *oidcFixture) (string, []*http.Cookie) {
			q := "code=c&state=" + f.stateOf(t)
			f.provider.EXPECT().Exchange(gomock.Any(), "c", f.verifier, f.nonce).Return(nil, errors.New("invalid_grant"))
			return q, f.flow
		}},
		{name: "no account", want: ErrorOIDCAccount, prepare: func(t *testing.T, f *oidcFixture) (string, []*http.Cookie) {
			q := "code=c&state=" + f.stateOf(t)
			f.provider.EXPECT().Exchange(gomock.Any(), "c", f.verifier, f.nonce).Return(&jane, nil)
			f.auth.EXPECT().LoginOIDC(gomock.Any(), jane).Return(nil, service.ErrOIDCAccount)
			return q, f.flow
		}},
		{name: "session failed", want: ErrorOIDCFailed, prepare: func(t *testing.T, f *oidcFixture) (string, []*http.Cookie) {
			q := "code=c&state=" + f.stateOf(t)
			f.provider.EXPECT().Exchange(gomock.Any(), "c", f.verifier, f.nonce).Return(&jane, nil)
			f.auth.EXPECT().LoginOIDC(gomock.Any(), jane).Return(nil, errors.New("database down"))
			return q, f.flow
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newOIDCFixture(t)
			query, cookies := tt.prepare(t, f)
			rec := f.get("/lacis/auth/oidc/callback?"+query, cookies...)
			require.Equal(t, http.StatusFound, rec.Code)
			assert.Equal(t, "/lacis/login?error="+tt.want, rec.Header().Get("Location"))
			assert.Nil(t, cookie(rec, CookieName), "no session")
		})
	}
}

// stateOf starts a flow and keeps its cookie in f.flow.
func (f *oidcFixture) stateOf(t *testing.T) string {
	f.flow = []*http.Cookie{f.login(t, "")}
	return f.state
}

func TestOIDC_LoginOnPublicHost(t *testing.T) {
	f := newOIDCFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/lacis/auth/oidc/login?redirect=%2Flacis%2Fhosts", nil)
	req.Host = "10.0.1.5:8080"
	rec := httptest.NewRecorder()
	f.e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "https://lacis.acme.example/lacis/auth/oidc/login?redirect=%2Flacis%2Fhosts", rec.Header().Get("Location"),
		"the provider comes back to the public URL: the flow starts there")
	assert.Nil(t, cookie(rec, FlowCookieName))
}

func TestOIDC_ProviderDown(t *testing.T) {
	f := newOIDCFixture(t)
	f.provider.EXPECT().AuthURL(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("discover OIDC provider"))
	rec := f.get("/lacis/auth/oidc/login")
	require.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/lacis/login?error="+ErrorOIDCUnavailable, rec.Header().Get("Location"))
	assert.Nil(t, cookie(rec, FlowCookieName))
}

func TestOIDC_Redirect(t *testing.T) {
	h := &OIDCHandler{cfg: oidcListen}
	for in, want := range map[string]string{
		"":                            "/lacis/",
		"/lacis/graph?level=app":      "/lacis/graph?level=app",
		"/lacis/hosts/acme-rp-1":      "/lacis/hosts/acme-rp-1",
		"https://evil.example/lacis/": "/lacis/",
		"//evil.example/lacis/":       "/lacis/",
		"/other":                      "/lacis/",
		`/lacis\..\evil`:              "/lacis/",
		"javascript:alert(1)":         "/lacis/",
		"/lacis/%zz":                  "/lacis/",
	} {
		assert.Equal(t, want, h.redirect(in), in)
	}
}

func TestCallbackURL(t *testing.T) {
	assert.Equal(t, "https://lacis.acme.example/lacis/auth/oidc/callback", CallbackURL(oidcListen))
	assert.Equal(t, "https://lacis.acme.example/auth/oidc/callback", CallbackURL(config.ListenConfig{PublicURL: "https://lacis.acme.example/"}))
}

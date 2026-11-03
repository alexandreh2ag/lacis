package oauth

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	lacisAuth "github.com/alexandreh2ag/lacis/auth"
	"github.com/alexandreh2ag/lacis/config"
	mockClock "github.com/alexandreh2ag/lacis/mocks/common/clock"
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
	listen   = config.ListenConfig{BasePath: "/lacis", PublicURL: "https://lacis.acme.example/lacis"}
	now      = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	alice    = &lacisAuth.Principal{User: model.User{Base: model.Base{ID: uuid.MustParse("00000000-0000-0000-0000-000000000001")}, Username: "alice"}}
	discard  = slog.New(slog.NewTextHandler(io.Discard, nil))
	callback = "https://claude.ai/api/mcp/auth_callback"
	client   = &model.OAuthClient{ID: "c1", Name: "Claude", RedirectURIs: model.StringArray{callback}}
)

type fixture struct {
	e     *echo.Echo
	oauth *mockService.MockOAuthService
	now   time.Time
}

// newFixture serves the routes; a request with the cookie lacis_session
// comes from alice.
func newFixture(t *testing.T) *fixture {
	ctrl := gomock.NewController(t)
	f := &fixture{e: echo.New(), oauth: mockService.NewMockOAuthService(ctrl), now: now}
	clk := mockClock.NewMockClock(ctrl)
	clk.EXPECT().Now().DoAndReturn(func() time.Time { return f.now }).AnyTimes()
	g := f.e.Group("/lacis", func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if _, err := c.Cookie("lacis_session"); err == nil {
				c.SetRequest(c.Request().WithContext(lacisAuth.WithPrincipal(c.Request().Context(), alice)))
			}
			return next(c)
		}
	})
	NewHandler(discard, listen, "a-secret-of-32-characters-at-least", clk, f.oauth).Register(f.e, g)
	return f
}

func (f *fixture) do(method, target, contentType, body string, session bool, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if session {
		req.AddCookie(&http.Cookie{Name: "lacis_session", Value: "s"})
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	f.e.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out), rec.Body.String())
	return out
}

func TestMetadata(t *testing.T) {
	f := newFixture(t)
	for _, path := range []string{"/lacis/.well-known/oauth-authorization-server", "/.well-known/oauth-authorization-server/lacis",
		"/lacis/.well-known/openid-configuration", "/.well-known/openid-configuration/lacis"} {
		rec := f.do(http.MethodGet, path, "", "", false)
		require.Equal(t, http.StatusOK, rec.Code, path)
		assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
		m := decode(t, rec)
		assert.Equal(t, "https://lacis.acme.example/lacis", m["issuer"])
		assert.Equal(t, "https://lacis.acme.example/lacis/oauth/authorize", m["authorization_endpoint"])
		assert.Equal(t, "https://lacis.acme.example/lacis/oauth/token", m["token_endpoint"])
		assert.Equal(t, "https://lacis.acme.example/lacis/oauth/register", m["registration_endpoint"])
		assert.Equal(t, []any{"S256"}, m["code_challenge_methods_supported"])
	}
	for _, path := range []string{"/lacis/.well-known/oauth-protected-resource", "/lacis/.well-known/oauth-protected-resource/mcp",
		"/.well-known/oauth-protected-resource/lacis/mcp"} {
		rec := f.do(http.MethodGet, path, "", "", false)
		require.Equal(t, http.StatusOK, rec.Code, path)
		m := decode(t, rec)
		assert.Equal(t, "https://lacis.acme.example/lacis/mcp", m["resource"])
		assert.Equal(t, []any{"https://lacis.acme.example/lacis"}, m["authorization_servers"])
	}
	assert.Equal(t, "https://lacis.acme.example/lacis/.well-known/oauth-protected-resource", ProtectedResourceMetadataURL(listen))
}

func TestMetadata_RootBasePath(t *testing.T) {
	e := echo.New()
	root := config.ListenConfig{BasePath: "/", PublicURL: "https://lacis.acme.example"}
	NewHandler(discard, root, "a-secret-of-32-characters-at-least", nil, nil).Register(e, e.Group(""))
	for _, path := range []string{"/.well-known/oauth-authorization-server", "/.well-known/oauth-protected-resource/mcp"} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusOK, rec.Code, path)
	}
}

func TestRegisterClient(t *testing.T) {
	body := `{"client_name":"Claude","redirect_uris":["` + callback + `"],"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"client_secret_post"}`
	tests := []struct {
		name        string
		contentType string
		body        string
		setup       func(f *fixture)
		wantStatus  int
		wantError   string
	}{
		{
			name: "confidential", contentType: "application/json", body: body, wantStatus: http.StatusCreated,
			setup: func(f *fixture) {
				f.oauth.EXPECT().RegisterClient(gomock.Any(), service.RegisterOAuthClientInput{Name: "Claude", RedirectURIs: []string{callback},
					AuthMethod: "client_secret_post", IP: "192.0.2.1"}).Return(&service.RegisteredOAuthClient{Secret: "lacis_cs_x", AuthMethod: "client_secret_post",
					Client: model.OAuthClient{ID: "c1", Name: "Claude", RedirectURIs: model.StringArray{callback}, CreatedAt: now}}, nil)
			},
		},
		{name: "not JSON", contentType: "text/plain", body: body, wantStatus: http.StatusBadRequest, wantError: service.OAuthErrInvalidClientMetadata},
		{name: "invalid JSON", contentType: "application/json", body: "{", wantStatus: http.StatusBadRequest, wantError: service.OAuthErrInvalidClientMetadata},
		{name: "implicit grant", contentType: "application/json", body: `{"client_name":"x","redirect_uris":["https://x"],"grant_types":["implicit"]}`,
			wantStatus: http.StatusBadRequest, wantError: service.OAuthErrInvalidClientMetadata},
		{
			name: "refused", contentType: "application/json", body: body, wantStatus: http.StatusBadRequest, wantError: service.OAuthErrInvalidRedirectURI,
			setup: func(f *fixture) {
				f.oauth.EXPECT().RegisterClient(gomock.Any(), gomock.Any()).Return(nil, &service.OAuthError{Code: service.OAuthErrInvalidRedirectURI})
			},
		},
		{
			name: "too many", contentType: "application/json", body: body, wantStatus: http.StatusTooManyRequests, wantError: "too_many_requests",
			setup: func(f *fixture) {
				f.oauth.EXPECT().RegisterClient(gomock.Any(), gomock.Any()).Return(nil, service.ErrTooManyAttempts)
			},
		},
		{
			name: "database down", contentType: "application/json", body: body, wantStatus: http.StatusInternalServerError, wantError: "server_error",
			setup: func(f *fixture) {
				f.oauth.EXPECT().RegisterClient(gomock.Any(), gomock.Any()).Return(nil, errors.New("db down"))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			rec := f.do(http.MethodPost, "/lacis/oauth/register", tt.contentType, tt.body, false)
			assert.Equal(t, tt.wantStatus, rec.Code)
			m := decode(t, rec)
			if tt.wantError != "" {
				assert.Equal(t, tt.wantError, m["error"])
				assert.NotContains(t, rec.Body.String(), "db down")
				return
			}
			assert.Equal(t, "c1", m["client_id"])
			assert.Equal(t, "lacis_cs_x", m["client_secret"])
			assert.EqualValues(t, 0, m["client_secret_expires_at"])
			assert.EqualValues(t, now.Unix(), m["client_id_issued_at"])
			assert.Equal(t, "client_secret_post", m["token_endpoint_auth_method"])
			assert.Equal(t, []any{callback}, m["redirect_uris"])
		})
	}
}

const authorizeQuery = "response_type=code&client_id=c1&redirect_uri=https%3A%2F%2Fclaude.ai%2Fapi%2Fmcp%2Fauth_callback&code_challenge=abc&code_challenge_method=S256&state=xyz&scope=read&resource=https%3A%2F%2Flacis.acme.example%2Flacis%2Fmcp"

func wantRequest() service.AuthorizeRequest {
	return service.AuthorizeRequest{ResponseType: "code", ClientID: "c1", RedirectURI: callback, CodeChallenge: "abc",
		CodeChallengeMethod: "S256", Scope: "read", Resource: "https://lacis.acme.example/lacis/mcp"}
}

// authorize starts an authorization and returns the signed request the
// consent page gets.
func (f *fixture) authorize(t *testing.T) string {
	t.Helper()
	f.oauth.EXPECT().CheckAuthorize(gomock.Any(), wantRequest()).Return(client, nil)
	rec := f.do(http.MethodGet, "/lacis/oauth/authorize?"+authorizeQuery, "", "", false)
	require.Equal(t, http.StatusFound, rec.Code)
	loc, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "/lacis/authorize", loc.Path)
	return loc.Query().Get("request")
}

func TestAuthorize(t *testing.T) {
	t.Run("to the consent", func(t *testing.T) {
		assert.NotEmpty(t, newFixture(t).authorize(t))
	})
	tests := []struct {
		name   string
		client *model.OAuthClient
		err    error
		want   string
	}{
		{name: "untrusted client", err: &service.OAuthError{Code: service.OAuthErrInvalidClient}, want: "/lacis/authorize?error=invalid_client"},
		{name: "error told to the client", client: client, err: &service.OAuthError{Code: service.OAuthErrInvalidRequest, Description: "PKCE"},
			want: callback + "?error=invalid_request&error_description=PKCE&iss=https%3A%2F%2Flacis.acme.example%2Flacis&state=xyz"},
		{name: "database down", err: errors.New("db down"), want: "/lacis/authorize?error=server_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.oauth.EXPECT().CheckAuthorize(gomock.Any(), wantRequest()).Return(tt.client, tt.err)
			rec := f.do(http.MethodGet, "/lacis/oauth/authorize?"+authorizeQuery, "", "", false)
			assert.Equal(t, http.StatusFound, rec.Code)
			assert.Equal(t, tt.want, rec.Header().Get("Location"))
		})
	}
}

func TestRequest(t *testing.T) {
	f := newFixture(t)
	signed := f.authorize(t)

	rec := f.do(http.MethodGet, "/lacis/oauth/request?request="+url.QueryEscape(signed), "", "", false)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "the page logs the user in first")

	f.oauth.EXPECT().CheckAuthorize(gomock.Any(), wantRequest()).Return(client, nil)
	rec = f.do(http.MethodGet, "/lacis/oauth/request?request="+url.QueryEscape(signed), "", "", true)
	require.Equal(t, http.StatusOK, rec.Code)
	var got Consent
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "Claude", got.ClientName)
	assert.Equal(t, "claude.ai", got.RedirectHost)
	assert.Equal(t, "alice", got.User.Username)

	tampered := "x" + signed
	rec = f.do(http.MethodGet, "/lacis/oauth/request?request="+url.QueryEscape(tampered), "", "", true)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, ErrorRequestExpired, decode(t, rec)["error"])

	f.oauth.EXPECT().CheckAuthorize(gomock.Any(), wantRequest()).Return(nil, &service.OAuthError{Code: service.OAuthErrInvalidClient})
	rec = f.do(http.MethodGet, "/lacis/oauth/request?request="+url.QueryEscape(signed), "", "", true)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "the client is gone")

	f.now = now.Add(RequestTTL)
	rec = f.do(http.MethodGet, "/lacis/oauth/request?request="+url.QueryEscape(signed), "", "", true)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "expired")
}

func TestConsent(t *testing.T) {
	f := newFixture(t)
	signed := f.authorize(t)
	body := func(approve bool) string {
		b, _ := json.Marshal(consentRequest{Request: signed, Approve: approve})
		return string(b)
	}

	rec := f.do(http.MethodPost, "/lacis/oauth/authorize", "application/x-www-form-urlencoded", body(true), true)
	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code, "no cross-site form")
	rec = f.do(http.MethodPost, "/lacis/oauth/authorize", "application/json", body(true), false)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	rec = f.do(http.MethodPost, "/lacis/oauth/authorize", "application/json", "{", true)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	rec = f.do(http.MethodPost, "/lacis/oauth/authorize", "application/json", `{"request":"forged.x","approve":true}`, true)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	f.oauth.EXPECT().Authorize(gomock.Any(), alice, wantRequest()).Return("lacis_ac_code", nil)
	rec = f.do(http.MethodPost, "/lacis/oauth/authorize", "application/json", body(true), true)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, callback+"?code=lacis_ac_code&iss=https%3A%2F%2Flacis.acme.example%2Flacis&state=xyz", decode(t, rec)["redirect"])

	f.oauth.EXPECT().CheckAuthorize(gomock.Any(), wantRequest()).Return(client, nil)
	rec = f.do(http.MethodPost, "/lacis/oauth/authorize", "application/json", body(false), true)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, callback+"?error=access_denied&iss=https%3A%2F%2Flacis.acme.example%2Flacis&state=xyz", decode(t, rec)["redirect"])

	f.oauth.EXPECT().CheckAuthorize(gomock.Any(), wantRequest()).Return(nil, &service.OAuthError{Code: service.OAuthErrInvalidClient})
	rec = f.do(http.MethodPost, "/lacis/oauth/authorize", "application/json", body(false), true)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "the refusal of a client gone goes nowhere")

	f.oauth.EXPECT().Authorize(gomock.Any(), alice, wantRequest()).Return("", service.ErrUnauthorized)
	rec = f.do(http.MethodPost, "/lacis/oauth/authorize", "application/json", body(true), true)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	f.oauth.EXPECT().Authorize(gomock.Any(), alice, wantRequest()).Return("", errors.New("db down"))
	rec = f.do(http.MethodPost, "/lacis/oauth/authorize", "application/json", body(true), true)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestCallback_KeepsTheQueryOfTheRedirectURI(t *testing.T) {
	h := NewHandler(discard, listen, "a-secret-of-32-characters-at-least", nil, nil)
	got := h.callback(request{AuthorizeRequest: service.AuthorizeRequest{RedirectURI: "http://localhost:3000/cb?x=1"}}, url.Values{"code": {"c"}})
	assert.Equal(t, "http://localhost:3000/cb?x=1&code=c&iss=https%3A%2F%2Flacis.acme.example%2Flacis", got)
}

func TestToken(t *testing.T) {
	tokens := &service.OAuthTokens{AccessToken: "lacis_at_a", RefreshToken: "lacis_rt_r", ExpiresIn: time.Hour, Scope: "read"}
	form := "application/x-www-form-urlencoded"
	t.Run("authorization code, secret in HTTP Basic", func(t *testing.T) {
		f := newFixture(t)
		f.oauth.EXPECT().Exchange(gomock.Any(), service.OAuthExchangeInput{OAuthClientAuth: service.OAuthClientAuth{ClientID: "c 1", ClientSecret: "s&1"},
			Code: "lacis_ac_x", RedirectURI: callback, CodeVerifier: "v", Resource: "https://lacis.acme.example/lacis/mcp"}).Return(tokens, nil)
		req := httptest.NewRequest(http.MethodPost, "/lacis/oauth/token", strings.NewReader(url.Values{"grant_type": {"authorization_code"}, "code": {"lacis_ac_x"},
			"redirect_uri": {callback}, "code_verifier": {"v"}, "resource": {"https://lacis.acme.example/lacis/mcp"}}.Encode()))
		req.Header.Set("Content-Type", form)
		req.SetBasicAuth(url.QueryEscape("c 1"), url.QueryEscape("s&1"))
		rec := httptest.NewRecorder()
		f.e.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		assert.Equal(t, map[string]any{"access_token": "lacis_at_a", "token_type": "Bearer", "expires_in": float64(3600),
			"refresh_token": "lacis_rt_r", "scope": "read"}, decode(t, rec))
	})
	t.Run("refresh, public client", func(t *testing.T) {
		f := newFixture(t)
		f.oauth.EXPECT().Refresh(gomock.Any(), service.OAuthRefreshInput{OAuthClientAuth: service.OAuthClientAuth{ClientID: "c1"},
			RefreshToken: "lacis_rt_r"}).Return(tokens, nil)
		rec := f.do(http.MethodPost, "/lacis/oauth/token", form, "grant_type=refresh_token&refresh_token=lacis_rt_r&client_id=c1", false)
		assert.Equal(t, http.StatusOK, rec.Code)
	})
	t.Run("client refused", func(t *testing.T) {
		f := newFixture(t)
		f.oauth.EXPECT().Refresh(gomock.Any(), gomock.Any()).Return(nil, &service.OAuthError{Code: service.OAuthErrInvalidClient, Description: "no"})
		rec := f.do(http.MethodPost, "/lacis/oauth/token", form, "grant_type=refresh_token&refresh_token=x&client_id=c1&client_secret=bad", false)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Equal(t, map[string]any{"error": "invalid_client", "error_description": "no"}, decode(t, rec))
	})
	t.Run("grant refused", func(t *testing.T) {
		f := newFixture(t)
		f.oauth.EXPECT().Exchange(gomock.Any(), gomock.Any()).Return(nil, &service.OAuthError{Code: service.OAuthErrInvalidGrant})
		rec := f.do(http.MethodPost, "/lacis/oauth/token", form, "grant_type=authorization_code&code=x&client_id=c1", false)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "invalid_grant", decode(t, rec)["error"])
	})
	t.Run("unsupported grant", func(t *testing.T) {
		rec := newFixture(t).do(http.MethodPost, "/lacis/oauth/token", form, "grant_type=password", false)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "unsupported_grant_type", decode(t, rec)["error"])
	})
	t.Run("invalid form", func(t *testing.T) {
		rec := newFixture(t).do(http.MethodPost, "/lacis/oauth/token", form, "%zz", false)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestRevoke(t *testing.T) {
	form := "application/x-www-form-urlencoded"
	f := newFixture(t)
	f.oauth.EXPECT().Revoke(gomock.Any(), service.OAuthClientAuth{ClientID: "c1"}, "lacis_rt_r").Return(nil)
	rec := f.do(http.MethodPost, "/lacis/oauth/revoke", form, "token=lacis_rt_r&client_id=c1", false)
	assert.Equal(t, http.StatusOK, rec.Code)

	f.oauth.EXPECT().Revoke(gomock.Any(), gomock.Any(), "x").Return(&service.OAuthError{Code: service.OAuthErrInvalidClient})
	rec = f.do(http.MethodPost, "/lacis/oauth/revoke", form, "token=x&client_id=c1", false)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = f.do(http.MethodPost, "/lacis/oauth/revoke", form, "%zz", false)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

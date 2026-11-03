package service

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/auth"
	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/hash"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gorm.io/gorm"
)

const (
	testVerifier    = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	testRedirectURI = "https://claude.ai/api/mcp/auth_callback"
)

func testChallenge() string {
	sum := sha256.Sum256([]byte(testVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func oauthContext(t *testing.T) *appContext.Context {
	appCtx := testContext(t)
	appCtx.Config.Listen.PublicURL = "https://lacis.acme.example/"
	appCtx.Config.MCP.AccessTokenTTL = time.Hour
	appCtx.Config.MCP.GrantLifetime = 30 * 24 * time.Hour
	return appCtx
}

func assertOAuthErr(t *testing.T, err error, code string) {
	t.Helper()
	var oe *OAuthError
	require.ErrorAs(t, err, &oe)
	assert.Equal(t, code, oe.Code)
}

func TestMCPResource(t *testing.T) {
	appCtx := oauthContext(t)
	assert.Equal(t, "https://lacis.acme.example", OAuthIssuer(appCtx.Config.Listen))
	assert.Equal(t, "https://lacis.acme.example/mcp", MCPResource(appCtx.Config.Listen))
}

func TestOAuthService_RegisterClient(t *testing.T) {
	tests := []struct {
		name       string
		in         RegisterOAuthClientInput
		wantMethod string
		wantSecret bool
		wantCode   string
	}{
		{name: "public client", in: RegisterOAuthClientInput{Name: " Claude ", RedirectURIs: []string{testRedirectURI}, AuthMethod: "none"}, wantMethod: "none"},
		{name: "confidential by default", in: RegisterOAuthClientInput{Name: "Claude", RedirectURIs: []string{testRedirectURI}}, wantMethod: OAuthAuthMethodSecretBasic, wantSecret: true},
		{name: "secret in the body", in: RegisterOAuthClientInput{Name: "Claude", RedirectURIs: []string{testRedirectURI}, AuthMethod: OAuthAuthMethodSecretPost}, wantMethod: OAuthAuthMethodSecretPost, wantSecret: true},
		{name: "loopback over HTTP", in: RegisterOAuthClientInput{Name: "Claude Code", RedirectURIs: []string{"http://localhost:33418/callback", "http://127.0.0.1:1/cb", "http://[::1]/cb"}, AuthMethod: "none"}, wantMethod: "none"},
		{name: "no name", in: RegisterOAuthClientInput{RedirectURIs: []string{testRedirectURI}}, wantCode: OAuthErrInvalidClientMetadata},
		{name: "name too long", in: RegisterOAuthClientInput{Name: strings.Repeat("x", MaxOAuthClientName+1), RedirectURIs: []string{testRedirectURI}}, wantCode: OAuthErrInvalidClientMetadata},
		{name: "control in the name", in: RegisterOAuthClientInput{Name: "Cla\nude", RedirectURIs: []string{testRedirectURI}}, wantCode: OAuthErrInvalidClientMetadata},
		{name: "no redirect URI", in: RegisterOAuthClientInput{Name: "Claude"}, wantCode: OAuthErrInvalidRedirectURI},
		{name: "too many redirect URIs", in: RegisterOAuthClientInput{Name: "Claude", RedirectURIs: make([]string, MaxOAuthRedirectURIs+1)}, wantCode: OAuthErrInvalidRedirectURI},
		{name: "HTTP off the loopback", in: RegisterOAuthClientInput{Name: "Claude", RedirectURIs: []string{"http://evil.example/cb"}}, wantCode: OAuthErrInvalidRedirectURI},
		{name: "custom scheme", in: RegisterOAuthClientInput{Name: "Claude", RedirectURIs: []string{"javascript://x/alert"}}, wantCode: OAuthErrInvalidRedirectURI},
		{name: "fragment", in: RegisterOAuthClientInput{Name: "Claude", RedirectURIs: []string{testRedirectURI + "#x"}}, wantCode: OAuthErrInvalidRedirectURI},
		{name: "user info", in: RegisterOAuthClientInput{Name: "Claude", RedirectURIs: []string{"https://u:p@claude.ai/cb"}}, wantCode: OAuthErrInvalidRedirectURI},
		{name: "relative", in: RegisterOAuthClientInput{Name: "Claude", RedirectURIs: []string{"/cb"}}, wantCode: OAuthErrInvalidRedirectURI},
		{name: "unknown auth method", in: RegisterOAuthClientInput{Name: "Claude", RedirectURIs: []string{testRedirectURI}, AuthMethod: "private_key_jwt"}, wantCode: OAuthErrInvalidClientMetadata},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMocks(t)
			var created *model.OAuthClient
			if tt.wantCode == "" {
				m.oauth.EXPECT().CreateClient(ctx, gomock.Any()).DoAndReturn(func(_ any, c *model.OAuthClient) error { created = c; return nil })
			}
			got, err := NewOAuthService(oauthContext(t), m.tx).RegisterClient(ctx, tt.in)
			if tt.wantCode != "" {
				assertOAuthErr(t, err, tt.wantCode)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantMethod, got.AuthMethod)
			assert.Equal(t, strings.TrimSpace(tt.in.Name), created.Name)
			assert.Len(t, created.ID, 43)
			assert.Equal(t, now, created.CreatedAt)
			if tt.wantSecret {
				assert.True(t, strings.HasPrefix(got.Secret, OAuthClientSecretPrefix))
				assert.Equal(t, hash.Token(got.Secret), *created.SecretHash)
			} else {
				assert.Empty(t, got.Secret)
				assert.Nil(t, created.SecretHash)
			}
		})
	}
}

func TestOAuthService_RegisterClient_Limits(t *testing.T) {
	m := newMocks(t)
	svc := NewOAuthService(oauthContext(t), m.tx)
	in := RegisterOAuthClientInput{Name: "Claude", RedirectURIs: []string{testRedirectURI}, IP: "203.0.113.7"}
	m.oauth.EXPECT().CreateClient(ctx, gomock.Any()).Times(MaxOAuthRegistrationsPerIP).Return(nil)
	for range MaxOAuthRegistrationsPerIP {
		_, err := svc.RegisterClient(ctx, in)
		require.NoError(t, err)
	}
	_, err := svc.RegisterClient(ctx, in)
	assert.ErrorIs(t, err, ErrTooManyAttempts)

	errDB := errors.New("db down")
	m.oauth.EXPECT().CreateClient(ctx, gomock.Any()).Return(errDB)
	_, err = svc.RegisterClient(ctx, RegisterOAuthClientInput{Name: "Claude", RedirectURIs: []string{testRedirectURI}, IP: "203.0.113.8"})
	assert.ErrorIs(t, err, errDB)
}

func validAuthorize() AuthorizeRequest {
	return AuthorizeRequest{ResponseType: "code", ClientID: "client-1", RedirectURI: testRedirectURI,
		CodeChallenge: testChallenge(), CodeChallengeMethod: "S256", Resource: "https://lacis.acme.example/mcp"}
}

func TestOAuthService_CheckAuthorize(t *testing.T) {
	client := &model.OAuthClient{ID: "client-1", Name: "Claude", RedirectURIs: model.StringArray{testRedirectURI}}
	tests := []struct {
		name       string
		mutate     func(r *AuthorizeRequest)
		findErr    error
		wantClient bool
		wantCode   string
	}{
		{name: "valid", wantClient: true},
		{name: "resource with a trailing slash", mutate: func(r *AuthorizeRequest) { r.Resource += "/" }, wantClient: true},
		{name: "no resource", mutate: func(r *AuthorizeRequest) { r.Resource = "" }, wantClient: true},
		{name: "unknown client", findErr: gorm.ErrRecordNotFound, wantCode: OAuthErrInvalidClient},
		{name: "redirect URI of another", mutate: func(r *AuthorizeRequest) { r.RedirectURI = "https://evil.example/cb" }, wantCode: OAuthErrInvalidRedirectURI},
		{name: "no redirect URI", mutate: func(r *AuthorizeRequest) { r.RedirectURI = "" }, wantCode: OAuthErrInvalidRedirectURI},
		{name: "implicit flow", mutate: func(r *AuthorizeRequest) { r.ResponseType = "token" }, wantClient: true, wantCode: OAuthErrUnsupportedResponse},
		{name: "no PKCE", mutate: func(r *AuthorizeRequest) { r.CodeChallenge = "" }, wantClient: true, wantCode: OAuthErrInvalidRequest},
		{name: "plain PKCE", mutate: func(r *AuthorizeRequest) { r.CodeChallengeMethod = "plain" }, wantClient: true, wantCode: OAuthErrInvalidRequest},
		{name: "another resource", mutate: func(r *AuthorizeRequest) { r.Resource = "https://evil.example/mcp" }, wantClient: true, wantCode: OAuthErrInvalidTarget},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMocks(t)
			if tt.findErr != nil {
				m.oauth.EXPECT().FindClient(ctx, "client-1").Return(nil, tt.findErr)
			} else {
				m.oauth.EXPECT().FindClient(ctx, "client-1").Return(client, nil)
			}
			req := validAuthorize()
			if tt.mutate != nil {
				tt.mutate(&req)
			}
			got, err := NewOAuthService(oauthContext(t), m.tx).CheckAuthorize(ctx, req)
			assert.Equal(t, tt.wantClient, got != nil)
			if tt.wantCode == "" {
				assert.NoError(t, err)
				return
			}
			assertOAuthErr(t, err, tt.wantCode)
		})
	}

	m := newMocks(t)
	errDB := errors.New("db down")
	m.oauth.EXPECT().FindClient(ctx, "client-1").Return(nil, errDB)
	_, err := NewOAuthService(oauthContext(t), m.tx).CheckAuthorize(ctx, validAuthorize())
	assert.ErrorIs(t, err, errDB)
}

func TestOAuthService_Authorize(t *testing.T) {
	client := &model.OAuthClient{ID: "client-1", Name: "Claude", RedirectURIs: model.StringArray{testRedirectURI}}
	user := model.User{Base: model.Base{ID: uuid.New()}, Username: "alice"}
	tests := []struct {
		name       string
		principal  *auth.Principal
		setup      func(m *mocks)
		wantGroups model.StringArray
		wantErr    error
	}{
		{name: "no principal", wantErr: ErrUnauthorized},
		{
			name:      "groups of the session",
			principal: &auth.Principal{User: user, SessionID: "s1"},
			setup: func(m *mocks) {
				m.session.EXPECT().FindValid(ctx, "s1", now).Return(&model.Session{OIDCGroups: model.StringArray{"ops"}}, nil)
			},
			wantGroups: model.StringArray{"ops"},
		},
		{name: "no session", principal: &auth.Principal{User: user}, wantGroups: model.StringArray{}},
		{
			name:      "session gone",
			principal: &auth.Principal{User: user, SessionID: "s1"},
			setup:     func(m *mocks) { m.session.EXPECT().FindValid(ctx, "s1", now).Return(nil, gorm.ErrRecordNotFound) },
			wantErr:   ErrUnauthorized,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMocks(t)
			if tt.principal != nil {
				m.oauth.EXPECT().FindClient(ctx, "client-1").Return(client, nil)
			}
			if tt.setup != nil {
				tt.setup(m)
			}
			var stored *model.OAuthCode
			if tt.wantErr == nil {
				m.oauth.EXPECT().MarkClientAuthorized(ctx, "client-1", now).Return(nil)
				m.oauth.EXPECT().CreateCode(ctx, gomock.Any()).DoAndReturn(func(_ any, c *model.OAuthCode) error { stored = c; return nil })
			}
			code, err := NewOAuthService(oauthContext(t), m.tx).Authorize(ctx, tt.principal, validAuthorize())
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(code, OAuthCodePrefix))
			assert.Equal(t, &model.OAuthCode{ID: hash.Token(code), ClientID: "client-1", UserID: user.ID, OIDCGroups: tt.wantGroups,
				RedirectURI: testRedirectURI, CodeChallenge: testChallenge(), Scope: OAuthScopeRead, ExpiresAt: now.Add(OAuthCodeTTL)}, stored)
		})
	}
}

func TestOAuthService_Exchange(t *testing.T) {
	secret := "lacis_cs_secret"
	public := &model.OAuthClient{ID: "client-1"}
	confidential := &model.OAuthClient{ID: "client-1", SecretHash: new(hash.Token(secret))}
	userID := uuid.New()
	code := func() *model.OAuthCode {
		return &model.OAuthCode{ClientID: "client-1", UserID: userID, OIDCGroups: model.StringArray{"ops"},
			RedirectURI: testRedirectURI, CodeChallenge: testChallenge(), Scope: OAuthScopeRead}
	}
	valid := OAuthExchangeInput{OAuthClientAuth: OAuthClientAuth{ClientID: "client-1"}, Code: "lacis_ac_x",
		RedirectURI: testRedirectURI, CodeVerifier: testVerifier}
	tests := []struct {
		name     string
		client   *model.OAuthClient
		in       func(in *OAuthExchangeInput)
		code     *model.OAuthCode
		codeErr  error
		wantCode string
	}{
		{name: "public client", client: public, code: code()},
		{name: "confidential client", client: confidential, in: func(in *OAuthExchangeInput) { in.ClientSecret = secret }, code: code()},
		{name: "wrong secret", client: confidential, in: func(in *OAuthExchangeInput) { in.ClientSecret = "nope" }, wantCode: OAuthErrInvalidClient},
		{name: "unknown code", client: public, codeErr: gorm.ErrRecordNotFound, wantCode: OAuthErrInvalidGrant},
		{name: "code of another client", client: public, code: &model.OAuthCode{ClientID: "client-2", RedirectURI: testRedirectURI, CodeChallenge: testChallenge()}, wantCode: OAuthErrInvalidGrant},
		{name: "another redirect URI", client: public, in: func(in *OAuthExchangeInput) { in.RedirectURI = "https://claude.ai/other" }, code: code(), wantCode: OAuthErrInvalidGrant},
		{name: "wrong verifier", client: public, in: func(in *OAuthExchangeInput) { in.CodeVerifier = strings.Repeat("a", 43) }, code: code(), wantCode: OAuthErrInvalidGrant},
		{name: "verifier too short", client: public, in: func(in *OAuthExchangeInput) { in.CodeVerifier = "abc" }, code: code(), wantCode: OAuthErrInvalidGrant},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMocks(t)
			m.oauth.EXPECT().FindClient(ctx, "client-1").Return(tt.client, nil)
			if tt.code != nil || tt.codeErr != nil {
				m.oauth.EXPECT().TakeCode(ctx, hash.Token("lacis_ac_x"), now).Return(tt.code, tt.codeErr)
			}
			var grant *model.OAuthGrant
			if tt.wantCode == "" {
				m.oauth.EXPECT().CreateGrant(ctx, gomock.Any()).DoAndReturn(func(_ any, g *model.OAuthGrant) error { grant = g; return nil })
			}
			in := valid
			if tt.in != nil {
				tt.in(&in)
			}
			got, err := NewOAuthService(oauthContext(t), m.tx).Exchange(ctx, in)
			if tt.wantCode != "" {
				assertOAuthErr(t, err, tt.wantCode)
				return
			}
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(got.AccessToken, OAuthAccessPrefix))
			assert.True(t, strings.HasPrefix(got.RefreshToken, OAuthRefreshPrefix))
			assert.Equal(t, time.Hour, got.ExpiresIn)
			assert.Equal(t, OAuthScopeRead, got.Scope)
			assert.Equal(t, hash.Token(got.AccessToken), grant.AccessHash)
			assert.Equal(t, hash.Token(got.RefreshToken), grant.RefreshHash)
			assert.Equal(t, userID, grant.UserID)
			assert.Equal(t, model.StringArray{"ops"}, grant.OIDCGroups)
			assert.Equal(t, now.Add(time.Hour), grant.AccessExpiresAt)
			assert.Equal(t, now.Add(30*24*time.Hour), grant.ExpiresAt)
		})
	}

	t.Run("another resource", func(t *testing.T) {
		in := valid
		in.Resource = "https://evil.example/mcp"
		_, err := NewOAuthService(oauthContext(t), newMocks(t).tx).Exchange(ctx, in)
		assertOAuthErr(t, err, OAuthErrInvalidTarget)
	})
	t.Run("unknown client", func(t *testing.T) {
		m := newMocks(t)
		m.oauth.EXPECT().FindClient(ctx, "client-1").Return(nil, gorm.ErrRecordNotFound)
		_, err := NewOAuthService(oauthContext(t), m.tx).Exchange(ctx, valid)
		assertOAuthErr(t, err, OAuthErrInvalidClient)
	})
}

func TestOAuthService_Refresh(t *testing.T) {
	client := &model.OAuthClient{ID: "client-1"}
	grant := func() *model.OAuthGrant {
		return &model.OAuthGrant{Base: model.Base{ID: uuid.New()}, ClientID: "client-1", Scope: OAuthScopeRead,
			RefreshHash: hash.Token("lacis_rt_x"), ExpiresAt: now.Add(30 * time.Minute)}
	}
	in := OAuthRefreshInput{OAuthClientAuth: OAuthClientAuth{ClientID: "client-1"}, RefreshToken: "lacis_rt_x"}

	t.Run("rotates the tokens", func(t *testing.T) {
		m := newMocks(t)
		g := grant()
		m.oauth.EXPECT().FindClient(ctx, "client-1").Return(client, nil)
		m.oauth.EXPECT().FindGrantByRefresh(ctx, hash.Token("lacis_rt_x")).Return(g, false, nil)
		m.oauth.EXPECT().SaveGrant(ctx, g).Return(nil)
		got, err := NewOAuthService(oauthContext(t), m.tx).Refresh(ctx, in)
		require.NoError(t, err)
		assert.Equal(t, hash.Token("lacis_rt_x"), *g.PreviousRefreshHash)
		assert.Equal(t, hash.Token(got.RefreshToken), g.RefreshHash)
		assert.Equal(t, 30*time.Minute, got.ExpiresIn, "never past the grant")
		assert.Equal(t, now.Add(30*time.Minute), g.AccessExpiresAt)
	})
	t.Run("a refresh token used twice revokes", func(t *testing.T) {
		m := newMocks(t)
		g := grant()
		m.oauth.EXPECT().FindClient(ctx, "client-1").Return(client, nil)
		m.oauth.EXPECT().FindGrantByRefresh(ctx, hash.Token("lacis_rt_x")).Return(g, true, nil)
		m.oauth.EXPECT().DeleteGrant(ctx, g.ID).Return(nil)
		_, err := NewOAuthService(oauthContext(t), m.tx).Refresh(ctx, in)
		assertOAuthErr(t, err, OAuthErrInvalidGrant)
	})
	t.Run("grant of another client", func(t *testing.T) {
		m := newMocks(t)
		g := grant()
		g.ClientID = "client-2"
		m.oauth.EXPECT().FindClient(ctx, "client-1").Return(client, nil)
		m.oauth.EXPECT().FindGrantByRefresh(ctx, hash.Token("lacis_rt_x")).Return(g, false, nil)
		_, err := NewOAuthService(oauthContext(t), m.tx).Refresh(ctx, in)
		assertOAuthErr(t, err, OAuthErrInvalidGrant)
	})
	t.Run("grant expired", func(t *testing.T) {
		m := newMocks(t)
		g := grant()
		g.ExpiresAt = now
		m.oauth.EXPECT().FindClient(ctx, "client-1").Return(client, nil)
		m.oauth.EXPECT().FindGrantByRefresh(ctx, hash.Token("lacis_rt_x")).Return(g, false, nil)
		_, err := NewOAuthService(oauthContext(t), m.tx).Refresh(ctx, in)
		assertOAuthErr(t, err, OAuthErrInvalidGrant)
	})
	t.Run("unknown refresh token", func(t *testing.T) {
		m := newMocks(t)
		m.oauth.EXPECT().FindClient(ctx, "client-1").Return(client, nil)
		m.oauth.EXPECT().FindGrantByRefresh(ctx, hash.Token("lacis_rt_x")).Return(nil, false, gorm.ErrRecordNotFound)
		_, err := NewOAuthService(oauthContext(t), m.tx).Refresh(ctx, in)
		assertOAuthErr(t, err, OAuthErrInvalidGrant)
	})
	t.Run("another resource", func(t *testing.T) {
		bad := in
		bad.Resource = "https://evil.example"
		_, err := NewOAuthService(oauthContext(t), newMocks(t).tx).Refresh(ctx, bad)
		assertOAuthErr(t, err, OAuthErrInvalidTarget)
	})
}

func TestOAuthService_Revoke(t *testing.T) {
	client := &model.OAuthClient{ID: "client-1"}
	id := uuid.New()
	auth := OAuthClientAuth{ClientID: "client-1"}
	t.Run("access token", func(t *testing.T) {
		m := newMocks(t)
		m.oauth.EXPECT().FindClient(ctx, "client-1").Return(client, nil)
		m.oauth.EXPECT().FindGrantByAccess(ctx, hash.Token("t"), now).Return(&model.OAuthGrant{Base: model.Base{ID: id}, ClientID: "client-1"}, nil)
		m.oauth.EXPECT().DeleteGrant(ctx, id).Return(nil)
		assert.NoError(t, NewOAuthService(oauthContext(t), m.tx).Revoke(ctx, auth, "t"))
	})
	t.Run("refresh token", func(t *testing.T) {
		m := newMocks(t)
		m.oauth.EXPECT().FindClient(ctx, "client-1").Return(client, nil)
		m.oauth.EXPECT().FindGrantByAccess(ctx, hash.Token("t"), now).Return(nil, gorm.ErrRecordNotFound)
		m.oauth.EXPECT().FindGrantByRefresh(ctx, hash.Token("t")).Return(&model.OAuthGrant{Base: model.Base{ID: id}, ClientID: "client-1"}, false, nil)
		m.oauth.EXPECT().DeleteGrant(ctx, id).Return(nil)
		assert.NoError(t, NewOAuthService(oauthContext(t), m.tx).Revoke(ctx, auth, "t"))
	})
	t.Run("unknown token", func(t *testing.T) {
		m := newMocks(t)
		m.oauth.EXPECT().FindClient(ctx, "client-1").Return(client, nil)
		m.oauth.EXPECT().FindGrantByAccess(ctx, hash.Token("t"), now).Return(nil, gorm.ErrRecordNotFound)
		m.oauth.EXPECT().FindGrantByRefresh(ctx, hash.Token("t")).Return(nil, false, gorm.ErrRecordNotFound)
		assert.NoError(t, NewOAuthService(oauthContext(t), m.tx).Revoke(ctx, auth, "t"))
	})
	t.Run("token of another client", func(t *testing.T) {
		m := newMocks(t)
		m.oauth.EXPECT().FindClient(ctx, "client-1").Return(client, nil)
		m.oauth.EXPECT().FindGrantByAccess(ctx, hash.Token("t"), now).Return(&model.OAuthGrant{ClientID: "client-2"}, nil)
		assert.NoError(t, NewOAuthService(oauthContext(t), m.tx).Revoke(ctx, auth, "t"))
	})
}

func TestOAuthService_AuthenticateAccess(t *testing.T) {
	user := model.User{Base: model.Base{ID: uuid.New()}, Username: "alice"}
	token := OAuthAccessPrefix + "x"
	tests := []struct {
		name     string
		token    string
		setup    func(m *mocks)
		wantErr  error
		wantUser bool
	}{
		{name: "not an access token", token: "lacis_rt_x", wantErr: ErrUnauthorized},
		{
			name:  "unknown or expired",
			token: token,
			setup: func(m *mocks) {
				m.oauth.EXPECT().FindGrantByAccess(ctx, hash.Token(token), now).Return(nil, gorm.ErrRecordNotFound)
			},
			wantErr: ErrUnauthorized,
		},
		{
			name:  "used lately",
			token: token,
			setup: func(m *mocks) {
				m.oauth.EXPECT().FindGrantByAccess(ctx, hash.Token(token), now).Return(&model.OAuthGrant{UserID: user.ID,
					OIDCGroups: model.StringArray{"ops"}, LastUsedAt: now, AccessExpiresAt: now.Add(time.Hour)}, nil)
				m.user.EXPECT().FindByID(ctx, user.ID).Return(&user, nil)
				m.role.EXPECT().FindPermissions(ctx, user.ID, []string{"ops"}).Return(nil, nil, nil)
			},
			wantUser: true,
		},
		{
			name:  "used long ago",
			token: token,
			setup: func(m *mocks) {
				id := uuid.New()
				m.oauth.EXPECT().FindGrantByAccess(ctx, hash.Token(token), now).Return(&model.OAuthGrant{Base: model.Base{ID: id}, UserID: user.ID,
					OIDCGroups: model.StringArray{}, LastUsedAt: now.Add(-time.Hour), AccessExpiresAt: now.Add(time.Hour)}, nil)
				m.oauth.EXPECT().TouchGrant(ctx, id, now).Return(nil)
				m.user.EXPECT().FindByID(ctx, user.ID).Return(&user, nil)
				m.role.EXPECT().FindPermissions(ctx, user.ID, []string{}).Return(nil, nil, nil)
			},
			wantUser: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMocks(t)
			if tt.setup != nil {
				tt.setup(m)
			}
			p, expires, err := NewOAuthService(oauthContext(t), m.tx).AuthenticateAccess(ctx, tt.token)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "alice", p.User.Username)
			assert.Empty(t, p.SessionID)
			assert.Equal(t, now.Add(time.Hour), expires)
		})
	}
}

func TestOAuthService_Grants(t *testing.T) {
	m := newMocks(t)
	userID, grantID := uuid.New(), uuid.New()
	svc := NewOAuthService(oauthContext(t), m.tx)
	m.oauth.EXPECT().FindUserGrants(ctx, userID, now).Return([]model.OAuthGrantView{{ClientName: "Claude"}}, nil)
	grants, err := svc.Grants(ctx, userID)
	require.NoError(t, err)
	assert.Len(t, grants, 1)

	m.oauth.EXPECT().DeleteUserGrant(ctx, userID, grantID).Return(true, nil)
	assert.NoError(t, svc.RevokeGrant(ctx, userID, grantID))
	m.oauth.EXPECT().DeleteUserGrant(ctx, userID, grantID).Return(false, nil)
	assert.ErrorIs(t, svc.RevokeGrant(ctx, userID, grantID), ErrNotFound)
}

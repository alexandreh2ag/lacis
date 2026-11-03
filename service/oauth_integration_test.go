//go:build integration

package service

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOAuth_Flow(t *testing.T) {
	f := newAuthFixture(t)
	appCtx := f.svc.appCtx
	appCtx.Config.Listen.PublicURL = "https://lacis.acme.example"
	appCtx.Config.MCP.AccessTokenTTL = time.Hour
	appCtx.Config.MCP.GrantLifetime = 30 * 24 * time.Hour
	svc := NewOAuthService(appCtx, f.svc.tx)

	registered, err := svc.RegisterClient(ctx, RegisterOAuthClientInput{Name: "Claude", RedirectURIs: []string{testRedirectURI}, AuthMethod: OAuthAuthMethodSecretPost})
	require.NoError(t, err)
	client := OAuthClientAuth{ClientID: registered.Client.ID, ClientSecret: registered.Secret}

	// The admin authorizes from a session of the front.
	login, err := f.svc.Login(ctx, LoginInput{Username: "admin", Password: "correct horse", IP: "10.0.0.1"})
	require.NoError(t, err)
	p, err := f.svc.Authenticate(ctx, login.Token)
	require.NoError(t, err)
	req := validAuthorize()
	req.ClientID = registered.Client.ID
	code, err := svc.Authorize(ctx, p, req)
	require.NoError(t, err)

	tokens, err := svc.Exchange(ctx, OAuthExchangeInput{OAuthClientAuth: client, Code: code, RedirectURI: testRedirectURI, CodeVerifier: testVerifier})
	require.NoError(t, err)
	_, err = svc.Exchange(ctx, OAuthExchangeInput{OAuthClientAuth: client, Code: code, RedirectURI: testRedirectURI, CodeVerifier: testVerifier})
	assertOAuthErr(t, err, OAuthErrInvalidGrant)

	principal, expires, err := svc.AuthenticateAccess(ctx, tokens.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, "admin", principal.User.Username)
	assert.True(t, principal.Can(f.ns.ID, model.ResourceInventory, model.ActionRead))
	assert.True(t, f.clock.now.Add(time.Hour).Equal(expires))

	grants, err := svc.Grants(ctx, p.User.ID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	assert.Equal(t, "Claude", grants[0].ClientName)
	assert.Equal(t, registered.Client.ID, grants[0].ClientID)

	// Past the access token, a refresh gives new tokens; the old refresh
	// token, shown again, revokes the grant.
	f.clock.now = f.clock.now.Add(2 * time.Hour)
	_, _, err = svc.AuthenticateAccess(ctx, tokens.AccessToken)
	assert.ErrorIs(t, err, ErrUnauthorized)
	renewed, err := svc.Refresh(ctx, OAuthRefreshInput{OAuthClientAuth: client, RefreshToken: tokens.RefreshToken})
	require.NoError(t, err)
	_, _, err = svc.AuthenticateAccess(ctx, renewed.AccessToken)
	require.NoError(t, err)
	_, err = svc.Refresh(ctx, OAuthRefreshInput{OAuthClientAuth: client, RefreshToken: tokens.RefreshToken})
	assertOAuthErr(t, err, OAuthErrInvalidGrant)
	_, _, err = svc.AuthenticateAccess(ctx, renewed.AccessToken)
	assert.ErrorIs(t, err, ErrUnauthorized, "the grant is revoked")

	// A new grant, from a new session, revoked by its user.
	login, err = f.svc.Login(ctx, LoginInput{Username: "admin", Password: "correct horse", IP: "10.0.0.1"})
	require.NoError(t, err)
	p, err = f.svc.Authenticate(ctx, login.Token)
	require.NoError(t, err)
	code, err = svc.Authorize(ctx, p, req)
	require.NoError(t, err)
	tokens, err = svc.Exchange(ctx, OAuthExchangeInput{OAuthClientAuth: client, Code: code, RedirectURI: testRedirectURI, CodeVerifier: testVerifier})
	require.NoError(t, err)
	grants, err = svc.Grants(ctx, p.User.ID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.NoError(t, svc.RevokeGrant(ctx, p.User.ID, grants[0].ID))
	_, _, err = svc.AuthenticateAccess(ctx, tokens.AccessToken)
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestOAuth_Purge(t *testing.T) {
	f := newAuthFixture(t)
	appCtx := f.svc.appCtx
	appCtx.Config.Listen.PublicURL = "https://lacis.acme.example"
	appCtx.Config.MCP.AccessTokenTTL = time.Hour
	appCtx.Config.MCP.GrantLifetime = 24 * time.Hour
	svc := NewOAuthService(appCtx, f.svc.tx)
	used, err := svc.RegisterClient(ctx, RegisterOAuthClientInput{Name: "Claude", RedirectURIs: []string{testRedirectURI}, AuthMethod: "none"})
	require.NoError(t, err)
	_, err = svc.RegisterClient(ctx, RegisterOAuthClientInput{Name: "Spam", RedirectURIs: []string{testRedirectURI}, AuthMethod: "none"})
	require.NoError(t, err)

	login, err := f.svc.Login(ctx, LoginInput{Username: "bob", Password: "battery staple", IP: "10.0.0.1"})
	require.NoError(t, err)
	p, err := f.svc.Authenticate(ctx, login.Token)
	require.NoError(t, err)
	req := validAuthorize()
	req.ClientID = used.Client.ID
	code, err := svc.Authorize(ctx, p, req)
	require.NoError(t, err)
	_, err = svc.Exchange(ctx, OAuthExchangeInput{OAuthClientAuth: OAuthClientAuth{ClientID: used.Client.ID}, Code: code, RedirectURI: testRedirectURI, CodeVerifier: testVerifier})
	require.NoError(t, err)
	_, err = svc.Authorize(ctx, p, req)
	require.NoError(t, err)

	// A code expired: the exchange refuses it. The session lives an hour.
	f.clock.now = f.clock.now.Add(OAuthCodeTTL)
	code, err = svc.Authorize(ctx, p, req)
	require.NoError(t, err)
	f.clock.now = f.clock.now.Add(OAuthCodeTTL)
	_, err = svc.Exchange(ctx, OAuthExchangeInput{OAuthClientAuth: OAuthClientAuth{ClientID: used.Client.ID}, Code: code, RedirectURI: testRedirectURI, CodeVerifier: testVerifier})
	assertOAuthErr(t, err, OAuthErrInvalidGrant)

	f.clock.now = f.clock.now.Add(25 * time.Hour)
	purged, err := f.repos.OAuth.Purge(ctx, f.clock.now, f.clock.now.Add(-OAuthUnauthorizedClientTTL))
	require.NoError(t, err)
	assert.EqualValues(t, 2, purged.Codes, "the code never exchanged, the one expired")
	assert.EqualValues(t, 1, purged.Grants)
	assert.EqualValues(t, 1, purged.Clients, "the client nobody authorized")
	_, err = f.repos.OAuth.FindClient(ctx, used.Client.ID)
	assert.NoError(t, err)
}

func TestVerifyPKCE(t *testing.T) {
	sum := sha256.Sum256([]byte(testVerifier))
	assert.True(t, verifyPKCE(testVerifier, base64.RawURLEncoding.EncodeToString(sum[:])))
}

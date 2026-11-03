package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alexandreh2ag/lacis/auth"
	"github.com/alexandreh2ag/lacis/config"
	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/hash"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	// OAuthScopeRead is the only scope: reading what the user may read.
	OAuthScopeRead = "read"
	// OAuthCodeTTL is how long an authorization code may wait for its
	// exchange.
	OAuthCodeTTL = 5 * time.Minute
	// OAuthUnauthorizedClientTTL is how long a client registered stays
	// without any user authorizing it.
	OAuthUnauthorizedClientTTL = 24 * time.Hour
	// MaxOAuthRedirectURIs and MaxOAuthClientName bound a registration.
	MaxOAuthRedirectURIs = 10
	MaxOAuthClientName   = 100
	// MaxOAuthRegistrationsPerIP bounds the registrations from an IP over
	// LoginWindow: anyone on the Internet may register.
	MaxOAuthRegistrationsPerIP = 20
	// The prefixes of the secrets given out, telling them apart in a leak.
	OAuthClientSecretPrefix = "lacis_cs_"
	OAuthCodePrefix         = "lacis_ac_"
	OAuthAccessPrefix       = "lacis_at_"
	OAuthRefreshPrefix      = "lacis_rt_"

	// OAuthAuthMethodNone is a public client, proving itself by PKCE only;
	// the others send the secret given at the registration.
	OAuthAuthMethodNone        = "none"
	OAuthAuthMethodSecretPost  = "client_secret_post"
	OAuthAuthMethodSecretBasic = "client_secret_basic"
)

// The error codes of OAuth (RFC 6749, 7591).
const (
	OAuthErrInvalidRequest        = "invalid_request"
	OAuthErrInvalidClient         = "invalid_client"
	OAuthErrInvalidGrant          = "invalid_grant"
	OAuthErrUnsupportedGrantType  = "unsupported_grant_type"
	OAuthErrUnsupportedResponse   = "unsupported_response_type"
	OAuthErrInvalidTarget         = "invalid_target"
	OAuthErrAccessDenied          = "access_denied"
	OAuthErrInvalidRedirectURI    = "invalid_redirect_uri"
	OAuthErrInvalidClientMetadata = "invalid_client_metadata"
)

// OAuthError is an error OAuth tells the client, by its code.
type OAuthError struct {
	Code        string
	Description string
}

func (e *OAuthError) Error() string { return e.Code + ": " + e.Description }

func oauthErr(code, format string, args ...any) *OAuthError {
	return &OAuthError{Code: code, Description: fmt.Sprintf(format, args...)}
}

// OAuthIssuer is the identifier of the authorization server: the public
// URL, base path included.
func OAuthIssuer(listen config.ListenConfig) string {
	return listen.PublicBase()
}

// MCPResource is the URL of the MCP endpoint, the resource the tokens are
// for (RFC 8707).
func MCPResource(listen config.ListenConfig) string {
	return listen.MCPURL()
}

type RegisterOAuthClientInput struct {
	Name         string
	RedirectURIs []string
	// AuthMethod is how the client authenticates at the token endpoint;
	// empty is client_secret_basic, as RFC 7591 says.
	AuthMethod string
	// IP is the address of the client, for the limit of registrations.
	IP string
}

// RegisteredOAuthClient is a client registered; Secret, shown once, is
// empty for a public client.
type RegisteredOAuthClient struct {
	Client     model.OAuthClient
	Secret     string
	AuthMethod string
}

// AuthorizeRequest are the parameters of an authorization request.
type AuthorizeRequest struct {
	ResponseType        string
	ClientID            string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
	Scope               string
	Resource            string
}

// OAuthClientAuth is how a client authenticates at the token endpoint.
type OAuthClientAuth struct {
	ClientID     string
	ClientSecret string
}

type OAuthExchangeInput struct {
	OAuthClientAuth
	Code         string
	RedirectURI  string
	CodeVerifier string
	Resource     string
}

type OAuthRefreshInput struct {
	OAuthClientAuth
	RefreshToken string
	Resource     string
}

// OAuthTokens are the tokens a client gets.
type OAuthTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    time.Duration
	Scope        string
}

type OAuthService interface {
	// RegisterClient registers a client (RFC 7591); an *OAuthError tells
	// a metadata refused, ErrTooManyAttempts too many registrations from
	// the IP.
	RegisterClient(ctx context.Context, in RegisterOAuthClientInput) (*RegisteredOAuthClient, error)
	// CheckAuthorize checks an authorization request. The client is nil
	// when the request names no client registered, or a redirect URI not
	// its own: the error must not be sent to that URI. Otherwise the error,
	// an *OAuthError, goes back to the client at the redirect URI.
	CheckAuthorize(ctx context.Context, req AuthorizeRequest) (*model.OAuthClient, error)
	// Authorize records the consent of the principal to the request,
	// checked again, and returns the authorization code.
	Authorize(ctx context.Context, p *auth.Principal, req AuthorizeRequest) (string, error)
	// Exchange trades an authorization code for tokens; Refresh a refresh
	// token for new ones, the old ones no more valid. Their errors are
	// *OAuthError but those of the database.
	Exchange(ctx context.Context, in OAuthExchangeInput) (*OAuthTokens, error)
	Refresh(ctx context.Context, in OAuthRefreshInput) (*OAuthTokens, error)
	// Revoke ends the grant of an access or refresh token of the client
	// (RFC 7009); an unknown token is no error.
	Revoke(ctx context.Context, client OAuthClientAuth, token string) error
	// AuthenticateAccess returns the principal of an access token and when
	// the token expires; ErrUnauthorized when unknown or expired.
	AuthenticateAccess(ctx context.Context, token string) (*auth.Principal, time.Time, error)
	// Grants returns the authorizations the user gave, still valid.
	Grants(ctx context.Context, userID uuid.UUID) ([]model.OAuthGrantView, error)
	// RevokeGrant ends an authorization of the user; ErrNotFound when the
	// user has none of this id.
	RevokeGrant(ctx context.Context, userID, grantID uuid.UUID) error
}

type oauthService struct {
	appCtx        *appContext.Context
	tx            repository.Transactor
	registrations *failures
}

func NewOAuthService(appCtx *appContext.Context, tx repository.Transactor) OAuthService {
	return &oauthService{appCtx: appCtx, tx: tx, registrations: newFailures()}
}

func (s *oauthService) RegisterClient(ctx context.Context, in RegisterOAuthClientInput) (*RegisteredOAuthClient, error) {
	now := s.appCtx.Clock.Now()
	key := "ip:" + in.IP
	if s.registrations.count(key, now) >= MaxOAuthRegistrationsPerIP {
		return nil, ErrTooManyAttempts
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || utf8.RuneCountInString(name) > MaxOAuthClientName || strings.ContainsFunc(name, isControl) {
		return nil, oauthErr(OAuthErrInvalidClientMetadata, "client_name is required, at most %d characters", MaxOAuthClientName)
	}
	if len(in.RedirectURIs) == 0 || len(in.RedirectURIs) > MaxOAuthRedirectURIs {
		return nil, oauthErr(OAuthErrInvalidRedirectURI, "between 1 and %d redirect_uris are required", MaxOAuthRedirectURIs)
	}
	for _, u := range in.RedirectURIs {
		if err := checkRedirectURI(u); err != nil {
			return nil, err
		}
	}
	method := in.AuthMethod
	if method == "" {
		method = OAuthAuthMethodSecretBasic
	}
	if method != OAuthAuthMethodNone && method != OAuthAuthMethodSecretPost && method != OAuthAuthMethodSecretBasic {
		return nil, oauthErr(OAuthErrInvalidClientMetadata, "token_endpoint_auth_method must be none, client_secret_post or client_secret_basic")
	}
	id, _, err := hash.NewToken("")
	if err != nil {
		return nil, err
	}
	out := &RegisteredOAuthClient{AuthMethod: method,
		Client: model.OAuthClient{ID: id, Name: name, RedirectURIs: model.StringArray(in.RedirectURIs), CreatedAt: now}}
	if method != OAuthAuthMethodNone {
		secret, hashed, err := hash.NewToken(OAuthClientSecretPrefix)
		if err != nil {
			return nil, err
		}
		out.Secret, out.Client.SecretHash = secret, &hashed
	}
	err = s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		return repos.OAuth.CreateClient(ctx, &out.Client)
	})
	if err != nil {
		return nil, err
	}
	s.registrations.add([]string{key}, now)
	return out, nil
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

// checkRedirectURI accepts an HTTPS URL, or an HTTP one on the loopback
// for a client on the machine of the user (RFC 8252), without fragment.
func checkRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Fragment != "" || u.User != nil || strings.ContainsFunc(raw, isControl) {
		return oauthErr(OAuthErrInvalidRedirectURI, "invalid redirect URI %q", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopback(u.Hostname()) {
			return nil
		}
	}
	return oauthErr(OAuthErrInvalidRedirectURI, "redirect URI %q: HTTPS is required, HTTP only on the loopback", raw)
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

var codeChallenge = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func (s *oauthService) CheckAuthorize(ctx context.Context, req AuthorizeRequest) (*model.OAuthClient, error) {
	var client *model.OAuthClient
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		var err error
		client, err = repos.OAuth.FindClient(ctx, req.ClientID)
		return err
	})
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, oauthErr(OAuthErrInvalidClient, "unknown client")
	case err != nil:
		return nil, err
	}
	if req.RedirectURI == "" || !contains(client.RedirectURIs, req.RedirectURI) {
		return nil, oauthErr(OAuthErrInvalidRedirectURI, "redirect_uri is not one of the client")
	}
	if req.ResponseType != "code" {
		return client, oauthErr(OAuthErrUnsupportedResponse, "response_type must be code")
	}
	if req.CodeChallengeMethod != "S256" || !codeChallenge.MatchString(req.CodeChallenge) {
		return client, oauthErr(OAuthErrInvalidRequest, "PKCE is required: a code_challenge with the method S256")
	}
	if err := s.checkResource(req.Resource); err != nil {
		return client, err
	}
	return client, nil
}

func contains(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}

// checkResource accepts no resource, or the MCP endpoint, the only one.
func (s *oauthService) checkResource(resource string) error {
	if resource == "" || strings.TrimSuffix(resource, "/") == MCPResource(s.appCtx.Config.Listen) {
		return nil
	}
	return oauthErr(OAuthErrInvalidTarget, "the only resource is %s", MCPResource(s.appCtx.Config.Listen))
}

func (s *oauthService) Authorize(ctx context.Context, p *auth.Principal, req AuthorizeRequest) (string, error) {
	if p == nil {
		return "", ErrUnauthorized
	}
	if _, err := s.CheckAuthorize(ctx, req); err != nil {
		return "", err
	}
	code, hashed, err := hash.NewToken(OAuthCodePrefix)
	if err != nil {
		return "", err
	}
	now := s.appCtx.Clock.Now()
	groups, err := s.groupsOf(ctx, p)
	if err != nil {
		return "", err
	}
	err = s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		if err := repos.OAuth.MarkClientAuthorized(ctx, req.ClientID, now); err != nil {
			return err
		}
		return repos.OAuth.CreateCode(ctx, &model.OAuthCode{ID: hashed, ClientID: req.ClientID, UserID: p.User.ID,
			OIDCGroups: groups, RedirectURI: req.RedirectURI, CodeChallenge: req.CodeChallenge,
			Scope: OAuthScopeRead, ExpiresAt: now.Add(OAuthCodeTTL)})
	})
	if err != nil {
		return "", err
	}
	return code, nil
}

// groupsOf are the OIDC groups of the session of the principal: the grant
// keeps them, so that their roles apply to it.
func (s *oauthService) groupsOf(ctx context.Context, p *auth.Principal) (model.StringArray, error) {
	groups := model.StringArray{}
	if p.SessionID == "" {
		return groups, nil
	}
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		session, err := repos.Session.FindValid(ctx, p.SessionID, s.appCtx.Clock.Now())
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUnauthorized
		}
		if err != nil {
			return err
		}
		if session.OIDCGroups != nil {
			groups = session.OIDCGroups
		}
		return nil
	})
	return groups, err
}

// authenticateClient finds the client and checks its secret, if it has
// one.
func authenticateClient(ctx context.Context, repos *repository.Repositories, in OAuthClientAuth) (*model.OAuthClient, error) {
	client, err := repos.OAuth.FindClient(ctx, in.ClientID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, oauthErr(OAuthErrInvalidClient, "unknown client")
	}
	if err != nil {
		return nil, err
	}
	if client.SecretHash != nil && subtle.ConstantTimeCompare([]byte(hash.Token(in.ClientSecret)), []byte(*client.SecretHash)) != 1 {
		return nil, oauthErr(OAuthErrInvalidClient, "client authentication failed")
	}
	return client, nil
}

func (s *oauthService) Exchange(ctx context.Context, in OAuthExchangeInput) (*OAuthTokens, error) {
	if err := s.checkResource(in.Resource); err != nil {
		return nil, err
	}
	now := s.appCtx.Clock.Now()
	var client *model.OAuthClient
	var code *model.OAuthCode
	// The code is taken in a transaction of its own: a failed exchange
	// spends it all the same.
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		var err error
		if client, err = authenticateClient(ctx, repos, in.OAuthClientAuth); err != nil {
			return err
		}
		code, err = repos.OAuth.TakeCode(ctx, hash.Token(in.Code), now)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return oauthErr(OAuthErrInvalidGrant, "unknown or expired code")
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if code.ClientID != client.ID || code.RedirectURI != in.RedirectURI {
		return nil, oauthErr(OAuthErrInvalidGrant, "the code was not given to this client or redirect_uri")
	}
	if !verifyPKCE(in.CodeVerifier, code.CodeChallenge) {
		return nil, oauthErr(OAuthErrInvalidGrant, "code_verifier does not match the code_challenge")
	}
	grant := &model.OAuthGrant{ClientID: client.ID, UserID: code.UserID, OIDCGroups: code.OIDCGroups, Scope: code.Scope,
		CreatedAt: now, ExpiresAt: now.Add(s.appCtx.Config.MCP.GrantLifetime)}
	tokens, err := s.issue(grant, now)
	if err != nil {
		return nil, err
	}
	err = s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		return repos.OAuth.CreateGrant(ctx, grant)
	})
	if err != nil {
		return nil, err
	}
	return tokens, nil
}

// verifyPKCE checks the verifier of the code against its S256 challenge.
func verifyPKCE(verifier, challenge string) bool {
	if l := len(verifier); l < 43 || l > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

// issue gives the grant new tokens; the access token never outlives the
// grant.
func (s *oauthService) issue(grant *model.OAuthGrant, now time.Time) (*OAuthTokens, error) {
	access, accessHash, err := hash.NewToken(OAuthAccessPrefix)
	if err != nil {
		return nil, err
	}
	refresh, refreshHash, err := hash.NewToken(OAuthRefreshPrefix)
	if err != nil {
		return nil, err
	}
	expires := now.Add(s.appCtx.Config.MCP.AccessTokenTTL)
	if grant.ExpiresAt.Before(expires) {
		expires = grant.ExpiresAt
	}
	grant.AccessHash, grant.AccessExpiresAt, grant.RefreshHash, grant.LastUsedAt = accessHash, expires, refreshHash, now
	return &OAuthTokens{AccessToken: access, RefreshToken: refresh, ExpiresIn: expires.Sub(now), Scope: grant.Scope}, nil
}

func (s *oauthService) Refresh(ctx context.Context, in OAuthRefreshInput) (*OAuthTokens, error) {
	if err := s.checkResource(in.Resource); err != nil {
		return nil, err
	}
	now := s.appCtx.Clock.Now()
	var tokens *OAuthTokens
	var stolen *model.OAuthGrant
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		client, err := authenticateClient(ctx, repos, in.OAuthClientAuth)
		if err != nil {
			return err
		}
		grant, previous, err := repos.OAuth.FindGrantByRefresh(ctx, hash.Token(in.RefreshToken))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return oauthErr(OAuthErrInvalidGrant, "unknown refresh token")
		}
		if err != nil {
			return err
		}
		if grant.ClientID != client.ID {
			return oauthErr(OAuthErrInvalidGrant, "the refresh token was not given to this client")
		}
		if previous {
			// A refresh token already traded comes back: one of the two
			// holders stole it, the grant ends for both.
			stolen = grant
			return repos.OAuth.DeleteGrant(ctx, grant.ID)
		}
		if !now.Before(grant.ExpiresAt) {
			return oauthErr(OAuthErrInvalidGrant, "the authorization expired, authorize again")
		}
		old := grant.RefreshHash
		if tokens, err = s.issue(grant, now); err != nil {
			return err
		}
		grant.PreviousRefreshHash = &old
		return repos.OAuth.SaveGrant(ctx, grant)
	})
	if err != nil {
		return nil, err
	}
	if stolen != nil {
		s.appCtx.Logger.Warn("OAuth refresh token used twice, authorization revoked", "grant_id", stolen.ID, "client_id", stolen.ClientID, "user_id", stolen.UserID)
		return nil, oauthErr(OAuthErrInvalidGrant, "refresh token already used, the authorization is revoked")
	}
	return tokens, nil
}

func (s *oauthService) Revoke(ctx context.Context, in OAuthClientAuth, token string) error {
	now := s.appCtx.Clock.Now()
	return s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		client, err := authenticateClient(ctx, repos, in)
		if err != nil {
			return err
		}
		hashed := hash.Token(token)
		grant, err := repos.OAuth.FindGrantByAccess(ctx, hashed, now)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			grant, _, err = repos.OAuth.FindGrantByRefresh(ctx, hashed)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if grant.ClientID != client.ID {
			return nil
		}
		return repos.OAuth.DeleteGrant(ctx, grant.ID)
	})
}

func (s *oauthService) AuthenticateAccess(ctx context.Context, token string) (*auth.Principal, time.Time, error) {
	if !strings.HasPrefix(token, OAuthAccessPrefix) {
		return nil, time.Time{}, ErrUnauthorized
	}
	now := s.appCtx.Clock.Now()
	var principal *auth.Principal
	var expires time.Time
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		grant, err := repos.OAuth.FindGrantByAccess(ctx, hash.Token(token), now)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUnauthorized
		}
		if err != nil {
			return err
		}
		if now.Sub(grant.LastUsedAt) >= touchEvery {
			if err := repos.OAuth.TouchGrant(ctx, grant.ID, now); err != nil {
				return err
			}
		}
		user, err := repos.User.FindByID(ctx, grant.UserID)
		if err != nil {
			return err
		}
		ns, global, err := repos.Role.FindPermissions(ctx, user.ID, grant.OIDCGroups)
		if err != nil {
			return err
		}
		principal, expires = &auth.Principal{User: *user, Namespace: ns, Global: global}, grant.AccessExpiresAt
		return nil
	})
	if err != nil {
		return nil, time.Time{}, err
	}
	return principal, expires, nil
}

func (s *oauthService) Grants(ctx context.Context, userID uuid.UUID) ([]model.OAuthGrantView, error) {
	var grants []model.OAuthGrantView
	err := s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		var err error
		grants, err = repos.OAuth.FindUserGrants(ctx, userID, s.appCtx.Clock.Now())
		return err
	})
	return grants, err
}

func (s *oauthService) RevokeGrant(ctx context.Context, userID, grantID uuid.UUID) error {
	return s.tx.InTx(ctx, func(repos *repository.Repositories) error {
		ok, err := repos.OAuth.DeleteUserGrant(ctx, userID, grantID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrNotFound
		}
		return nil
	})
}

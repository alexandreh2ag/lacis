// Package oauth serves the OAuth 2.1 authorization server of the MCP
// endpoint: its metadata (RFC 8414, RFC 9728), the registration of the
// clients (RFC 7591), the authorization with the consent of a user logged
// in to the front, locally or by OIDC, the token endpoint and the
// revocation (RFC 7009).
package oauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	lacisAuth "github.com/alexandreh2ag/lacis/auth"
	"github.com/alexandreh2ag/lacis/common/clock"
	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/http/route/auth"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/labstack/echo/v5"
)

const (
	PathAuthorize = "oauth/authorize"
	PathRequest   = "oauth/request"
	PathToken     = "oauth/token"
	PathRegister  = "oauth/register"
	PathRevoke    = "oauth/revoke"
	// PathConsent is the page of the front asking the user to consent.
	PathConsent = "authorize"

	WellKnownAuthorizationServer = ".well-known/oauth-authorization-server"
	WellKnownOpenIDConfiguration = ".well-known/openid-configuration"
	WellKnownProtectedResource   = ".well-known/oauth-protected-resource"

	// RequestTTL is how long an authorization request waits for the
	// consent, a login included.
	RequestTTL = 15 * time.Minute
	// ResourceName names Lacis to the clients.
	ResourceName = "Lacis"
)

// ProtectedResourceMetadataURL is where the metadata of the MCP endpoint
// are, as its 401 tells: under the base path, reached whatever the reverse
// proxy forwards.
func ProtectedResourceMetadataURL(listen config.ListenConfig) string {
	return service.OAuthIssuer(listen) + "/" + WellKnownProtectedResource
}

type Handler struct {
	logger *slog.Logger
	listen config.ListenConfig
	secret []byte
	clock  clock.Clock
	oauth  service.OAuthService
}

// NewHandler serves the authorization server; secret signs the requests
// waiting for a consent.
func NewHandler(logger *slog.Logger, listen config.ListenConfig, secret string, clk clock.Clock, oauth service.OAuthService) *Handler {
	return &Handler{logger: logger, listen: listen, secret: []byte(secret), clock: clk, oauth: oauth}
}

// Register adds the routes to g, mounted on the base path, and the
// metadata at the root of e where RFC 8414 and RFC 9728 put them for a
// base path.
func (h *Handler) Register(e *echo.Echo, g *echo.Group) {
	g.GET("/"+WellKnownAuthorizationServer, h.AuthorizationServerMetadata)
	g.GET("/"+WellKnownOpenIDConfiguration, h.AuthorizationServerMetadata)
	g.GET("/"+WellKnownProtectedResource, h.ProtectedResourceMetadata)
	g.GET("/"+WellKnownProtectedResource+"/mcp", h.ProtectedResourceMetadata)
	if prefix := strings.TrimSuffix(h.listen.Prefix(), "/"); prefix != "" {
		e.GET("/"+WellKnownAuthorizationServer+prefix, h.AuthorizationServerMetadata)
		e.GET("/"+WellKnownOpenIDConfiguration+prefix, h.AuthorizationServerMetadata)
		e.GET("/"+WellKnownProtectedResource+prefix+"/mcp", h.ProtectedResourceMetadata)
	}
	g.POST("/"+PathRegister, h.RegisterClient)
	g.GET("/"+PathAuthorize, h.Authorize)
	g.GET("/"+PathRequest, h.Request)
	g.POST("/"+PathAuthorize, h.Consent)
	g.POST("/"+PathToken, h.Token)
	g.POST("/"+PathRevoke, h.Revoke)
}

type oauthError struct {
	Error       string `json:"error"`
	Description string `json:"error_description,omitempty"`
}

// metadata allows any origin: it is public, and a client in a browser
// reads it.
func metadata(c *echo.Context, v any) error {
	c.Response().Header().Set("Access-Control-Allow-Origin", "*")
	return c.JSON(http.StatusOK, v)
}

// AuthorizationServerMetadata serves RFC 8414.
func (h *Handler) AuthorizationServerMetadata(c *echo.Context) error {
	issuer := service.OAuthIssuer(h.listen)
	return metadata(c, map[string]any{
		"issuer":                                         issuer,
		"authorization_endpoint":                         issuer + "/" + PathAuthorize,
		"token_endpoint":                                 issuer + "/" + PathToken,
		"registration_endpoint":                          issuer + "/" + PathRegister,
		"revocation_endpoint":                            issuer + "/" + PathRevoke,
		"scopes_supported":                               []string{service.OAuthScopeRead},
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          authMethods,
		"revocation_endpoint_auth_methods_supported":     authMethods,
		"authorization_response_iss_parameter_supported": true,
	})
}

var authMethods = []string{service.OAuthAuthMethodNone, service.OAuthAuthMethodSecretPost, service.OAuthAuthMethodSecretBasic}

// ProtectedResourceMetadata serves RFC 9728 for the MCP endpoint.
func (h *Handler) ProtectedResourceMetadata(c *echo.Context) error {
	return metadata(c, map[string]any{
		"resource":                 service.MCPResource(h.listen),
		"authorization_servers":    []string{service.OAuthIssuer(h.listen)},
		"scopes_supported":         []string{service.OAuthScopeRead},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            ResourceName,
	})
}

type registration struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
}

type registered struct {
	registration
	ClientID              string `json:"client_id"`
	ClientSecret          string `json:"client_secret,omitempty"`
	ClientIDIssuedAt      int64  `json:"client_id_issued_at"`
	ClientSecretExpiresAt *int64 `json:"client_secret_expires_at,omitempty"`
}

// RegisterClient serves RFC 7591.
func (h *Handler) RegisterClient(c *echo.Context) error {
	if !auth.IsJSON(c.Request()) {
		return c.JSON(http.StatusBadRequest, oauthError{Error: service.OAuthErrInvalidClientMetadata, Description: "the body must be JSON"})
	}
	var req registration
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return c.JSON(http.StatusBadRequest, oauthError{Error: service.OAuthErrInvalidClientMetadata, Description: "invalid JSON body"})
	}
	if !subset(req.GrantTypes, "authorization_code", "refresh_token") || !subset(req.ResponseTypes, "code") {
		return c.JSON(http.StatusBadRequest, oauthError{Error: service.OAuthErrInvalidClientMetadata,
			Description: "only the grant types authorization_code and refresh_token, and the response type code"})
	}
	out, err := h.oauth.RegisterClient(c.Request().Context(), service.RegisterOAuthClientInput{
		Name: req.ClientName, RedirectURIs: req.RedirectURIs, AuthMethod: req.TokenEndpointAuthMethod, IP: c.RealIP()})
	if err != nil {
		return h.fail(c, "OAuth registration", err)
	}
	res := registered{ClientID: out.Client.ID, ClientSecret: out.Secret, ClientIDIssuedAt: out.Client.CreatedAt.Unix(),
		registration: registration{ClientName: out.Client.Name, RedirectURIs: out.Client.RedirectURIs, TokenEndpointAuthMethod: out.AuthMethod,
			GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"}}}
	if out.Secret != "" {
		res.ClientSecretExpiresAt = new(int64)
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.JSON(http.StatusCreated, res)
}

func subset(values []string, allowed ...string) bool {
	for _, v := range values {
		if !slices.Contains(allowed, v) {
			return false
		}
	}
	return true
}

// fail answers an error of the service: an OAuth error with its code, the
// others hidden.
func (h *Handler) fail(c *echo.Context, what string, err error) error {
	var oe *service.OAuthError
	switch {
	case errors.As(err, &oe):
		status := http.StatusBadRequest
		if oe.Code == service.OAuthErrInvalidClient {
			status = http.StatusUnauthorized
		}
		return c.JSON(status, oauthError{Error: oe.Code, Description: oe.Description})
	case errors.Is(err, service.ErrTooManyAttempts):
		c.Response().Header().Set("Retry-After", "900")
		return c.JSON(http.StatusTooManyRequests, oauthError{Error: "too_many_requests", Description: "too many registrations, try again later"})
	}
	h.logger.Error(what, "error", err)
	return c.JSON(http.StatusInternalServerError, oauthError{Error: "server_error"})
}

// request is an authorization request waiting for the consent, signed:
// the browser holds it, cannot change it.
type request struct {
	service.AuthorizeRequest
	State   string `json:"state,omitempty"`
	Expires int64  `json:"exp"`
}

func authorizeRequest(q url.Values) request {
	return request{State: q.Get("state"), AuthorizeRequest: service.AuthorizeRequest{
		ResponseType: q.Get("response_type"), ClientID: q.Get("client_id"), RedirectURI: q.Get("redirect_uri"),
		CodeChallenge: q.Get("code_challenge"), CodeChallengeMethod: q.Get("code_challenge_method"),
		Scope: q.Get("scope"), Resource: q.Get("resource")}}
}

// Authorize checks the request and sends the browser to the consent page
// of the front, which logs the user in first if needed.
func (h *Handler) Authorize(c *echo.Context) error {
	req := authorizeRequest(c.Request().URL.Query())
	client, err := h.oauth.CheckAuthorize(c.Request().Context(), req.AuthorizeRequest)
	var oe *service.OAuthError
	switch {
	case client == nil && errors.As(err, &oe):
		// Neither the client nor the redirect URI can be trusted: the
		// error stays here.
		return c.Redirect(http.StatusFound, h.listen.Prefix()+PathConsent+"?error="+url.QueryEscape(oe.Code))
	case errors.As(err, &oe):
		return c.Redirect(http.StatusFound, h.callback(req, url.Values{"error": {oe.Code}, "error_description": {oe.Description}}))
	case err != nil:
		h.logger.Error("OAuth authorize", "error", err)
		return c.Redirect(http.StatusFound, h.listen.Prefix()+PathConsent+"?error=server_error")
	}
	req.Expires = h.clock.Now().Add(RequestTTL).Unix()
	return c.Redirect(http.StatusFound, h.listen.Prefix()+PathConsent+"?request="+url.QueryEscape(h.sign(req)))
}

// callback is the redirect URI of the request with params, its state and
// the issuer (RFC 9207).
func (h *Handler) callback(req request, params url.Values) string {
	if req.State != "" {
		params.Set("state", req.State)
	}
	params.Set("iss", service.OAuthIssuer(h.listen))
	sep := "?"
	if strings.Contains(req.RedirectURI, "?") {
		sep = "&"
	}
	return req.RedirectURI + sep + params.Encode()
}

// Consent is what the consent page shows.
type Consent struct {
	ClientName   string    `json:"client_name"`
	RedirectHost string    `json:"redirect_host"`
	User         auth.User `json:"user"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// ErrorRequestExpired tells the consent page the request is no more
// valid: the user starts again from the client.
const ErrorRequestExpired = "request_expired"

// Request tells the consent page what the request asks; 401 without a
// session, the page logs the user in first.
func (h *Handler) Request(c *echo.Context) error {
	p := lacisAuth.FromContext(c.Request().Context())
	if p == nil {
		return c.JSON(http.StatusUnauthorized, errorResponse{Error: "unauthorized"})
	}
	req, ok := h.verify(c.QueryParam("request"))
	if !ok {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: ErrorRequestExpired})
	}
	client, err := h.oauth.CheckAuthorize(c.Request().Context(), req.AuthorizeRequest)
	if err != nil || client == nil {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: ErrorRequestExpired})
	}
	host := req.RedirectURI
	if u, err := url.Parse(req.RedirectURI); err == nil {
		host = u.Host
	}
	u := p.User
	return c.JSON(http.StatusOK, Consent{ClientName: client.Name, RedirectHost: host,
		User: auth.User{ID: u.ID.String(), Username: u.Username, FirstName: u.FirstName, LastName: u.LastName}})
}

type consentRequest struct {
	Request string `json:"request"`
	Approve bool   `json:"approve"`
}

type consentResponse struct {
	Redirect string `json:"redirect"`
}

// Consent records the answer of the user and gives the page where the
// browser goes back to the client, with the code or the refusal. The body
// is JSON: a cross-site form cannot send it.
func (h *Handler) Consent(c *echo.Context) error {
	if !auth.IsJSON(c.Request()) {
		return c.JSON(http.StatusUnsupportedMediaType, errorResponse{Error: "the body must be JSON"})
	}
	p := lacisAuth.FromContext(c.Request().Context())
	if p == nil {
		return c.JSON(http.StatusUnauthorized, errorResponse{Error: "unauthorized"})
	}
	var body consentRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&body); err != nil {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid JSON body"})
	}
	req, ok := h.verify(body.Request)
	if !ok {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: ErrorRequestExpired})
	}
	if !body.Approve {
		if _, err := h.oauth.CheckAuthorize(c.Request().Context(), req.AuthorizeRequest); err != nil {
			return c.JSON(http.StatusBadRequest, errorResponse{Error: ErrorRequestExpired})
		}
		return c.JSON(http.StatusOK, consentResponse{Redirect: h.callback(req, url.Values{"error": {service.OAuthErrAccessDenied}})})
	}
	code, err := h.oauth.Authorize(c.Request().Context(), p, req.AuthorizeRequest)
	var oe *service.OAuthError
	switch {
	case errors.As(err, &oe), errors.Is(err, service.ErrUnauthorized):
		return c.JSON(http.StatusBadRequest, errorResponse{Error: ErrorRequestExpired})
	case err != nil:
		h.logger.Error("OAuth consent", "error", err)
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: "internal error"})
	}
	h.logger.Info("OAuth access authorized", "user", p.User.Username, "client_id", req.ClientID)
	return c.JSON(http.StatusOK, consentResponse{Redirect: h.callback(req, url.Values{"code": {code}})})
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// clientAuth reads the client of a token or revocation request: HTTP Basic
// or the body.
func clientAuth(r *http.Request) service.OAuthClientAuth {
	if id, secret, ok := r.BasicAuth(); ok {
		// RFC 6749 2.3.1: both are form-encoded first.
		if v, err := url.QueryUnescape(id); err == nil {
			id = v
		}
		if v, err := url.QueryUnescape(secret); err == nil {
			secret = v
		}
		return service.OAuthClientAuth{ClientID: id, ClientSecret: secret}
	}
	return service.OAuthClientAuth{ClientID: r.PostForm.Get("client_id"), ClientSecret: r.PostForm.Get("client_secret")}
}

// Token serves the grants authorization_code and refresh_token.
func (h *Handler) Token(c *echo.Context) error {
	r := c.Request()
	c.Response().Header().Set("Cache-Control", "no-store")
	c.Response().Header().Set("Pragma", "no-cache")
	if err := r.ParseForm(); err != nil {
		return c.JSON(http.StatusBadRequest, oauthError{Error: service.OAuthErrInvalidRequest, Description: "invalid form body"})
	}
	form := r.PostForm
	client := clientAuth(r)
	var tokens *service.OAuthTokens
	var err error
	switch form.Get("grant_type") {
	case "authorization_code":
		tokens, err = h.oauth.Exchange(r.Context(), service.OAuthExchangeInput{OAuthClientAuth: client, Code: form.Get("code"),
			RedirectURI: form.Get("redirect_uri"), CodeVerifier: form.Get("code_verifier"), Resource: form.Get("resource")})
	case "refresh_token":
		tokens, err = h.oauth.Refresh(r.Context(), service.OAuthRefreshInput{OAuthClientAuth: client,
			RefreshToken: form.Get("refresh_token"), Resource: form.Get("resource")})
	default:
		return c.JSON(http.StatusBadRequest, oauthError{Error: service.OAuthErrUnsupportedGrantType})
	}
	if err != nil {
		return h.fail(c, "OAuth token", err)
	}
	return c.JSON(http.StatusOK, tokenResponse{AccessToken: tokens.AccessToken, TokenType: "Bearer",
		ExpiresIn: int64(tokens.ExpiresIn.Seconds()), RefreshToken: tokens.RefreshToken, Scope: tokens.Scope})
}

// Revoke serves RFC 7009: an unknown token is no error.
func (h *Handler) Revoke(c *echo.Context) error {
	r := c.Request()
	if err := r.ParseForm(); err != nil {
		return c.JSON(http.StatusBadRequest, oauthError{Error: service.OAuthErrInvalidRequest, Description: "invalid form body"})
	}
	if err := h.oauth.Revoke(r.Context(), clientAuth(r), r.PostForm.Get("token")); err != nil {
		return h.fail(c, "OAuth revoke", err)
	}
	return c.NoContent(http.StatusOK)
}

// sign writes the request and its HMAC.
func (h *Handler) sign(req request) string {
	payload, _ := json.Marshal(req)
	p := base64.RawURLEncoding.EncodeToString(payload)
	return p + "." + base64.RawURLEncoding.EncodeToString(h.mac(p))
}

// verify reads a signed request, unless changed or expired.
func (h *Handler) verify(signed string) (request, bool) {
	var req request
	p, sig, ok := strings.Cut(signed, ".")
	if !ok {
		return req, false
	}
	mac, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(mac, h.mac(p)) {
		return req, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil || json.Unmarshal(payload, &req) != nil {
		return req, false
	}
	return req, h.clock.Now().Unix() < req.Expires
}

func (h *Handler) mac(payload string) []byte {
	m := hmac.New(sha256.New, h.secret)
	m.Write([]byte("oauth-request:" + payload))
	return m.Sum(nil)
}

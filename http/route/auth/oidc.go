package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alexandreh2ag/lacis/auth/openid"
	"github.com/alexandreh2ag/lacis/common/clock"
	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/labstack/echo/v5"
	"golang.org/x/oauth2"
)

const (
	PathOIDCLogin    = "auth/oidc/login"
	PathOIDCCallback = "auth/oidc/callback"
	// FlowCookieName carries a login in progress, from the login to the
	// callback: its state, PKCE verifier, nonce and the page to go back to.
	FlowCookieName = "lacis_oidc"
	// FlowTTL is how long a login may stay at the provider.
	FlowTTL = 10 * time.Minute
)

// The errors the callback gives the login page of the front, in its error
// parameter.
const (
	ErrorOIDCUnavailable = "oidc_unavailable"
	ErrorOIDCExpired     = "oidc_expired"
	ErrorOIDCDenied      = "oidc_denied"
	ErrorOIDCFailed      = "oidc_failed"
	ErrorOIDCAccount     = "oidc_account"
)

// CallbackURL is where the provider sends the browser back: it must be
// registered at the provider.
func CallbackURL(listen config.ListenConfig) string {
	return strings.TrimSuffix(listen.PublicURL, "/") + "/" + PathOIDCCallback
}

type flow struct {
	State    string `json:"s"`
	Verifier string `json:"v"`
	Nonce    string `json:"n"`
	Redirect string `json:"r"`
	Expires  int64  `json:"e"`
}

type OIDCHandler struct {
	logger   *slog.Logger
	cfg      config.ListenConfig
	secret   []byte
	clock    clock.Clock
	provider openid.Provider
	auth     service.AuthService
}

// NewOIDCHandler serves the code flow of provider; secret signs the cookie
// of a login in progress.
func NewOIDCHandler(logger *slog.Logger, cfg config.ListenConfig, secret string, clk clock.Clock, provider openid.Provider, auth service.AuthService) *OIDCHandler {
	return &OIDCHandler{logger: logger, cfg: cfg, secret: []byte(secret), clock: clk, provider: provider, auth: auth}
}

func (h *OIDCHandler) Register(g *echo.Group) {
	g.GET("/"+PathOIDCLogin, h.Login)
	g.GET("/"+PathOIDCCallback, h.Callback)
}

// Login sends the browser to the provider; redirect is the page of the
// front to go back to, under the base path.
func (h *OIDCHandler) Login(c *echo.Context) error {
	// The provider comes back to the public URL: start there, so that the
	// cookie of the flow comes back with it.
	if public, err := url.Parse(h.cfg.PublicURL); err == nil && public.Host != "" && !strings.EqualFold(public.Host, c.Request().Host) {
		target := strings.TrimSuffix(h.cfg.PublicURL, "/") + "/" + PathOIDCLogin
		if q := c.Request().URL.RawQuery; q != "" {
			target += "?" + q
		}
		return c.Redirect(http.StatusFound, target)
	}
	f := flow{State: random(), Verifier: oauth2.GenerateVerifier(), Nonce: random(), Redirect: h.redirect(c.QueryParam("redirect")),
		Expires: h.clock.Now().Add(FlowTTL).Unix()}
	target, err := h.provider.AuthURL(c.Request().Context(), f.State, f.Verifier, f.Nonce)
	if err != nil {
		h.logger.Error("OIDC login", "error", err)
		return c.Redirect(http.StatusFound, h.loginPage(ErrorOIDCUnavailable))
	}
	c.SetCookie(h.flowCookie(h.sign(f), int(FlowTTL.Seconds())))
	return c.Redirect(http.StatusFound, target)
}

// Callback ends the flow: it checks the state, trades the code, opens the
// session and goes back to the page the login started from.
func (h *OIDCHandler) Callback(c *echo.Context) error {
	c.SetCookie(h.flowCookie("", -1))
	f, ok := h.flow(c)
	if !ok {
		return c.Redirect(http.StatusFound, h.loginPage(ErrorOIDCExpired))
	}
	if e := c.QueryParam("error"); e != "" {
		h.logger.Info("OIDC login refused by the provider", "error", e, "description", c.QueryParam("error_description"))
		return c.Redirect(http.StatusFound, h.loginPage(ErrorOIDCDenied))
	}
	if subtle.ConstantTimeCompare([]byte(c.QueryParam("state")), []byte(f.State)) != 1 {
		return c.Redirect(http.StatusFound, h.loginPage(ErrorOIDCExpired))
	}
	ctx := c.Request().Context()
	claims, err := h.provider.Exchange(ctx, c.QueryParam("code"), f.Verifier, f.Nonce)
	if err != nil {
		h.logger.Error("OIDC callback", "error", err)
		return c.Redirect(http.StatusFound, h.loginPage(ErrorOIDCFailed))
	}
	login, err := h.auth.LoginOIDC(ctx, *claims)
	switch {
	case errors.Is(err, service.ErrOIDCAccount):
		h.logger.Warn("OIDC identity without account", "subject", claims.Subject, "username", claims.Username)
		return c.Redirect(http.StatusFound, h.loginPage(ErrorOIDCAccount))
	case err != nil:
		h.logger.Error("OIDC session", "error", err)
		return c.Redirect(http.StatusFound, h.loginPage(ErrorOIDCFailed))
	}
	c.SetCookie(sessionCookie(h.cfg, login.Token, login.ExpiresAt))
	return c.Redirect(http.StatusFound, f.Redirect)
}

// redirect keeps a page of the front: a path under the base path, never
// another site.
func (h *OIDCHandler) redirect(target string) string {
	prefix := h.cfg.Prefix()
	u, err := url.Parse(target)
	if target == "" || err != nil || u.Scheme != "" || u.Host != "" || strings.HasPrefix(target, "//") ||
		strings.Contains(target, `\`) || !strings.HasPrefix(u.Path, prefix) {
		return prefix
	}
	return u.RequestURI()
}

// loginPage is the login of the front, telling why the login failed.
func (h *OIDCHandler) loginPage(code string) string {
	return h.cfg.Prefix() + "login?error=" + code
}

func (h *OIDCHandler) flowCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{Name: FlowCookieName, Value: value, Path: h.cfg.Prefix() + "auth/oidc/", MaxAge: maxAge,
		HttpOnly: true, Secure: Secure(h.cfg), SameSite: http.SameSiteLaxMode}
}

// sign writes the flow and its HMAC: the browser holds it, cannot change it.
func (h *OIDCHandler) sign(f flow) string {
	payload, _ := json.Marshal(f)
	p := base64.RawURLEncoding.EncodeToString(payload)
	return p + "." + base64.RawURLEncoding.EncodeToString(h.mac(p))
}

// flow reads the flow of the cookie, if signed and not expired.
func (h *OIDCHandler) flow(c *echo.Context) (flow, bool) {
	var f flow
	cookie, err := c.Cookie(FlowCookieName)
	if err != nil {
		return f, false
	}
	p, sig, ok := strings.Cut(cookie.Value, ".")
	if !ok {
		return f, false
	}
	mac, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(mac, h.mac(p)) {
		return f, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil || json.Unmarshal(payload, &f) != nil {
		return f, false
	}
	return f, h.clock.Now().Unix() < f.Expires
}

func (h *OIDCHandler) mac(payload string) []byte {
	m := hmac.New(sha256.New, h.secret)
	m.Write([]byte("oidc-flow:" + payload))
	return m.Sum(nil)
}

func random() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Package auth exposes the login and logout of the front, local or through
// an OIDC provider, and the middleware reading its session cookie.
package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	lacisAuth "github.com/alexandreh2ag/lacis/auth"
	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/labstack/echo/v5"
)

// CookieName is the session cookie; it holds a random token only.
const CookieName = "lacis_session"

const (
	PathLogin  = "auth/login"
	PathLogout = "auth/logout"
)

type Handler struct {
	logger *slog.Logger
	cfg    config.ListenConfig
	auth   service.AuthService
}

func NewHandler(logger *slog.Logger, cfg config.ListenConfig, auth service.AuthService) *Handler {
	return &Handler{logger: logger, cfg: cfg, auth: auth}
}

// Register adds the auth routes to the group mounted on the base path.
func (h *Handler) Register(g *echo.Group) {
	g.POST("/"+PathLogin, h.Login)
	g.POST("/"+PathLogout, h.Logout)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// User is the user logged in, as the login returns it.
type User struct {
	ID        string  `json:"id"`
	Username  string  `json:"username"`
	FirstName *string `json:"first_name,omitempty"`
	LastName  *string `json:"last_name,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// Login checks a local password, sets the session cookie and returns the
// user.
func (h *Handler) Login(c *echo.Context) error {
	if !IsJSON(c.Request()) {
		return c.JSON(http.StatusUnsupportedMediaType, errorResponse{Error: "the body must be JSON"})
	}
	var req loginRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid JSON body"})
	}
	ctx := c.Request().Context()
	login, err := h.auth.Login(ctx, service.LoginInput{Username: req.Username, Password: req.Password, IP: c.RealIP()})
	switch {
	case errors.Is(err, service.ErrUnauthorized):
		return c.JSON(http.StatusUnauthorized, errorResponse{Error: "invalid username or password"})
	case errors.Is(err, service.ErrTooManyAttempts):
		c.Response().Header().Set("Retry-After", "900")
		return c.JSON(http.StatusTooManyRequests, errorResponse{Error: "too many attempts, try again later"})
	case errors.Is(err, service.ErrLocalAuthDisabled):
		return c.JSON(http.StatusForbidden, errorResponse{Error: "local authentication is disabled"})
	case err != nil:
		h.logger.Error("login", "error", err)
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: "internal error"})
	}
	principal, err := h.auth.Authenticate(ctx, login.Token)
	if err != nil {
		h.logger.Error("load the session opened", "error", err)
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: "internal error"})
	}
	c.SetCookie(h.cookie(login.Token, login.ExpiresAt))
	u := principal.User
	return c.JSON(http.StatusOK, User{ID: u.ID.String(), Username: u.Username, FirstName: u.FirstName, LastName: u.LastName})
}

// Logout closes the session and clears the cookie.
func (h *Handler) Logout(c *echo.Context) error {
	if cookie, err := c.Cookie(CookieName); err == nil {
		if err := h.auth.Logout(c.Request().Context(), cookie.Value); err != nil {
			h.logger.Error("logout", "error", err)
			return c.JSON(http.StatusInternalServerError, errorResponse{Error: "internal error"})
		}
	}
	c.SetCookie(h.cookie("", time.Unix(0, 0)))
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) cookie(token string, expires time.Time) *http.Cookie {
	return sessionCookie(h.cfg, token, expires)
}

// sessionCookie is HttpOnly, SameSite=Lax, limited to the base path, and
// Secure unless the server is reached in plain HTTP.
func sessionCookie(cfg config.ListenConfig, token string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     cfg.Prefix(),
		Expires:  expires,
		HttpOnly: true,
		Secure:   Secure(cfg),
		SameSite: http.SameSiteLaxMode,
	}
}

// Secure tells whether the server is reached in HTTPS: it serves TLS, or
// its public URL is HTTPS, behind a proxy ending TLS.
func Secure(cfg config.ListenConfig) bool {
	return cfg.TLS.Enabled() || strings.HasPrefix(cfg.PublicURL, "https://")
}

// IsJSON tells a request whose body is JSON: a cross-site form cannot send
// one without a preflight, which blocks the simple CSRF.
func IsJSON(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get(echo.HeaderContentType))
	return err == nil && mediaType == echo.MIMEApplicationJSON
}

// Session reads the session cookie and puts its principal in the context
// of the request; a request without a valid session goes on anonymous.
func Session(logger *slog.Logger, auth service.AuthService) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			cookie, err := c.Cookie(CookieName)
			if err != nil || cookie.Value == "" {
				return next(c)
			}
			r := c.Request()
			principal, err := auth.Authenticate(r.Context(), cookie.Value)
			switch {
			case errors.Is(err, service.ErrUnauthorized):
				return next(c)
			case err != nil:
				logger.Error("authenticate session", "error", err)
				return c.JSON(http.StatusInternalServerError, errorResponse{Error: "internal error"})
			}
			c.SetRequest(r.WithContext(lacisAuth.WithPrincipal(r.Context(), principal)))
			return next(c)
		}
	}
}

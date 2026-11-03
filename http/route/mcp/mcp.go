// Package mcp serves the MCP endpoint (streamable HTTP), behind an OAuth
// access token given by the authorization server of Lacis.
package mcp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/http/route/oauth"
	"github.com/alexandreh2ag/lacis/mcpserver"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/labstack/echo/v5"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	Path = "mcp"
	// MaxBodyBytes bounds a request: a tool call is small.
	MaxBodyBytes = 1 << 20
)

// Handler builds the handler of the endpoint: the bearer token checked by
// verifier, then the server.
func Handler(logger *slog.Logger, listen config.ListenConfig, oauthService service.OAuthService, server *mcp.Server) http.Handler {
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		// No session kept between the requests: several instances of
		// Lacis serve them alike, and the tools need none.
		Stateless: true,
		Logger:    logger,
		// The protection against DNS rebinding refuses a request reaching
		// a loopback address under another name, which is what a reverse
		// proxy on the host does. A browser led there by a rebinding has
		// no access token: the bearer token protects the endpoint.
		DisableLocalhostProtection: true,
		MaxRequestBodyBytes:        MaxBodyBytes,
	})
	return auth.RequireBearerToken(Verifier(logger, oauthService), &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: oauth.ProtectedResourceMetadataURL(listen),
		Scopes:              []string{service.OAuthScopeRead},
	})(handler)
}

// Verifier checks an access token and gives its principal to the tools.
// An error of the database is logged, not told.
func Verifier(logger *slog.Logger, oauthService service.OAuthService) auth.TokenVerifier {
	return func(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		p, expires, err := oauthService.AuthenticateAccess(ctx, token)
		if errors.Is(err, service.ErrUnauthorized) {
			return nil, auth.ErrInvalidToken
		}
		if err != nil {
			logger.Error("MCP access token", "error", err)
			return nil, errors.New("internal error")
		}
		return &auth.TokenInfo{Scopes: []string{service.OAuthScopeRead}, Expiration: expires, UserID: p.User.ID.String(),
			Extra: map[string]any{mcpserver.PrincipalKey: p}}, nil
	}
}

// Register serves the endpoint at the base path; the token comes in a
// header, never in a cookie.
func Register(g *echo.Group, handler http.Handler) {
	g.Any("/"+Path, echo.WrapHandler(handler))
}

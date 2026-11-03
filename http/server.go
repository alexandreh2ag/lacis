// Package http builds the echo server of lacis-server. Every route lives in a
// group mounted on listen.base_path.
package http

import (
	stdContext "context"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/alexandreh2ag/lacis/auth/openid"
	"github.com/alexandreh2ag/lacis/common/version"
	"github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/graph/resolver"
	"github.com/alexandreh2ag/lacis/http/route/agent"
	"github.com/alexandreh2ag/lacis/http/route/auth"
	"github.com/alexandreh2ag/lacis/http/route/graphql"
	"github.com/alexandreh2ag/lacis/http/route/health"
	iconRoute "github.com/alexandreh2ag/lacis/http/route/icon"
	mcpRoute "github.com/alexandreh2ag/lacis/http/route/mcp"
	"github.com/alexandreh2ag/lacis/http/route/oauth"
	"github.com/alexandreh2ag/lacis/http/route/ui"
	"github.com/alexandreh2ag/lacis/mcpserver"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/spf13/afero"
)

// GracefulTimeout is how long a shutdown waits for in-flight requests.
const GracefulTimeout = 10 * time.Second

// The limits of the HTTP server: slow clients cannot hold the connections
// for good.
const (
	ReadHeaderTimeout = 10 * time.Second
	ReadTimeout       = 2 * time.Minute
	WriteTimeout      = 2 * time.Minute
	IdleTimeout       = 120 * time.Second
	MaxHeaderBytes    = 64 << 10
)

// FrontBodyLimit bounds the body of a request of the front: logins and
// GraphQL operations are small. An uploaded logo may take more, up to
// icons.max_size.
const FrontBodyLimit = 1 << 20

// multipartOverhead is what a multipart form adds to the logo it carries.
const multipartOverhead = 64 << 10

// Services are the services the routes call.
type Services struct {
	Agent     service.AgentService
	Snapshot  service.SnapshotService
	Auth      service.AuthService
	Namespace service.NamespaceService
	History   service.HistoryService
	Metadata  service.MetadataService
	User      service.UserService
	Role      service.RoleService
	Token     service.EnrollmentTokenService
	TechIcon  service.TechIconService
	Task      service.TaskService
	Graph     service.GraphService
	// OAuth serves the MCP endpoint and its authorization; nil when mcp
	// is off.
	OAuth service.OAuthService
	// OIDC is the provider of auth.oidc; nil when it is off.
	OIDC openid.Provider
	// UI is the built front; nil serves none.
	UI fs.FS
}

// CreateServerHTTP returns the echo instance serving every route under the
// base path.
func CreateServerHTTP(ctx *context.Context, services *Services) *echo.Echo {
	e := echo.New()
	e.Logger = ctx.Logger
	e.Use(
		middleware.RequestID(),
		requestLogger(ctx.Config.Listen.Prefix()+auth.PathOIDCCallback, ctx.Config.Listen.Prefix()+health.PathHealthz),
		middleware.Recover(),
	)
	g := e.Group(strings.TrimSuffix(ctx.Config.Listen.Prefix(), "/"))
	health.Register(g)
	agent.NewHandler(ctx.Logger, ctx.Clock, ctx.Config.AgentAPI, services.Agent, services.Snapshot).Register(g)
	front := g.Group("", middleware.BodyLimit(max(FrontBodyLimit, int64(ctx.Config.Icons.MaxSize)+multipartOverhead)),
		auth.Session(ctx.Logger, services.Auth))
	secure := auth.Secure(ctx.Config.Listen, ctx.Config.Auth.Session.CookieSecure)
	auth.NewHandler(ctx.Logger, ctx.Config.Listen, secure, services.Auth).Register(front)
	if services.OIDC != nil {
		auth.NewOIDCHandler(ctx.Logger, ctx.Config.Listen, secure, ctx.Config.Auth.Session.Secret, ctx.Clock, services.OIDC, services.Auth).Register(front)
	}
	graphql.Register(front, graphql.NewServer(ctx.Config.GraphQL, &resolver.Resolver{
		Clock: ctx.Clock, NamespaceService: services.Namespace, HistoryService: services.History,
		MetadataService: services.Metadata, UserService: services.User, RoleService: services.Role,
		EnrollmentTokenService: services.Token, AgentService: services.Agent, TechIconService: services.TechIcon,
		TaskService: services.Task, OAuthService: services.OAuth, Logger: ctx.Logger, MaxNodes: ctx.Config.Graph.MaxNodes}))
	iconRoute.NewHandler(ctx.Logger, ctx.Config.Icons, services.TechIcon).Register(front)
	if services.OAuth != nil {
		oauth.NewHandler(ctx.Logger, ctx.Config.Listen, ctx.Config.Auth.Session.Secret, ctx.Clock, services.OAuth).Register(e, front)
		server := mcpserver.New(mcpserver.Services{Namespace: services.Namespace, Graph: services.Graph, History: services.History},
			ctx.Clock, ctx.Logger, ctx.Config, version.Version)
		mcpRoute.Register(g, mcpRoute.Handler(ctx.Logger, ctx.Config.Listen, services.OAuth, server))
	}
	if services.UI != nil {
		ui.NewHandler(services.UI, ctx.Config, version.Version).Register(g)
	}
	return e
}

// requestLogger logs each request as echo does, but the query string of the
// OIDC callback: its code and state are secrets for a while. Successful
// health probes are not logged: they would drown the other requests.
func requestLogger(callbackPath, healthzPath string) echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogLatency: true, LogRemoteIP: true, LogHost: true, LogMethod: true, LogURI: true, LogRequestID: true,
		LogUserAgent: true, LogStatus: true, LogContentLength: true, LogResponseSize: true,
		// The error goes to the error handler, which sets the status logged.
		HandleError: true,
		LogValuesFunc: func(c *echo.Context, v middleware.RequestLoggerValues) error {
			attrs := []slog.Attr{
				slog.String("method", v.Method),
				slog.String("uri", maskQuery(v.URI, callbackPath)),
				slog.Int("status", v.Status),
				slog.Duration("latency", v.Latency),
				slog.String("host", v.Host),
				slog.String("bytes_in", v.ContentLength),
				slog.Int64("bytes_out", v.ResponseSize),
				slog.String("user_agent", v.UserAgent),
				slog.String("remote_ip", v.RemoteIP),
				slog.String("request_id", v.RequestID),
			}
			if v.Error == nil && v.Status < http.StatusBadRequest && c.Request().URL.Path == healthzPath {
				return nil
			}
			if v.Error == nil {
				c.Logger().LogAttrs(stdContext.Background(), slog.LevelInfo, "REQUEST", attrs...)
				return nil
			}
			attrs = append(attrs, slog.String("error", v.Error.Error()))
			c.Logger().LogAttrs(stdContext.Background(), slog.LevelError, "REQUEST_ERROR", attrs...)
			return nil
		},
	})
}

// maskQuery hides the query string of uri when its path is path.
func maskQuery(uri, path string) string {
	p, query, ok := strings.Cut(uri, "?")
	if !ok || p != path || query == "" {
		return uri
	}
	return p + "?REDACTED"
}

// configureServer sets the limits of the HTTP server.
func configureServer(s *http.Server) error {
	s.ReadHeaderTimeout = ReadHeaderTimeout
	s.ReadTimeout = ReadTimeout
	s.WriteTimeout = WriteTimeout
	s.IdleTimeout = IdleTimeout
	s.MaxHeaderBytes = MaxHeaderBytes
	return nil
}

// Start serves e until stdCtx is cancelled, then shuts down gracefully.
// onListen, when set, receives the address actually listened on.
func Start(stdCtx stdContext.Context, ctx *context.Context, e *echo.Echo, onListen func(addr net.Addr)) error {
	listen := ctx.Config.Listen
	sc := echo.StartConfig{
		Address:         listen.Address,
		HideBanner:      true,
		HidePort:        true,
		GracefulTimeout: GracefulTimeout,
		BeforeServeFunc: configureServer,
		ListenerAddrFunc: func(addr net.Addr) {
			ctx.Logger.Info("listening", "address", addr.String(), "base_path", listen.Prefix(), "tls", listen.TLS.Enabled())
			if onListen != nil {
				onListen(addr)
			}
		},
	}
	if !listen.TLS.Enabled() {
		return sc.Start(stdCtx, e)
	}
	cert, err := afero.ReadFile(ctx.Fs, listen.TLS.CertFile)
	if err != nil {
		return fmt.Errorf("read TLS certificate: %w", err)
	}
	key, err := afero.ReadFile(ctx.Fs, listen.TLS.KeyFile)
	if err != nil {
		return fmt.Errorf("read TLS key: %w", err)
	}
	return sc.StartTLS(stdCtx, e, cert, key)
}

// Package http builds the echo server of lacis-server. Every route lives in a
// group mounted on listen.base_path.
package http

import (
	stdContext "context"
	"fmt"
	"io/fs"
	"net"
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
	"github.com/alexandreh2ag/lacis/http/route/ui"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/spf13/afero"
)

// GracefulTimeout is how long a shutdown waits for in-flight requests.
const GracefulTimeout = 10 * time.Second

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
		middleware.RequestLogger(),
		middleware.Recover(),
	)
	g := e.Group(strings.TrimSuffix(ctx.Config.Listen.Prefix(), "/"))
	health.Register(g)
	agent.NewHandler(ctx.Logger, ctx.Config.AgentAPI, services.Agent, services.Snapshot).Register(g)
	front := g.Group("", auth.Session(ctx.Logger, services.Auth))
	auth.NewHandler(ctx.Logger, ctx.Config.Listen, services.Auth).Register(front)
	if services.OIDC != nil {
		auth.NewOIDCHandler(ctx.Logger, ctx.Config.Listen, ctx.Config.Auth.Session.Secret, ctx.Clock, services.OIDC, services.Auth).Register(front)
	}
	graphql.Register(front, graphql.NewServer(ctx.Config.GraphQL, &resolver.Resolver{
		Clock: ctx.Clock, NamespaceService: services.Namespace, HistoryService: services.History,
		MetadataService: services.Metadata, UserService: services.User, RoleService: services.Role,
		EnrollmentTokenService: services.Token, AgentService: services.Agent, TechIconService: services.TechIcon,
		TaskService: services.Task}))
	iconRoute.NewHandler(ctx.Logger, ctx.Config.Icons, services.TechIcon).Register(front)
	if services.UI != nil {
		ui.NewHandler(services.UI, ctx.Config, version.Version).Register(g)
	}
	return e
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

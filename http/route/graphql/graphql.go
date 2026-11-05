// Package graphql serves the GraphQL API of the front.
package graphql

import (
	"net/http"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	lacisAuth "github.com/alexandreh2ag/lacis/auth"
	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/graph"
	"github.com/alexandreh2ag/lacis/graph/resolver"
	"github.com/alexandreh2ag/lacis/http/route/auth"
	"github.com/labstack/echo/v5"
	"github.com/vektah/gqlparser/v2/ast"
)

const Path = "graphql"

// GraphCost is the complexity of a graph or an impact field, on top of the
// fields asked inside.
const GraphCost = 200

// NewServer returns the GraphQL handler: POST with a JSON body only, the
// depth and complexity of the operations limited.
func NewServer(cfg config.GraphQLConfig, r *resolver.Resolver) *handler.Server {
	gc := graph.Config{Resolvers: r, Directives: graph.DirectiveRoot{Can: r.Can, CanGlobal: r.CanGlobal}}
	// Drawing a graph or walking it costs far more than reading a field:
	// a query may hold a few of them, not hundreds.
	gc.Complexity.Namespace.Graph = func(childComplexity int, _ graph.GraphLevel, _ *graph.GraphFilter) int {
		return GraphCost + childComplexity
	}
	gc.Complexity.Namespace.Impact = func(childComplexity int, _ graph.GraphLevel, _ *graph.GraphFilter, _ string, _ graph.ImpactDirection, _ *bool) int {
		return GraphCost + childComplexity
	}
	srv := handler.New(graph.NewExecutableSchema(gc))
	srv.AddTransport(transport.POST{})
	srv.SetQueryCache(lru.New[*ast.QueryDocument](1000))
	srv.SetErrorPresenter(r.ErrorPresenter)
	if cfg.Introspection {
		srv.Use(extension.Introspection{})
	}
	srv.Use(graph.DepthLimit{Max: cfg.MaxDepth})
	srv.Use(extension.FixedComplexityLimit(cfg.MaxComplexity))
	srv.AroundOperations(resolver.AroundOperations)
	return srv
}

// Register mounts the handler on the group, which reads the session: a
// request without session gets 401, one whose body is not JSON 415.
func Register(g *echo.Group, srv http.Handler) {
	wrapped := echo.WrapHandler(srv)
	g.POST("/"+Path, func(c *echo.Context) error {
		if lacisAuth.FromContext(c.Request().Context()) == nil {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}
		if !auth.IsJSON(c.Request()) {
			return c.JSON(http.StatusUnsupportedMediaType, map[string]string{"error": "the body must be JSON"})
		}
		return wrapped(c)
	})
}

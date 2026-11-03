package graphql

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	lacisAuth "github.com/alexandreh2ag/lacis/auth"
	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/graph/resolver"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
)

func TestRegister(t *testing.T) {
	principal := &lacisAuth.Principal{User: model.User{Username: "bob"}}
	tests := []struct {
		name        string
		principal   *lacisAuth.Principal
		contentType string
		wantStatus  int
		wantBody    string
	}{
		{name: "served", principal: principal, contentType: "application/json", wantStatus: http.StatusOK, wantBody: `{"data":{"me":{"username":"bob"}}}`},
		{name: "anonymous", contentType: "application/json", wantStatus: http.StatusUnauthorized, wantBody: `{"error":"unauthorized"}`},
		{name: "form", principal: principal, contentType: "application/x-www-form-urlencoded", wantStatus: http.StatusUnsupportedMediaType, wantBody: `{"error":"the body must be JSON"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			g := e.Group("/lacis", func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c *echo.Context) error {
					if tt.principal != nil {
						r := c.Request()
						c.SetRequest(r.WithContext(lacisAuth.WithPrincipal(r.Context(), tt.principal)))
					}
					return next(c)
				}
			})
			Register(g, NewServer(config.GraphQLConfig{MaxDepth: 5, MaxComplexity: 100}, &resolver.Resolver{}))
			req := httptest.NewRequest(http.MethodPost, "/lacis/graphql", strings.NewReader(`{"query":"{ me { username } }"}`))
			req.Header.Set(echo.HeaderContentType, tt.contentType)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.JSONEq(t, tt.wantBody, rec.Body.String())
		})
	}
}

func TestNewServer_Introspection(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		srv := NewServer(config.GraphQLConfig{Introspection: enabled, MaxDepth: 10, MaxComplexity: 1000}, &resolver.Resolver{})
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"query":"{ __schema { queryType { name } } }"}`))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req = req.WithContext(lacisAuth.WithPrincipal(req.Context(), &lacisAuth.Principal{}))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		assert.Equal(t, enabled, strings.Contains(rec.Body.String(), `"queryType":{"name":"Query"}`), rec.Body.String())
	}
}

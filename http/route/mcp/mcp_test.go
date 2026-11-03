package mcp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	lacisAuth "github.com/alexandreh2ag/lacis/auth"
	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/mcpserver"
	mockService "github.com/alexandreh2ag/lacis/mocks/service"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/labstack/echo/v5"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestVerifier(t *testing.T) {
	oauth := mockService.NewMockOAuthService(gomock.NewController(t))
	verify := Verifier(discard, oauth)
	expires := time.Date(2026, 11, 2, 13, 0, 0, 0, time.UTC)
	p := &lacisAuth.Principal{User: model.User{Username: "alice"}}

	oauth.EXPECT().AuthenticateAccess(gomock.Any(), "good").Return(p, expires, nil)
	info, err := verify(context.Background(), "good", nil)
	require.NoError(t, err)
	assert.Equal(t, expires, info.Expiration)
	assert.Equal(t, []string{service.OAuthScopeRead}, info.Scopes)
	assert.Same(t, p, info.Extra[mcpserver.PrincipalKey])

	oauth.EXPECT().AuthenticateAccess(gomock.Any(), "bad").Return(nil, time.Time{}, service.ErrUnauthorized)
	_, err = verify(context.Background(), "bad", nil)
	assert.ErrorIs(t, err, auth.ErrInvalidToken)

	oauth.EXPECT().AuthenticateAccess(gomock.Any(), "x").Return(nil, time.Time{}, errors.New("db down"))
	_, err = verify(context.Background(), "x", nil)
	assert.EqualError(t, err, "internal error")
}

func TestRegister(t *testing.T) {
	e := echo.New()
	oauth := mockService.NewMockOAuthService(gomock.NewController(t))
	listen := config.ListenConfig{BasePath: "/lacis", PublicURL: "https://lacis.acme.example/lacis"}
	Register(e.Group("/lacis"), Handler(discard, listen, oauth, mcp.NewServer(&mcp.Implementation{Name: "lacis"}, nil)))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/lacis/mcp", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, `Bearer resource_metadata="https://lacis.acme.example/lacis/.well-known/oauth-protected-resource", scope="read"`, rec.Header().Get("WWW-Authenticate"))
}

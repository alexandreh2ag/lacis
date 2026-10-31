package agent

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexandreh2ag/lacis/common/api"
	"github.com/alexandreh2ag/lacis/config"
	mockService "github.com/alexandreh2ag/lacis/mocks/service"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestEnroll(t *testing.T) {
	agentID := uuid.MustParse("019a2400-0000-7000-8000-0000000000aa")
	tests := []struct {
		name          string
		authorization string
		enrollErr     error
		wantSecret    string
		wantStatus    int
		wantBody      string
	}{
		{
			name:          "enrolled",
			authorization: "Bearer lacis_et_secret",
			wantSecret:    "lacis_et_secret",
			wantStatus:    http.StatusCreated,
			wantBody:      `{"agent_id":"019a2400-0000-7000-8000-0000000000aa","agent_token":"lacis_at_secret"}`,
		},
		{name: "no header", wantStatus: http.StatusUnauthorized, wantBody: `{"code":"invalid_token","message":"missing bearer token"}`},
		{name: "not a bearer", authorization: "Basic abc", wantStatus: http.StatusUnauthorized, wantBody: `{"code":"invalid_token","message":"missing bearer token"}`},
		{name: "empty bearer", authorization: "Bearer  ", wantStatus: http.StatusUnauthorized, wantBody: `{"code":"invalid_token","message":"missing bearer token"}`},
		{
			name:          "refused token",
			authorization: "Bearer lacis_et_revoked",
			wantSecret:    "lacis_et_revoked",
			enrollErr:     service.ErrUnauthorized,
			wantStatus:    http.StatusUnauthorized,
			wantBody:      `{"code":"invalid_token","message":"unknown, expired or revoked enrollment token"}`,
		},
		{
			name:          "internal error",
			authorization: "Bearer lacis_et_secret",
			wantSecret:    "lacis_et_secret",
			enrollErr:     errors.New("db down"),
			wantStatus:    http.StatusInternalServerError,
			wantBody:      `{"code":"internal_error"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := mockService.NewMockAgentService(gomock.NewController(t))
			if tt.wantSecret != "" {
				var enrolled *service.EnrolledAgent
				if tt.enrollErr == nil {
					enrolled = &service.EnrolledAgent{Agent: &model.Agent{Base: model.Base{ID: agentID}}, Secret: "lacis_at_secret"}
				}
				svc.EXPECT().Enroll(gomock.Any(), tt.wantSecret).Return(enrolled, tt.enrollErr)
			}
			e := echo.New()
			NewHandler(discard, config.DefaultConfig().AgentAPI, svc, nil).Register(e.Group("/lacis"))

			req := httptest.NewRequest(http.MethodPost, "/lacis/"+api.PathEnroll, nil)
			if tt.authorization != "" {
				req.Header.Set(api.HeaderAuthorization, tt.authorization)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.JSONEq(t, tt.wantBody, rec.Body.String())
		})
	}
}

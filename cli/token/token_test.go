package token

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/context"
	mockService "github.com/alexandreh2ag/lacis/mocks/service"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func withService(t *testing.T, svc service.EnrollmentTokenService, err error) *bool {
	released := false
	previous := NewService
	NewService = func(*context.Context) (service.EnrollmentTokenService, func(), error) {
		return svc, func() { released = true }, err
	}
	t.Cleanup(func() { NewService = previous })
	return &released
}

func run(args ...string) (stdout, stderr string, err error) {
	cmd := GetTokenCmd(context.TestContext(nil))
	cmd.SilenceUsage = true // as under the root command
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestCreateCmd(t *testing.T) {
	expires := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		args       []string
		want       service.CreateEnrollmentTokenInput
		expiresAt  *time.Time
		wantStderr string
	}{
		{
			name:       "defaults",
			args:       []string{"create"},
			want:       service.CreateEnrollmentTokenInput{TTL: 7 * 24 * time.Hour},
			expiresAt:  &expires,
			wantStderr: "enrollment token, valid until 2026-11-03T12:00:00Z:\n",
		},
		{
			name:       "label and ttl",
			args:       []string{"create", "--label", "web", "--ttl", "1d12h"},
			want:       service.CreateEnrollmentTokenInput{Label: "web", TTL: 36 * time.Hour},
			expiresAt:  &expires,
			wantStderr: "enrollment token, valid until 2026-11-03T12:00:00Z:\n",
		},
		{
			name:       "never expires",
			args:       []string{"create", "--ttl", "0"},
			want:       service.CreateEnrollmentTokenInput{},
			wantStderr: "enrollment token, never expires:\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := mockService.NewMockEnrollmentTokenService(gomock.NewController(t))
			svc.EXPECT().Create(gomock.Any(), tt.want).Return(&service.CreatedEnrollmentToken{
				Token:  &model.EnrollmentToken{ExpiresAt: tt.expiresAt},
				Secret: "lacis_et_secret",
			}, nil)
			released := withService(t, svc, nil)

			out, errOut, err := run(tt.args...)
			require.NoError(t, err)
			assert.Equal(t, "lacis_et_secret\n", out, "the token is alone on stdout")
			assert.Equal(t, tt.wantStderr, errOut)
			assert.True(t, *released)
		})
	}
}

func TestCreateCmd_Errors(t *testing.T) {
	t.Run("invalid ttl", func(t *testing.T) {
		_, _, err := run("create", "--ttl", "soon")
		assert.ErrorContains(t, err, `"soon"`)
	})
	t.Run("service unavailable", func(t *testing.T) {
		withService(t, nil, errors.New("database down"))
		_, _, err := run("create")
		assert.ErrorContains(t, err, "database down")
	})
	t.Run("creation fails", func(t *testing.T) {
		svc := mockService.NewMockEnrollmentTokenService(gomock.NewController(t))
		svc.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil, service.ErrInvalid)
		released := withService(t, svc, nil)
		out, _, err := run("create")
		assert.ErrorIs(t, err, service.ErrInvalid)
		assert.Empty(t, out)
		assert.True(t, *released)
	})
}

func TestNewService(t *testing.T) {
	ctx := context.TestContext(nil)
	ctx.Config.Database.DSN = "postgres://nobody@127.0.0.1:1/none?connect_timeout=1"
	ctx.Config.Database.MaxOpenConns = 1
	_, _, err := NewService(ctx)
	assert.ErrorContains(t, err, "open database")
}

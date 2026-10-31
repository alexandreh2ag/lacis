package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/hash"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gorm.io/gorm"
)

func TestAgentService_Enroll(t *testing.T) {
	m := newMocks(t)
	token := &model.EnrollmentToken{Base: model.Base{ID: uuid.New()}}
	m.token.EXPECT().FindByHash(ctx, hash.Token("lacis_et_secret")).Return(token, nil)
	m.agent.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ any, a *model.Agent) error {
		a.ID = uuid.New()
		return nil
	})

	got, err := NewAgentService(testContext(t), m.token, m.agent).Enroll(ctx, "lacis_et_secret")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(got.Secret, AgentTokenPrefix))
	assert.Equal(t, hash.Token(got.Secret), got.Agent.TokenHash)
	assert.Equal(t, token.ID, got.Agent.EnrollmentTokenID)
	assert.NotEqual(t, uuid.Nil, got.Agent.ID)
}

func TestAgentService_Enroll_Errors(t *testing.T) {
	errDB := errors.New("db down")
	tests := []struct {
		name    string
		token   *model.EnrollmentToken
		findErr error
		saveErr error
		wantErr error
	}{
		{name: "unknown token", findErr: gorm.ErrRecordNotFound, wantErr: ErrUnauthorized},
		{name: "lookup fails", findErr: errDB, wantErr: errDB},
		{name: "expired token", token: &model.EnrollmentToken{ExpiresAt: new(now.Add(-time.Second))}, wantErr: ErrUnauthorized},
		{name: "revoked token", token: &model.EnrollmentToken{RevokedAt: new(now.Add(-time.Hour))}, wantErr: ErrUnauthorized},
		{name: "creation fails", token: &model.EnrollmentToken{}, saveErr: errDB, wantErr: errDB},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMocks(t)
			m.token.EXPECT().FindByHash(ctx, gomock.Any()).Return(tt.token, tt.findErr)
			if tt.saveErr != nil {
				m.agent.EXPECT().Create(ctx, gomock.Any()).Return(tt.saveErr)
			}
			_, err := NewAgentService(testContext(t), m.token, m.agent).Enroll(ctx, "lacis_et_secret")
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

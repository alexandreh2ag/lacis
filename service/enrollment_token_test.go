package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	appContext "github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/hash"
	mockClock "github.com/alexandreh2ag/lacis/mocks/common/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var now = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

func testContext(t *testing.T) *appContext.Context {
	appCtx := appContext.TestContext(nil)
	clk := mockClock.NewMockClock(gomock.NewController(t))
	clk.EXPECT().Now().AnyTimes().Return(now)
	appCtx.Clock = clk
	return appCtx
}

func TestEnrollmentTokenService_Create(t *testing.T) {
	tests := []struct {
		name        string
		in          CreateEnrollmentTokenInput
		wantLabel   *string
		wantExpires *time.Time
	}{
		{name: "label and lifetime", in: CreateEnrollmentTokenInput{Label: " web servers ", TTL: 7 * 24 * time.Hour}, wantLabel: new("web servers"), wantExpires: new(now.Add(7 * 24 * time.Hour))},
		{name: "never expires", in: CreateEnrollmentTokenInput{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMocks(t)
			m.token.EXPECT().Create(ctx, gomock.Any()).Return(nil)

			got, err := NewEnrollmentTokenService(testContext(t), m.token, m.agent).Create(ctx, tt.in)
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(got.Secret, EnrollmentTokenPrefix))
			assert.Equal(t, hash.Token(got.Secret), got.Token.TokenHash)
			assert.Equal(t, tt.wantExpires, got.Token.ExpiresAt)
			assert.Equal(t, tt.wantLabel, got.Token.Label)
		})
	}
}

func TestEnrollmentTokenService_Create_Errors(t *testing.T) {
	errDB := errors.New("db down")
	tests := []struct {
		name    string
		in      CreateEnrollmentTokenInput
		setup   func(m *mocks)
		wantErr error
		wantMsg string
	}{
		{name: "negative lifetime", in: CreateEnrollmentTokenInput{TTL: -time.Hour}, wantErr: ErrInvalid, wantMsg: "lifetime"},
		{
			name:    "creation fails",
			in:      CreateEnrollmentTokenInput{TTL: time.Hour},
			setup:   func(m *mocks) { m.token.EXPECT().Create(ctx, gomock.Any()).Return(errDB) },
			wantErr: errDB,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMocks(t)
			if tt.setup != nil {
				tt.setup(m)
			}
			_, err := NewEnrollmentTokenService(testContext(t), m.token, m.agent).Create(ctx, tt.in)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.ErrorContains(t, err, tt.wantMsg)
		})
	}
}

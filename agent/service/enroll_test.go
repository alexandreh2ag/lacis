package service

import (
	"errors"
	"testing"

	"github.com/alexandreh2ag/lacis/agent/context"
	mockClient "github.com/alexandreh2ag/lacis/agent/mocks/client"
	"github.com/alexandreh2ag/lacis/agent/state"
	"github.com/alexandreh2ag/lacis/common/api"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func enrollContext(token string) *context.Context {
	ctx := context.TestContext(nil)
	ctx.Config.Auth.StateDir = "/var/lib/lacis"
	ctx.Config.Auth.EnrollmentToken = token
	return ctx
}

func TestEnroll(t *testing.T) {
	ctx := enrollContext("lacis_et_secret")
	c := mockClient.NewMockClient(gomock.NewController(t))
	c.EXPECT().Enroll(gomock.Any(), "lacis_et_secret").Return(&api.EnrollResponse{AgentID: "019a", AgentToken: "lacis_at_secret"}, nil)

	id, enrolled, err := Enroll(t.Context(), ctx, c)
	require.NoError(t, err)
	assert.True(t, enrolled)
	want := &state.Identity{AgentID: "019a", AgentToken: "lacis_at_secret"}
	assert.Equal(t, want, id)

	// Enrolled once: the identity is kept and the server is not called.
	id, enrolled, err = Enroll(t.Context(), ctx, c)
	require.NoError(t, err)
	assert.False(t, enrolled)
	assert.Equal(t, want, id)
}

func TestEnroll_Errors(t *testing.T) {
	t.Run("no enrollment token", func(t *testing.T) {
		_, _, err := Enroll(t.Context(), enrollContext(""), nil)
		assert.ErrorIs(t, err, ErrNoEnrollmentToken)
	})
	t.Run("invalid identity", func(t *testing.T) {
		ctx := enrollContext("lacis_et_secret")
		require.NoError(t, afero.WriteFile(ctx.Fs, "/var/lib/lacis/"+state.IdentityFile, []byte("{"), 0o600))
		_, _, err := Enroll(t.Context(), ctx, nil)
		assert.ErrorContains(t, err, "decode identity")
	})
	t.Run("refused", func(t *testing.T) {
		c := mockClient.NewMockClient(gomock.NewController(t))
		c.EXPECT().Enroll(gomock.Any(), gomock.Any()).Return(nil, errors.New("server answered 401 invalid_token"))
		_, _, err := Enroll(t.Context(), enrollContext("lacis_et_secret"), c)
		assert.ErrorContains(t, err, "enroll: server answered 401")
	})
	t.Run("identity not kept", func(t *testing.T) {
		ctx := enrollContext("lacis_et_secret")
		ctx.Fs = afero.NewReadOnlyFs(afero.NewMemMapFs())
		c := mockClient.NewMockClient(gomock.NewController(t))
		c.EXPECT().Enroll(gomock.Any(), gomock.Any()).Return(&api.EnrollResponse{AgentID: "019a", AgentToken: "t"}, nil)
		_, _, err := Enroll(t.Context(), ctx, c)
		assert.ErrorContains(t, err, "agent 019a enrolled but its identity was not kept")
	})
}

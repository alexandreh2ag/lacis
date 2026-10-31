package cli

import (
	"errors"
	"testing"

	"github.com/alexandreh2ag/lacis/agent/client"
	"github.com/alexandreh2ag/lacis/agent/context"
	mockClient "github.com/alexandreh2ag/lacis/agent/mocks/client"
	"github.com/alexandreh2ag/lacis/agent/state"
	"github.com/alexandreh2ag/lacis/common/api"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const enrollYAML = validYAML + `
auth:
  enrollment_token: lacis_et_secret
  state_dir: /var/lib/lacis
`

func withClient(t *testing.T, c client.Client, err error) {
	previous := NewClient
	NewClient = func(*context.Context) (client.Client, error) { return c, err }
	t.Cleanup(func() { NewClient = previous })
}

func TestEnrollCmd(t *testing.T) {
	c := mockClient.NewMockClient(gomock.NewController(t))
	c.EXPECT().Enroll(gomock.Any(), "lacis_et_secret").Return(&api.EnrollResponse{AgentID: "019a", AgentToken: "lacis_at_secret"}, nil)
	withClient(t, c, nil)
	ctx := context.TestContext(nil)
	require.NoError(t, afero.WriteFile(ctx.Fs, "/etc/lacis/agent.yaml", []byte(enrollYAML), 0o600))

	out, err := run(ctx, "enroll")
	require.NoError(t, err)
	assert.Equal(t, "agent 019a enrolled\n", out)
	id, err := state.Load(ctx.Fs, "/var/lib/lacis")
	require.NoError(t, err)
	assert.Equal(t, "lacis_at_secret", id.AgentToken)

	out, err = run(ctx, "enroll")
	require.NoError(t, err)
	assert.Equal(t, "agent 019a already enrolled\n", out)
}

func TestEnrollCmd_Errors(t *testing.T) {
	t.Run("client", func(t *testing.T) {
		withClient(t, nil, errors.New("read CA file"))
		ctx := context.TestContext(nil)
		require.NoError(t, afero.WriteFile(ctx.Fs, "/etc/lacis/agent.yaml", []byte(enrollYAML), 0o600))
		_, err := run(ctx, "enroll")
		assert.ErrorContains(t, err, "read CA file")
	})
	t.Run("no enrollment token", func(t *testing.T) {
		withClient(t, nil, nil)
		ctx := context.TestContext(nil)
		require.NoError(t, afero.WriteFile(ctx.Fs, "/etc/lacis/agent.yaml", []byte(validYAML), 0o600))
		_, err := run(ctx, "enroll")
		assert.ErrorContains(t, err, "auth.enrollment_token is not set")
	})
}

func TestNewClient(t *testing.T) {
	ctx := context.TestContext(nil)
	ctx.Config.Server.URL = "https://lacis.example.com"
	c, err := NewClient(ctx)
	require.NoError(t, err)
	assert.NotNil(t, c)
}

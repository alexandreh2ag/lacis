package cli

import (
	"bytes"
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/agent/client"
	"github.com/alexandreh2ag/lacis/agent/context"
	mockClient "github.com/alexandreh2ag/lacis/agent/mocks/client"
	"github.com/alexandreh2ag/lacis/agent/service"
	"github.com/alexandreh2ag/lacis/agent/state"
	"github.com/alexandreh2ag/lacis/common/api"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestStartCmd(t *testing.T) {
	logs := &bytes.Buffer{}
	ctx := context.TestContext(logs)
	require.NoError(t, afero.WriteFile(ctx.Fs, "/etc/lacis/agent.yaml", []byte(enrollYAML), 0o600))
	require.NoError(t, afero.WriteFile(ctx.Fs, service.HostnameFile, []byte("host\n"), 0o644))
	require.NoError(t, state.Save(ctx.Fs, "/var/lib/lacis", &state.Identity{AgentID: "019a", AgentToken: "lacis_at_secret"}))

	sent := make(chan struct{})
	c := mockClient.NewMockClient(gomock.NewController(t))
	c.EXPECT().SendSnapshot(gomock.Any(), "lacis_at_secret", gomock.Any()).DoAndReturn(func(any, string, any) (api.SnapshotStatus, error) {
		close(sent)
		return api.SnapshotAccepted, nil
	})
	withClient(t, c, nil)

	errs := make(chan error, 1)
	go func() {
		_, err := run(ctx, "start")
		errs <- err
	}()
	select {
	case <-sent:
	case <-time.After(5 * time.Second):
		t.Fatal("no snapshot sent")
	}
	ctx.Signal() <- syscall.SIGTERM
	select {
	case err := <-errs:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("the agent did not stop")
	}
	assert.Contains(t, logs.String(), "shutting down")
	assert.Contains(t, logs.String(), "agent stopped")
}

func TestStartCmd_Errors(t *testing.T) {
	t.Run("client", func(t *testing.T) {
		withClient(t, nil, errors.New("read CA file"))
		ctx := context.TestContext(nil)
		require.NoError(t, afero.WriteFile(ctx.Fs, "/etc/lacis/agent.yaml", []byte(enrollYAML), 0o600))
		_, err := run(ctx, "start")
		assert.ErrorContains(t, err, "read CA file")
	})
	t.Run("token refused", func(t *testing.T) {
		c := mockClient.NewMockClient(gomock.NewController(t))
		c.EXPECT().Enroll(gomock.Any(), gomock.Any()).Return(nil, &client.Error{Status: 401})
		withClient(t, c, nil)
		ctx := context.TestContext(nil)
		require.NoError(t, afero.WriteFile(ctx.Fs, "/etc/lacis/agent.yaml", []byte(enrollYAML), 0o600))
		_, err := run(ctx, "start")
		assert.ErrorIs(t, err, service.ErrTokenRefused)
	})
}

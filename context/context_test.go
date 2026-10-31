package context

import (
	"bytes"
	"encoding/json"
	"os"
	"syscall"
	"testing"

	"github.com/alexandreh2ag/lacis/config"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultContext(t *testing.T) {
	ctx := DefaultContext()
	assert.IsType(t, &afero.OsFs{}, ctx.Fs)
	assert.Equal(t, os.Stderr, ctx.LogOutput)
	assert.NotNil(t, ctx.Clock)
	assert.Equal(t, config.DefaultConfig(), ctx.Config)
}

func TestTestContext(t *testing.T) {
	logs := &bytes.Buffer{}
	ctx := TestContext(logs)
	assert.IsType(t, &afero.MemMapFs{}, ctx.Fs)

	ctx.Logger.Info("hello")
	assert.Contains(t, logs.String(), "msg=hello")

	assert.NotNil(t, TestContext(nil).Logger)
}

func TestContext_SetLogFormat(t *testing.T) {
	logs := &bytes.Buffer{}
	ctx := TestContext(logs)

	ctx.SetLogFormat(config.LogFormatJSON)
	ctx.Logger.Info("hello")
	line := map[string]any{}
	require.NoError(t, json.Unmarshal(logs.Bytes(), &line))
	assert.Equal(t, "hello", line["msg"])

	logs.Reset()
	ctx.SetLogFormat(config.LogFormatText)
	ctx.Logger.Info("again")
	assert.Contains(t, logs.String(), "msg=again")
}

func TestContext_CancelAndSignal(t *testing.T) {
	ctx := TestContext(nil)
	ctx.Signal() <- os.Signal(syscall.SIGTERM)
	assert.Equal(t, syscall.SIGTERM, <-ctx.Signal())

	ctx.Cancel()
	_, open := <-ctx.Done()
	assert.False(t, open)
}

package context

import (
	"bytes"
	"os"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

func TestDefaultContext(t *testing.T) {
	ctx := DefaultContext()
	assert.IsType(t, &afero.OsFs{}, ctx.Fs)
	assert.NotNil(t, ctx.Executor)
	assert.NotNil(t, ctx.Clock)
	assert.NotNil(t, ctx.Config)
}

func TestTestContext(t *testing.T) {
	logs := &bytes.Buffer{}
	ctx := TestContext(logs)
	assert.IsType(t, &afero.MemMapFs{}, ctx.Fs)

	ctx.Logger.Info("hello")
	assert.Contains(t, logs.String(), "msg=hello")

	assert.NotNil(t, TestContext(nil).Logger)
}

func TestContext_CancelAndSignal(t *testing.T) {
	ctx := TestContext(nil)
	ctx.Signal() <- os.Signal(syscall.SIGTERM)
	assert.Equal(t, syscall.SIGTERM, <-ctx.Signal())

	ctx.Cancel()
	_, open := <-ctx.Done()
	assert.False(t, open)
}

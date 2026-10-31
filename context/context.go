// Package context builds the application context of the server: the
// configuration and the dependencies shared by the commands, HTTP and the
// workers.
package context

import (
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexandreh2ag/lacis/common/clock"
	"github.com/alexandreh2ag/lacis/config"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/afero"
)

type Context struct {
	Logger    *slog.Logger
	LogLevel  *slog.LevelVar
	Config    *config.Config
	Validator *validator.Validate
	Fs        afero.Fs
	Clock     clock.Clock

	// LogOutput is where Logger writes; SetLogFormat keeps it.
	LogOutput io.Writer

	sigs chan os.Signal
	done chan bool
}

func (c *Context) Cancel() {
	close(c.done)
}

func (c *Context) Done() <-chan bool {
	return c.done
}

func (c *Context) Signal() chan os.Signal {
	return c.sigs
}

// SetLogFormat replaces the logger handler by a text or JSON one, keeping
// the output and the level.
func (c *Context) SetLogFormat(format string) {
	opts := &slog.HandlerOptions{Level: c.LogLevel}
	if format == config.LogFormatJSON {
		c.Logger = slog.New(slog.NewJSONHandler(c.LogOutput, opts))
		return
	}
	c.Logger = slog.New(slog.NewTextHandler(c.LogOutput, opts))
}

func newContext(out io.Writer, fs afero.Fs, sigs chan os.Signal) *Context {
	level := &slog.LevelVar{}
	level.Set(slog.LevelInfo)
	ctx := &Context{
		LogLevel:  level,
		LogOutput: out,
		Config:    config.DefaultConfig(),
		Validator: validator.New(validator.WithRequiredStructEnabled()),
		Fs:        fs,
		Clock:     clock.New(),
		sigs:      sigs,
		done:      make(chan bool),
	}
	ctx.SetLogFormat(config.LogFormatText)
	return ctx
}

// DefaultContext wires the real implementations.
func DefaultContext() *Context {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	return newContext(os.Stderr, afero.NewOsFs(), sigs)
}

// TestContext wires an in-memory file system and logs to logs (discarded
// when nil).
func TestContext(logs io.Writer) *Context {
	if logs == nil {
		logs = io.Discard
	}
	return newContext(logs, afero.NewMemMapFs(), make(chan os.Signal, 1))
}

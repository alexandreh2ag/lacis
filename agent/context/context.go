// Package context builds the application context of the agent: the
// configuration and every dependency on the system, injected so that tests
// can replace them.
package context

import (
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexandreh2ag/lacis/agent/config"
	"github.com/alexandreh2ag/lacis/agent/exec"
	"github.com/alexandreh2ag/lacis/agent/kernel"
	"github.com/alexandreh2ag/lacis/common/clock"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/afero"
)

type Context struct {
	Logger    *slog.Logger
	LogLevel  *slog.LevelVar
	Config    *config.Config
	Validator *validator.Validate
	// Fs reads /proc, /sys, /etc, the configs and the .env files.
	Fs       afero.Fs
	Executor exec.Executor
	// Kernel gives the network interfaces and the file system usage.
	Kernel kernel.Kernel
	Clock  clock.Clock

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

// DefaultContext wires the real implementations.
func DefaultContext() *Context {
	level := &slog.LevelVar{}
	level.Set(slog.LevelInfo)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	return &Context{
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})),
		LogLevel:  level,
		Config:    config.DefaultConfig(),
		Validator: validator.New(validator.WithRequiredStructEnabled()),
		Fs:        afero.NewOsFs(),
		Executor:  exec.NewOsExecutor(),
		Kernel:    kernel.New(),
		Clock:     clock.New(),
		sigs:      sigs,
		done:      make(chan bool),
	}
}

// TestContext wires an in-memory file system and logs to logs (discarded
// when nil). Tests replace the other dependencies with mocks.
func TestContext(logs io.Writer) *Context {
	if logs == nil {
		logs = io.Discard
	}
	level := &slog.LevelVar{}
	level.Set(slog.LevelInfo)
	return &Context{
		Logger:    slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: level})),
		LogLevel:  level,
		Config:    config.DefaultConfig(),
		Validator: validator.New(validator.WithRequiredStructEnabled()),
		Fs:        afero.NewMemMapFs(),
		Executor:  exec.NewOsExecutor(),
		Kernel:    kernel.New(),
		Clock:     clock.New(),
		sigs:      make(chan os.Signal, 1),
		done:      make(chan bool),
	}
}

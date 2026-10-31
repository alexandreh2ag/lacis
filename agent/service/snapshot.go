// Package service assembles the snapshot of the host.
package service

import (
	stdContext "context"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/collector"
	// Registers the collectors.
	_ "github.com/alexandreh2ag/lacis/agent/collector/apps"
	_ "github.com/alexandreh2ag/lacis/agent/collector/docker"
	_ "github.com/alexandreh2ag/lacis/agent/collector/envfiles"
	_ "github.com/alexandreh2ag/lacis/agent/collector/host"
	_ "github.com/alexandreh2ag/lacis/agent/collector/runtimes"
	_ "github.com/alexandreh2ag/lacis/agent/collector/services"
	_ "github.com/alexandreh2ag/lacis/agent/collector/webservers"
	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/alexandreh2ag/lacis/common/version"
	"github.com/spf13/afero"
)

// HostnameFile is read rather than calling os.Hostname, so that the
// hostname comes from the context file system like every other system value.
const HostnameFile = "/proc/sys/kernel/hostname"

var ErrNoHostname = errors.New("unable to determine the hostname")

// Hostname returns the hostname override of the configuration, or the
// hostname of the machine.
func Hostname(ctx *context.Context) (string, error) {
	if ctx.Config.Hostname != "" {
		return ctx.Config.Hostname, nil
	}
	b, err := afero.ReadFile(ctx.Fs, HostnameFile)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrNoHostname, err)
	}
	hostname := strings.TrimSpace(string(b))
	if hostname == "" {
		return "", ErrNoHostname
	}
	return hostname, nil
}

// Collect builds a complete snapshot of the host with every collector. It
// returns the error of stdCtx when stdCtx is done before the end, rather
// than a partial snapshot.
func Collect(stdCtx stdContext.Context, ctx *context.Context) (*snapshot.Snapshot, error) {
	start := ctx.Clock.Now()
	hostname, err := Hostname(ctx)
	if err != nil {
		return nil, err
	}
	sys, err := system.Load(ctx)
	if err != nil {
		return nil, err
	}
	collectors, err := collector.CreateCollectors(ctx)
	if err != nil {
		return nil, err
	}
	agent := snapshot.AgentInfo{
		Version: version.Version,
		Commit:  version.Commit,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}
	s := snapshot.New(agent, ctx.Config.Labels, hostname)
	collector.Run(stdCtx, ctx, sys, collectors, s)
	if err := stdCtx.Err(); err != nil {
		return nil, err
	}
	s.CollectedAt = start.UTC()
	s.DurationMs = ctx.Clock.Now().Sub(start).Milliseconds()
	return s, nil
}

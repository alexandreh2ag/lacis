// Package cli holds the cobra commands of lacis-agent.
package cli

import (
	"fmt"
	"log/slog"

	"github.com/alexandreh2ag/lacis/agent/config"
	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/spf13/cobra"
)

const (
	Name = "lacis-agent"

	FlagConfig   = "config"
	FlagLogLevel = "level"
)

func GetRootCmd(ctx *context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:               Name,
		Short:             fmt.Sprintf("%s: inventories this host and pushes it to lacis-server", Name),
		SilenceUsage:      true,
		PersistentPreRunE: GetRootPreRunEFn(ctx, true),
	}
	cmd.PersistentFlags().StringP(FlagConfig, "c", config.DefaultDir, "config file, or directory holding agent.yaml or agent.yml")
	cmd.PersistentFlags().StringP(FlagLogLevel, "l", "INFO", "log level: DEBUG, INFO, WARN, ERROR")

	cmd.AddCommand(
		GetVersionCmd(),
		GetCollectCmd(ctx),
		GetEnrollCmd(ctx),
		GetStartCmd(ctx),
	)
	return cmd
}

// GetRootPreRunEFn sets the log level, then loads the configuration into the
// context and, when validate is true, rejects an invalid one.
func GetRootPreRunEFn(ctx *context.Context, validate bool) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		levelStr, _ := cmd.Flags().GetString(FlagLogLevel)
		level := slog.LevelInfo
		if err := level.UnmarshalText([]byte(levelStr)); err != nil {
			return fmt.Errorf("invalid log level %q: %w", levelStr, err)
		}
		ctx.LogLevel.Set(level)

		path, _ := cmd.Flags().GetString(FlagConfig)
		file, ignored, err := config.FindFile(ctx.Fs, path, config.Name)
		if err != nil {
			return err
		}
		for _, f := range ignored {
			ctx.Logger.Warn("config file ignored, another one has priority", "ignored", f, "used", file)
		}
		if file == "" {
			ctx.Logger.Debug("no config file found, using defaults and environment", "path", path)
		} else {
			ctx.Logger.Debug("using config file", "file", file)
		}
		cfg, err := config.Load(ctx.Fs, file)
		if err != nil {
			return err
		}
		ctx.Config = cfg
		if cfg.Server.Insecure {
			ctx.Logger.Warn("server certificate verification is disabled (server.insecure), do not use in production")
		}
		if validate {
			if err := cfg.Validate(ctx.Validator); err != nil {
				return fmt.Errorf("invalid configuration:\n%w", err)
			}
		}
		return nil
	}
}

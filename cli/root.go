// Package cli holds the cobra commands of lacis-server.
package cli

import (
	"fmt"
	"log/slog"

	"github.com/alexandreh2ag/lacis/cli/db"
	"github.com/alexandreh2ag/lacis/cli/graph"
	"github.com/alexandreh2ag/lacis/cli/history"
	"github.com/alexandreh2ag/lacis/cli/snapshot"
	"github.com/alexandreh2ag/lacis/cli/token"
	"github.com/alexandreh2ag/lacis/cli/user"
	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/context"
	"github.com/spf13/cobra"
)

const (
	Name = "lacis-server"

	FlagConfig   = "config"
	FlagLogLevel = "level"
)

func GetRootCmd(ctx *context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:               Name,
		Short:             fmt.Sprintf("%s: infrastructure inventory and dependency graph", Name),
		SilenceUsage:      true,
		PersistentPreRunE: GetRootPreRunEFn(ctx, true),
	}
	cmd.PersistentFlags().StringP(FlagConfig, "c", config.DefaultDir, "config file, or directory holding server.yaml or server.yml")
	cmd.PersistentFlags().StringP(FlagLogLevel, "l", "", "log level: DEBUG, INFO, WARN, ERROR (overrides log.level)")

	cmd.AddCommand(
		GetVersionCmd(),
		GetValidateCmd(ctx),
		GetStartCmd(ctx),
		GetWorkerCmd(ctx),
		db.GetDBCmd(ctx),
		user.GetUserCmd(ctx),
		token.GetTokenCmd(ctx),
		snapshot.GetSnapshotCmd(ctx),
		graph.GetGraphCmd(ctx),
		history.GetHistoryCmd(ctx),
	)
	return cmd
}

// GetRootPreRunEFn loads the configuration into the context, configures the
// logger and, when validate is true, rejects an invalid configuration.
func GetRootPreRunEFn(ctx *context.Context, validate bool) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		path, _ := cmd.Flags().GetString(FlagConfig)
		file, ignored, err := config.FindFile(ctx.Fs, path, config.Name)
		if err != nil {
			return err
		}
		cfg, err := config.Load(ctx.Fs, file)
		if err != nil {
			return err
		}
		ctx.Config = cfg

		levelStr := cfg.Log.Level
		if flag, _ := cmd.Flags().GetString(FlagLogLevel); flag != "" {
			levelStr = flag
		}
		level := slog.LevelInfo
		if err := level.UnmarshalText([]byte(levelStr)); err != nil {
			return fmt.Errorf("invalid log level %q: %w", levelStr, err)
		}
		ctx.LogLevel.Set(level)
		ctx.SetLogFormat(cfg.Log.Format)

		for _, f := range ignored {
			ctx.Logger.Warn("config file ignored, another one has priority", "ignored", f, "used", file)
		}
		if file == "" {
			ctx.Logger.Debug("no config file found, using defaults and environment", "path", path)
		} else {
			ctx.Logger.Debug("using config file", "file", file)
		}

		if validate {
			if err := cfg.Validate(ctx.Validator); err != nil {
				return fmt.Errorf("invalid configuration:\n%w", err)
			}
		}
		return nil
	}
}

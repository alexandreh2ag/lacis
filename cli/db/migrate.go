// Package db holds the "db" subcommands of lacis-server.
package db

import (
	"github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/database"
	"github.com/spf13/cobra"
)

// NewMigrator builds the migrator used by "db migrate"; tests replace it.
var NewMigrator = database.NewMigrator

func GetDBCmd(ctx *context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Database commands",
	}
	cmd.AddCommand(GetMigrateCmd(ctx))
	return cmd
}

func GetMigrateCmd(ctx *context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply the pending migrations of Lacis, then those of River",
		RunE:  GetMigrateRunFn(ctx),
	}
}

func GetMigrateRunFn(ctx *context.Context) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		return NewMigrator(ctx.Config.Database.DSN, ctx.Logger).Migrate(cmd.Context())
	}
}

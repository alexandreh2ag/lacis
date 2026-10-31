package cli

import (
	"github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/database"
	"github.com/spf13/cobra"
)

func GetWorkerCmd(ctx *context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "worker",
		Short: "Work the jobs and run the scheduled tasks, without serving the API",
		Long: "Works the jobs (normalization, links) and runs the scheduled tasks until SIGINT or SIGTERM, to run them " +
			"apart from start with workers.enabled false. Several workers share the jobs; each scheduled run happens once.",
		RunE: GetWorkerRunFn(ctx),
	}
}

func GetWorkerRunFn(ctx *context.Context) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		if ctx.Config.Database.MigrateOnStart {
			if err := database.NewMigrator(ctx.Config.Database.DSN, ctx.Logger).Migrate(cmd.Context()); err != nil {
				return err
			}
		}
		db, err := database.Open(ctx.Config.Database, ctx.Logger)
		if err != nil {
			return err
		}
		defer func() { _ = database.Close(db) }()
		_, stop, err := StartJobs(cmd.Context(), ctx, db, true)
		if err != nil {
			return err
		}
		defer stop()
		select {
		case sig := <-ctx.Signal():
			ctx.Logger.Info("shutting down", "signal", sig.String())
		case <-cmd.Context().Done():
		}
		return nil
	}
}

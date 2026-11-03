package cli

import (
	stdContext "context"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/service"
	"github.com/spf13/cobra"
)

func GetStartCmd(ctx *context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Run the agent: enroll when needed, then collect and send a snapshot every interval",
		Long: "Enroll with auth.enrollment_token when auth.state_dir holds no identity, then collect and send " +
			"a snapshot every interval plus a random jitter. Stops on SIGINT or SIGTERM.",
		RunE: GetStartRunFn(ctx),
	}
}

func GetStartRunFn(ctx *context.Context) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		c, err := NewClient(ctx)
		if err != nil {
			return err
		}
		stdCtx, cancel := stdContext.WithCancel(cmd.Context())
		defer cancel()
		go func() {
			select {
			case sig := <-ctx.Signal():
				ctx.Logger.Info("shutting down", "signal", sig.String())
				cancel()
			case <-stdCtx.Done():
			}
		}()
		if err := service.Run(stdCtx, ctx, c); err != nil {
			return err
		}
		ctx.Logger.Info("agent stopped")
		return nil
	}
}

package cli

import (
	"fmt"

	"github.com/alexandreh2ag/lacis/agent/client"
	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/service"
	"github.com/spf13/cobra"
)

// NewClient builds the client of the server; tests replace it.
var NewClient = func(ctx *context.Context) (client.Client, error) {
	return client.New(ctx.Config.Server, ctx.Fs)
}

func GetEnrollCmd(ctx *context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "enroll",
		Short: "Enroll the agent with auth.enrollment_token, once",
		Long: "Exchange auth.enrollment_token for the identity of the agent, kept in auth.state_dir. " +
			"Nothing is done when the agent is already enrolled: remove the identity file to enroll it again.",
		RunE: GetEnrollRunFn(ctx),
	}
}

func GetEnrollRunFn(ctx *context.Context) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		c, err := NewClient(ctx)
		if err != nil {
			return err
		}
		id, enrolled, err := service.Enroll(cmd.Context(), ctx, c)
		if err != nil {
			return err
		}
		if enrolled {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "agent %s enrolled\n", id.AgentID)
		} else {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "agent %s already enrolled\n", id.AgentID)
		}
		return nil
	}
}

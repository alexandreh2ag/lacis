package cli

import (
	"encoding/json"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/service"
	"github.com/spf13/cobra"
)

func GetCollectCmd(ctx *context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "collect",
		Short: "Collect once and print the snapshot on stdout, without sending it",
		Long: "Collect once and print on stdout exactly what would be sent to the server, " +
			"secret filtering included. Nothing is sent.",
		RunE: GetCollectRunFn(ctx),
	}
}

func GetCollectRunFn(ctx *context.Context) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		s, err := service.Collect(cmd.Context(), ctx)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(s)
	}
}

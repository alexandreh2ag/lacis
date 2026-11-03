package cli

import (
	"fmt"

	"github.com/alexandreh2ag/lacis/context"
	"github.com/spf13/cobra"
)

func GetValidateCmd(ctx *context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the configuration without starting anything",
		// The run validates and reports, so the pre-run only loads.
		PersistentPreRunE: GetRootPreRunEFn(ctx, false),
		RunE:              GetValidateRunFn(ctx),
	}
}

func GetValidateRunFn(ctx *context.Context) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		if err := ctx.Config.Validate(ctx.Validator); err != nil {
			return fmt.Errorf("invalid configuration:\n%w", err)
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "configuration is valid")
		return nil
	}
}

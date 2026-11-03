package cli

import (
	"fmt"
	"github.com/alexandreh2ag/lacis/common/version"
	"github.com/spf13/cobra"
)

func GetVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version and commit",
		// version needs no configuration.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		Run:               GetVersionRunFn(),
	}
}

func GetVersionRunFn() func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, _ []string) {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), version.GetFormattedVersion())
	}
}

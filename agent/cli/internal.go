package cli

import (
	"errors"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/fsutil"
	"github.com/spf13/cobra"
)

const FlagMax = "max"

// ExitCodeError makes lacis-agent exit with Code.
type ExitCodeError struct {
	Code int
	Err  error
}

func (e *ExitCodeError) Error() string { return e.Err.Error() }
func (e *ExitCodeError) Unwrap() error { return e.Err }

// ExitCode returns the exit code of lacis-agent for the error of its
// command.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *ExitCodeError
	if errors.As(err, &exit) {
		return exit.Code
	}
	return 1
}

// GetInternalCmd groups the commands the agent runs itself, as another
// user; hidden.
func GetInternalCmd(ctx *context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "internal",
		Short:  "Commands run by the agent itself",
		Hidden: true,
		// Run as the user of another process: the configuration of the
		// agent is neither needed nor readable.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
	}
	cmd.AddCommand(GetInternalReadCmd(ctx))
	return cmd
}

// GetInternalReadCmd prints a regular file: the agent runs it with the
// user, group and groups of a process to read a configuration file named
// by its command line, with no more rights than that process.
func GetInternalReadCmd(ctx *context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "read <path>",
		Short: "Print a regular file, of at most --max bytes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			limit, _ := cmd.Flags().GetInt64(FlagMax)
			b, err := fsutil.ReadRegularFollow(ctx.Fs, args[0], limit)
			if err != nil {
				return &ExitCodeError{Code: fsutil.ExitCode(err), Err: err}
			}
			_, err = cmd.OutOrStdout().Write(b)
			return err
		},
	}
	cmd.Flags().Int64(FlagMax, fsutil.MaxConfigSize, "maximum size of the file")
	return cmd
}

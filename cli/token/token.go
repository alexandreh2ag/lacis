// Package token holds the "token" subcommands of lacis-server.
package token

import (
	"fmt"
	"time"

	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/database"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/spf13/cobra"
)

const (
	FlagLabel = "label"
	FlagTTL   = "ttl"

	DefaultTTL = "7d"
)

// NewService builds the service used by the commands, and the function
// releasing it; tests replace it.
var NewService = func(ctx *context.Context) (service.EnrollmentTokenService, func(), error) {
	db, err := database.Open(ctx.Config.Database, ctx.Logger)
	if err != nil {
		return nil, nil, err
	}
	return service.NewEnrollmentTokenService(ctx, repository.NewEnrollmentTokenRepository(db), repository.NewAgentRepository(db)), func() { _ = database.Close(db) }, nil
}

func GetTokenCmd(ctx *context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Enrollment token commands",
	}
	cmd.AddCommand(GetCreateCmd(ctx))
	return cmd
}

func GetCreateCmd(ctx *context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an enrollment token",
		Long: "Create a token enrolling agents into any namespace, the one given by their namespace label. " +
			"The token is printed alone on the standard output, once: only its hash is stored.",
		Example: "  lacis-server token create --label web --ttl 1d\n" +
			"  lacis-server token create --label ci --ttl 0   # never expires",
		RunE: GetCreateRunFn(ctx),
	}
	cmd.Flags().String(FlagLabel, "", "free label, e.g. what the token is for")
	cmd.Flags().String(FlagTTL, DefaultTTL, "lifetime, e.g. 12h, 7d, 1d12h; 0 never expires")
	return cmd
}

func GetCreateRunFn(ctx *context.Context) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		label, _ := cmd.Flags().GetString(FlagLabel)
		ttlStr, _ := cmd.Flags().GetString(FlagTTL)
		ttl, err := config.ParseDuration(ttlStr)
		if err != nil {
			return err
		}

		svc, release, err := NewService(ctx)
		if err != nil {
			return err
		}
		defer release()
		created, err := svc.Create(cmd.Context(), service.CreateEnrollmentTokenInput{
			Label: label, TTL: ttl,
		})
		if err != nil {
			return err
		}
		if created.Token.ExpiresAt == nil {
			cmd.PrintErrln("enrollment token, never expires:")
		} else {
			cmd.PrintErrf("enrollment token, valid until %s:\n", created.Token.ExpiresAt.Format(time.RFC3339))
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), created.Secret)
		return nil
	}
}

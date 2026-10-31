// Package user holds the "user" subcommands of lacis-server.
package user

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/database"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/repository"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	FlagUsername  = "username"
	FlagFirstName = "first-name"
	FlagLastName  = "last-name"
	FlagRole      = "role"
)

// NewService builds the service used by the commands, and the function
// releasing it; tests replace it.
var NewService = func(ctx *context.Context) (service.UserService, func(), error) {
	db, err := database.Open(ctx.Config.Database, ctx.Logger)
	if err != nil {
		return nil, nil, err
	}
	return service.NewUserService(ctx, repository.NewTransactor(db)), func() { _ = database.Close(db) }, nil
}

func GetUserCmd(ctx *context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Local user commands",
	}
	cmd.AddCommand(GetCreateCmd(ctx))
	return cmd
}

func GetCreateCmd(ctx *context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a local user",
		Long: "Create a local user with the given roles. The password is prompted on a terminal, " +
			"otherwise read from the first line of the standard input.",
		Example: "  lacis-server user create --username admin\n" +
			"  echo \"$PASSWORD\" | lacis-server user create --username bob --first-name Bob --last-name Martin --role acme-viewer",
		RunE: GetCreateRunFn(ctx),
	}
	cmd.Flags().String(FlagUsername, "", "username, used as login")
	cmd.Flags().String(FlagFirstName, "", "first name")
	cmd.Flags().String(FlagLastName, "", "last name")
	cmd.Flags().StringSlice(FlagRole, []string{model.RoleAdmin}, "role code, repeatable")
	_ = cmd.MarkFlagRequired(FlagUsername)
	return cmd
}

func GetCreateRunFn(ctx *context.Context) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		username, _ := cmd.Flags().GetString(FlagUsername)
		firstName, _ := cmd.Flags().GetString(FlagFirstName)
		lastName, _ := cmd.Flags().GetString(FlagLastName)
		roles, _ := cmd.Flags().GetStringSlice(FlagRole)

		password, err := readPassword(cmd)
		if err != nil {
			return err
		}
		svc, release, err := NewService(ctx)
		if err != nil {
			return err
		}
		defer release()
		user, err := svc.Create(cmd.Context(), service.CreateUserInput{
			Username: username, Password: password, FirstName: firstName, LastName: lastName, Roles: roles,
		})
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "user %s created with roles: %s\n", user.Username, strings.Join(roles, ", "))
		return nil
	}
}

// Terminal access, replaced in tests.
var (
	isTerminal   = term.IsTerminal
	readTerminal = term.ReadPassword
)

// readPassword prompts twice for the password on a terminal, and reads the
// first line of the input otherwise.
func readPassword(cmd *cobra.Command) (string, error) {
	in := cmd.InOrStdin()
	if f, ok := in.(*os.File); ok && isTerminal(int(f.Fd())) {
		cmd.PrintErr("Password: ")
		first, err := readTerminal(int(f.Fd()))
		cmd.PrintErrln()
		if err != nil {
			return "", err
		}
		cmd.PrintErr("Confirm password: ")
		second, err := readTerminal(int(f.Fd()))
		cmd.PrintErrln()
		if err != nil {
			return "", err
		}
		if string(first) != string(second) {
			return "", errors.New("the passwords do not match")
		}
		return string(first), nil
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read password: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

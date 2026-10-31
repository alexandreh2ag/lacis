package user

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/alexandreh2ag/lacis/context"
	mockService "github.com/alexandreh2ag/lacis/mocks/service"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// withService makes the commands use svc, and reports whether it was
// released.
func withService(t *testing.T, svc service.UserService, err error) *bool {
	released := false
	previous := NewService
	NewService = func(*context.Context) (service.UserService, func(), error) {
		return svc, func() { released = true }, err
	}
	t.Cleanup(func() { NewService = previous })
	return &released
}

func run(ctx *context.Context, stdin string, args ...string) (stdout, stderr string, err error) {
	cmd := GetUserCmd(ctx)
	cmd.SilenceUsage = true // as under the root command
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestCreateCmd(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		stdin     string
		want      service.CreateUserInput
		wantRoles string
	}{
		{
			name:      "default role",
			args:      []string{"create", "--username", "admin"},
			stdin:     "s3cret-password\n",
			want:      service.CreateUserInput{Username: "admin", Password: "s3cret-password", Roles: []string{model.RoleAdmin}},
			wantRoles: "admin",
		},
		{
			name:      "roles and name, no trailing newline",
			args:      []string{"create", "--username", "bob", "--first-name", "Bob", "--last-name", "Martin", "--role", "acme-viewer", "--role", "auditor"},
			stdin:     "s3cret-password",
			want:      service.CreateUserInput{Username: "bob", Password: "s3cret-password", FirstName: "Bob", LastName: "Martin", Roles: []string{"acme-viewer", "auditor"}},
			wantRoles: "acme-viewer, auditor",
		},
		{
			name:      "windows line ending",
			args:      []string{"create", "--username", "bob"},
			stdin:     "s3cret-password\r\nignored\n",
			want:      service.CreateUserInput{Username: "bob", Password: "s3cret-password", Roles: []string{model.RoleAdmin}},
			wantRoles: "admin",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := mockService.NewMockUserService(gomock.NewController(t))
			svc.EXPECT().Create(gomock.Any(), tt.want).Return(&model.User{Username: tt.want.Username}, nil)
			released := withService(t, svc, nil)

			out, _, err := run(context.TestContext(nil), tt.stdin, tt.args...)
			require.NoError(t, err)
			assert.Equal(t, "user "+tt.want.Username+" created with roles: "+tt.wantRoles+"\n", out)
			assert.True(t, *released)
		})
	}
}

func TestCreateCmd_Errors(t *testing.T) {
	t.Run("missing username", func(t *testing.T) {
		_, _, err := run(context.TestContext(nil), "", "create")
		assert.ErrorContains(t, err, `required flag(s) "username" not set`)
	})
	t.Run("service unavailable", func(t *testing.T) {
		withService(t, nil, errors.New("database down"))
		_, _, err := run(context.TestContext(nil), "s3cret-password", "create", "--username", "a")
		assert.ErrorContains(t, err, "database down")
	})
	t.Run("creation fails", func(t *testing.T) {
		svc := mockService.NewMockUserService(gomock.NewController(t))
		svc.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil, service.ErrAlreadyExists)
		released := withService(t, svc, nil)
		_, _, err := run(context.TestContext(nil), "s3cret-password", "create", "--username", "a")
		assert.ErrorIs(t, err, service.ErrAlreadyExists)
		assert.True(t, *released)
	})
	t.Run("unreadable input", func(t *testing.T) {
		cmd := GetCreateCmd(context.TestContext(nil))
		cmd.SetIn(failingReader{})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--username", "a"})
		assert.ErrorContains(t, cmd.Execute(), "read password")
	})
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestReadPassword_Terminal(t *testing.T) {
	stdin, err := os.CreateTemp(t.TempDir(), "stdin")
	require.NoError(t, err)
	errRead := errors.New("read error")
	tests := []struct {
		name    string
		answers []string
		failAt  int
		want    string
		wantErr string
	}{
		{name: "confirmed", answers: []string{"s3cret-password", "s3cret-password"}, failAt: -1, want: "s3cret-password"},
		{name: "mismatch", answers: []string{"s3cret-password", "other"}, failAt: -1, wantErr: "do not match"},
		{name: "first read fails", failAt: 0, wantErr: "read error"},
		{name: "confirmation read fails", answers: []string{"s3cret-password"}, failAt: 1, wantErr: "read error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			prevIs, prevRead := isTerminal, readTerminal
			isTerminal = func(fd int) bool { return fd == int(stdin.Fd()) }
			readTerminal = func(int) ([]byte, error) {
				defer func() { calls++ }()
				if calls == tt.failAt {
					return nil, errRead
				}
				return []byte(tt.answers[calls]), nil
			}
			t.Cleanup(func() { isTerminal, readTerminal = prevIs, prevRead })

			cmd := GetCreateCmd(context.TestContext(nil))
			cmd.SetIn(stdin)
			errOut := &bytes.Buffer{}
			cmd.SetErr(errOut)
			got, err := readPassword(cmd)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, "Password: \nConfirm password: \n", errOut.String())
		})
	}
}

func TestNewService(t *testing.T) {
	ctx := context.TestContext(nil)
	ctx.Config.Database.DSN = "postgres://nobody@127.0.0.1:1/none?connect_timeout=1"
	ctx.Config.Database.MaxOpenConns = 1
	_, _, err := NewService(ctx)
	assert.ErrorContains(t, err, "open database")
}

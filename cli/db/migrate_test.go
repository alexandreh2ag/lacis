package db

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"

	"github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/database"
	mockDatabase "github.com/alexandreh2ag/lacis/mocks/database"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestMigrateCmd(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr string
	}{
		{name: "success"},
		{name: "failure", err: errors.New("boom"), wantErr: "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			ctx := context.TestContext(nil)
			ctx.Config.Database.DSN = "postgres://lacis@db/lacis"

			migrator := mockDatabase.NewMockMigrator(ctrl)
			migrator.EXPECT().Migrate(gomock.Any()).Return(tt.err)
			previous := NewMigrator
			NewMigrator = func(dsn string, logger *slog.Logger) database.Migrator {
				assert.Equal(t, "postgres://lacis@db/lacis", dsn)
				assert.Same(t, ctx.Logger, logger)
				return migrator
			}
			t.Cleanup(func() { NewMigrator = previous })

			cmd := GetDBCmd(ctx)
			cmd.SetArgs([]string{"migrate"})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			err := cmd.Execute()
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			assert.NoError(t, err)
		})
	}
}

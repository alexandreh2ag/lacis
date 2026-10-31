//go:build integration

package database_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/database"
	"github.com/alexandreh2ag/lacis/database/dbtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrator_Idempotent(t *testing.T) {
	db := dbtest.Open(t, "database")
	logs := &bytes.Buffer{}

	err := database.NewMigrator(dbtest.DSN(t, "database"), slog.New(slog.NewTextHandler(logs, nil))).Migrate(context.Background())
	require.NoError(t, err)
	assert.Contains(t, logs.String(), `msg="lacis migrations applied" version=20261030130000 changed=false`)
	assert.Contains(t, logs.String(), `msg="river migrations applied" versions=[]`)

	var tables []string
	require.NoError(t, db.Raw("select tablename from pg_tables where schemaname = current_schema() order by 1").Scan(&tables).Error)
	for _, table := range []string{"namespaces", "hosts", "snapshots", "links", "users", "roles", "namespace_permissions", "global_permissions", "river_job", "schema_migrations"} {
		assert.Contains(t, tables, table)
	}

	// The admin role is created with every permission.
	var perms []string
	require.NoError(t, db.Raw(`select coalesce(n.namespace_id::text, 'all') || ' ' || n.resource || ' ' || n.action from roles r
		join namespace_permissions n on n.role_id = r.id where r.code = 'admin'
		union all select 'global ' || g.resource || ' ' || g.action from roles r
		join global_permissions g on g.role_id = r.id where r.code = 'admin' order by 1`).Scan(&perms).Error)
	assert.Equal(t, []string{"all * *", "global * *"}, perms)
}

func TestMigrator_Errors(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	err := database.NewMigrator("postgres://nobody@127.0.0.1:1/none?connect_timeout=1", logger).Migrate(context.Background())
	assert.ErrorContains(t, err, "connect to database")

	err = database.NewMigrator("::not a dsn", logger).Migrate(context.Background())
	assert.Error(t, err)
}

func TestOpen(t *testing.T) {
	dbtest.Open(t, "database")
	debug := slog.New(slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	for _, logger := range []*slog.Logger{slog.New(slog.DiscardHandler), debug} {
		db, err := database.Open(config.DatabaseConfig{DSN: dbtest.DSN(t, "database"), MaxOpenConns: 2}, logger)
		require.NoError(t, err)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		assert.Equal(t, 2, sqlDB.Stats().MaxOpenConnections)
		require.NoError(t, database.Close(db))
	}

	_, err := database.Open(config.DatabaseConfig{DSN: "postgres://nobody@127.0.0.1:1/none?connect_timeout=1", MaxOpenConns: 1}, slog.New(slog.DiscardHandler))
	assert.ErrorContains(t, err, "open database")
}

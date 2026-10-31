//go:build integration

// Package dbtest gives integration tests a migrated Postgres database. Each
// test package gets its own database, lacis_test_<name>, recreated once per
// run, so packages tested in parallel do not share data.
package dbtest

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/alexandreh2ag/lacis/config"
	"github.com/alexandreh2ag/lacis/database"
	"github.com/alexandreh2ag/lacis/migrations"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"gorm.io/gorm"
)

// EnvDSN names the variable holding the DSN of a database the tests can
// connect to; it defaults to the one of compose.yml.
const EnvDSN = "LACIS_TEST_DATABASE_DSN"

const defaultDSN = "postgres://lacis:lacis@localhost:5432/lacis?sslmode=disable"

var (
	mu  sync.Mutex
	dbs = map[string]*gorm.DB{}
)

// DSN returns the DSN of the database lacis_test_<name>.
func DSN(t testing.TB, name string) string {
	t.Helper()
	base := os.Getenv(EnvDSN)
	if base == "" {
		base = defaultDSN
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse %s: %v", EnvDSN, err)
	}
	u.Path = "/lacis_test_" + name
	return u.String()
}

// Open returns the database lacis_test_<name>, created and migrated on the
// first call of the run, and truncated on every call.
func Open(t testing.TB, name string) *gorm.DB {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	db, ok := dbs[name]
	if !ok {
		db = create(t, name)
		dbs[name] = db
	}
	Truncate(t, db)
	return db
}

func create(t testing.TB, name string) *gorm.DB {
	t.Helper()
	dsn := DSN(t, name)
	admin, err := sql.Open("pgx", strings.Replace(dsn, "/lacis_test_"+name, "/postgres", 1))
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}
	defer func() { _ = admin.Close() }()
	ident := fmt.Sprintf(`"lacis_test_%s"`, name)
	for _, q := range []string{"drop database if exists " + ident + " with (force)", "create database " + ident} {
		if _, err := admin.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := database.NewMigrator(dsn, logger).Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := database.Open(config.DatabaseConfig{DSN: dsn, MaxOpenConns: 5}, logger)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return db
}

// Truncate empties every Lacis table, leaving the migration tables and River
// alone, then replays the seed migrations (*_seed_*.up.sql).
func Truncate(t testing.TB, db *gorm.DB) {
	t.Helper()
	var tables []string
	err := db.Raw(`select tablename from pg_tables where schemaname = current_schema()
		and tablename <> 'schema_migrations' and tablename not like 'river%'`).Scan(&tables).Error
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	if len(tables) == 0 {
		return
	}
	if err := db.Exec("truncate " + strings.Join(tables, ", ") + " cascade").Error; err != nil {
		t.Fatalf("truncate: %v", err)
	}
	seeds, err := fs.Glob(migrations.FS, "*_seed_*.up.sql")
	if err != nil {
		t.Fatalf("list seeds: %v", err)
	}
	for _, seed := range seeds {
		stmt, err := fs.ReadFile(migrations.FS, seed)
		if err != nil {
			t.Fatalf("read %s: %v", seed, err)
		}
		if err := db.Exec(string(stmt)).Error; err != nil {
			t.Fatalf("replay %s: %v", seed, err)
		}
	}
}

package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/alexandreh2ag/lacis/migrations"
	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivermigrate"
)

// Migrator applies the database migrations.
type Migrator interface {
	// Migrate applies the pending migrations of Lacis, then those of River.
	Migrate(ctx context.Context) error
}

type migrator struct {
	dsn    string
	logger *slog.Logger
}

// NewMigrator returns a Migrator that opens its own connection to dsn for
// each run.
func NewMigrator(dsn string, logger *slog.Logger) Migrator {
	return &migrator{dsn: dsn, logger: logger}
}

func (m *migrator) Migrate(ctx context.Context) error {
	if err := m.migrateLacis(); err != nil {
		return err
	}
	return m.migrateRiver(ctx)
}

func (m *migrator) migrateLacis() error {
	db, err := sql.Open("pgx", m.dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("read migrations: %w", err)
	}
	driver, err := migratepgx.WithInstance(db, &migratepgx.Config{})
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("connect to database: %w", err)
	}
	mig, err := migrate.NewWithInstance("iofs", source, "pgx", driver)
	if err != nil {
		_ = driver.Close()
		return fmt.Errorf("create migrator: %w", err)
	}
	// Closes the driver, its connection and db.
	defer func() { _, _ = mig.Close() }()
	err = mig.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply lacis migrations: %w", err)
	}
	version, _, verr := mig.Version()
	if verr != nil {
		return fmt.Errorf("read lacis migration version: %w", verr)
	}
	m.logger.Info("lacis migrations applied", "version", version, "changed", err == nil)
	return nil
}

func (m *migrator) migrateRiver(ctx context.Context) error {
	db, err := sql.Open("pgx", m.dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()
	mig, err := rivermigrate.New(riverdatabasesql.New(db), &rivermigrate.Config{Logger: m.logger})
	if err != nil {
		return fmt.Errorf("create river migrator: %w", err)
	}
	res, err := mig.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return fmt.Errorf("apply river migrations: %w", err)
	}
	versions := make([]int, 0, len(res.Versions))
	for _, v := range res.Versions {
		versions = append(versions, v.Version)
	}
	m.logger.Info("river migrations applied", "versions", versions)
	return nil
}

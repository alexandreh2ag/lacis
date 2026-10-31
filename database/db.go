// Package database opens the Postgres connection and applies the
// migrations.
package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/alexandreh2ag/lacis/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// SlowThreshold is the duration above which a query is logged as a warning.
const SlowThreshold = 200 * time.Millisecond

// Open connects to Postgres with GORM. Every SQL query is logged when log
// is at the debug level; otherwise only errors and slow queries are.
func Open(cfg config.DatabaseConfig, log *slog.Logger) (*gorm.DB, error) {
	level := logger.Warn
	if log.Enabled(context.Background(), slog.LevelDebug) {
		level = logger.Info
	}
	db, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{
		Logger: logger.NewSlogLogger(log, logger.Config{
			LogLevel:                  level,
			SlowThreshold:             SlowThreshold,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      true,
		}),
		TranslateError: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	return db, nil
}

// Close closes the connection pool behind db.
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

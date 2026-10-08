package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

//go:generate go tool sqlc generate -f ../../sqlc.yaml

func migrationFS() (fs.FS, error) {
	return fs.Sub(migrationFiles, "migrations")
}

func provider(sqlDB *sql.DB) (*goose.Provider, error) {
	fsys, err := migrationFS()
	if err != nil {
		return nil, err
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, sqlDB, fsys)
	if err != nil {
		return nil, fmt.Errorf("migration provider: %w", err)
	}
	return p, nil
}

// Migrate applies embedded goose migrations.
func Migrate(ctx context.Context, sqlDB *sql.DB) error {
	p, err := provider(sqlDB)
	if err != nil {
		return err
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// MigrationsCurrent reports whether every embedded migration has been applied.
func MigrationsCurrent(ctx context.Context, sqlDB *sql.DB) error {
	p, err := provider(sqlDB)
	if err != nil {
		return err
	}
	pending, err := p.HasPending(ctx)
	if err != nil {
		return fmt.Errorf("migration status: %w", err)
	}
	if !pending {
		return nil
	}
	current, target, err := p.GetVersions(ctx)
	if err != nil {
		return fmt.Errorf("migrations are pending")
	}
	return fmt.Errorf("database version %d, latest migration %d", current, target)
}

package postgres

import (
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var MigrationsFS embed.FS

// Migrate runs all pending up-migrations against the given DSN.
// Returns nil if the database is already at the latest version.
func Migrate(dsn string) error {
	if dsn == "" {
		return fmt.Errorf("postgres: empty DSN")
	}
	src, err := iofs.New(MigrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("postgres: open embedded migrations: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, "pgx5://"+dsnWithoutScheme(dsn))
	if err != nil {
		return fmt.Errorf("postgres: new migrator: %w", err)
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("postgres: migrate up: %w", err)
	}
	return nil
}

// dsnWithoutScheme strips the "postgres://" or "postgresql://" prefix so the
// pgx5 driver scheme can be prepended for golang-migrate registration.
func dsnWithoutScheme(dsn string) string {
	for _, prefix := range []string{"postgres://", "postgresql://"} {
		if len(dsn) >= len(prefix) && dsn[:len(prefix)] == prefix {
			return dsn[len(prefix):]
		}
	}
	return dsn
}

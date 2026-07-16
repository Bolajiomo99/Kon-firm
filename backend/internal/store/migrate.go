package store

import (
	"context"
	"embed"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrations live inside this package because go:embed cannot reach parent
// directories.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate applies every migration in lexical order.
//
// Each file must be idempotent: this runs on every boot, including on the
// deployed instance, so a fresh clone and a redeploy converge on the same
// schema without anyone remembering to run a separate command.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("store: reading migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		sqlBytes, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("store: reading %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx, string(sqlBytes)); err != nil {
			return fmt.Errorf("store: applying %s: %w", name, err)
		}
	}
	return nil
}

package database

import (
	"context"
	"embed"
	"fmt"
	"log"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func NewPool(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("unable to ping database: %w", err)
	}

	return pool, nil
}

// RunMigrations applies any embedded migration files that have not already
// been recorded in schema_migrations. Each migration runs inside its own
// transaction so a partial failure leaves no half-applied state.
//
// Pre-existing databases (which had migrations 001-014 applied via the old
// re-run-everything loop) get their version rows backfilled lazily: the
// CREATE/ALTER … IF NOT EXISTS statements in those files are no-ops on a
// populated schema, the transaction commits cleanly, and the version is
// recorded so future runs skip the file.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	applied, err := loadAppliedMigrations(ctx, pool)
	if err != nil {
		return fmt.Errorf("loading applied migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("reading migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		version := entry.Name()
		if applied[version] {
			continue
		}

		sql, err := migrationsFS.ReadFile("migrations/" + version)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", version, err)
		}

		if err := applyMigration(ctx, pool, version, string(sql)); err != nil {
			return err
		}
		log.Printf("Applied migration %s", version)
	}

	return nil
}

func loadAppliedMigrations(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
	rows, err := pool.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

func applyMigration(ctx context.Context, pool *pgxpool.Pool, version, sql string) (err error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx for %s: %w", version, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err = tx.Exec(ctx, sql); err != nil {
		return fmt.Errorf("executing migration %s: %w", version, err)
	}
	if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
		return fmt.Errorf("recording migration %s: %w", version, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", version, err)
	}
	return nil
}

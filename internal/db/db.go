package db

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	_ "modernc.org/sqlite" // pure-Go sqlite driver — no cgo, no native build step
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS // NOTE: migrations/ lives inside this package dir for go:embed to see it

// Open connects to the sqlite file at path (creating parent dirs if needed)
// and runs any migrations that haven't been applied yet.
func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	conn.SetMaxOpenConns(1) // sqlite + this driver: single writer is simplest and safest

	// The admin command runs as a second process on the same file. Wait up to
	// 5s for the other one's write to finish instead of failing instantly with
	// "database is locked". (Per-connection setting; there is exactly one.)
	if _, err := conn.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}

	if err := migrate(conn); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return conn, nil
}

func migrate(conn *sql.DB) error {
	if _, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`); err != nil {
		return err
	}

	entries, err := embeddedMigrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		var already int
		if err := conn.QueryRow(
			`SELECT COUNT(*) FROM schema_migrations WHERE filename = ?`, name,
		).Scan(&already); err != nil {
			return err
		}
		if already > 0 {
			continue
		}

		sqlBytes, err := embeddedMigrations.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if _, err := conn.Exec(string(sqlBytes)); err != nil {
			return fmt.Errorf("applying %s: %w", name, err)
		}
		if _, err := conn.Exec(
			`INSERT INTO schema_migrations (filename) VALUES (?)`, name,
		); err != nil {
			return err
		}
	}
	return nil
}

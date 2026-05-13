// Package store manages the shared SQLite database connection and migrations.
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // SQLite driver
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const (
	maxOpenConns    = 1
	maxIdleConns    = 1
	connMaxLifetime = 0
)

// To add a new migration, create a sequentially numbered .sql file in
// internal/store/migrations/ (e.g. 002_add_indexes.sql). It will be
// applied automatically on the next Open() call.

// Open opens (or creates) the SQLite database at path and runs all pending
// migrations before returning the connection.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening db: %w", err)
	}

	// SQLite only supports one writer at a time; using a single connection
	// avoids "database is locked" errors under concurrent access.
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxLifetime(connMaxLifetime)

	if err := migrate(db); err != nil {
		if err := db.Close(); err != nil {
			slog.Error("failed to close database", slog.Any("error", err))
		}

		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

// migrate creates the schema_migrations tracking table if needed, then applies
// any .sql files from the migrations directory that have not yet been run.
func migrate(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT     PRIMARY KEY,
		applied_at DATETIME NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("reading migrations dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version := entry.Name()

		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&count); err != nil {
			return fmt.Errorf("checking migration %s: %w", version, err)
		}
		if count > 0 {
			continue
		}

		sqlBytes, err := fs.ReadFile(migrationsFS, "migrations/"+version)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", version, err)
		}

		if _, err := db.Exec(string(sqlBytes)); err != nil {
			return fmt.Errorf("applying migration %s: %w", version, err)
		}

		if _, err := db.Exec(
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			version, time.Now().UTC(),
		); err != nil {
			return fmt.Errorf("recording migration %s: %w", version, err)
		}
	}

	return nil
}

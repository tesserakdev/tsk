// Package activitylog records and queries MCP tool call activity in a local SQLite DB.
package activitylog

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Logger records and queries tool call activity.
type Logger interface {
	Record(Entry) error
	Query(tool string, tail int, since time.Duration) ([]Entry, error)
}

// Entry represents a single recorded tool call.
type Entry struct {
	ID           string
	Tool         string
	Params       string
	Status       int
	Response     string // scrubbed and truncated before storage
	ScrubActions int    // number of scrub rules that fired; non-zero means sensitive data was present
	TS           time.Time
}

// Log records and queries tool call activity.
type Log struct {
	db *sql.DB
}

// New creates a Log backed by the provided database connection.
// The caller is responsible for opening the DB and running migrations via store.Open.
func New(db *sql.DB) *Log {
	return &Log{db: db}
}

// Record inserts an entry into the calls table.
// The entry's ID is set to a new UUIDv7 if empty.
func (l *Log) Record(e Entry) error {
	if e.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("generating id: %w", err)
		}

		e.ID = id.String()
	}

	if e.TS.IsZero() {
		e.TS = time.Now()
	}

	_, err := l.db.Exec(
		`INSERT INTO requests (id, tool, params, status, response, scrub_actions, ts)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.Tool, e.Params, e.Status, e.Response, e.ScrubActions, e.TS.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("recording entry: %w", err)
	}

	return nil
}

// Query returns log entries optionally filtered by tool name (non-empty),
// limited to the most recent tail rows (tail > 0), and restricted to entries
// no older than since (since > 0). Results are ordered newest-first.
func (l *Log) Query(tool string, tail int, since time.Duration) ([]Entry, error) {
	query := `SELECT id, tool, params, status, response, scrub_actions, ts FROM requests`

	whereClauses := []string{}
	args := []any{}

	if tool != "" {
		whereClauses = append(whereClauses, "tool = ?")
		args = append(args, tool)
	}

	if since > 0 {
		cutoff := time.Now().UTC().Add(-since).Format(time.RFC3339Nano)
		whereClauses = append(whereClauses, "ts >= ?")
		args = append(args, cutoff)
	}

	if len(whereClauses) > 0 {
		query += " WHERE " + strings.Join(whereClauses, " AND ")
	}

	query += ` ORDER BY ts DESC`
	if tail > 0 {
		query += ` LIMIT ?`
		args = append(args, tail)
	}

	rows, err := l.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying activity log: %w", err)
	}

	defer func() {
		if err := rows.Close(); err != nil {
			slog.Error("failed to close rows", slog.Any("error", err))
		}
	}()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var ts string

		if err := rows.Scan(&e.ID, &e.Tool, &e.Params, &e.Status, &e.Response, &e.ScrubActions, &ts); err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}

		t, _ := time.Parse(time.RFC3339Nano, ts)
		e.TS = t
		entries = append(entries, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}

	return entries, nil
}

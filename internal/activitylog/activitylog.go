// Package activitylog records and queries MCP tool call activity in a local SQLite DB.
package activitylog

import (
	"database/sql"
	"encoding/json"
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

// RotationEntry represents a single credential rotation event.
type RotationEntry struct {
	ID   string
	Keys []string
	TS   time.Time
}

// Event is a unified view over request and rotation log entries, used when
// querying both tables together ordered by time. Exactly one of Request or
// Rotation is non-nil; use Kind() and TS() to access common fields.
type Event struct {
	Request  *Entry
	Rotation *RotationEntry
}

// Kind returns "request" or "rotation".
func (e Event) Kind() string {
	if e.Request != nil {
		return "request"
	}
	return "rotation"
}

// TS returns the timestamp of the event.
func (e Event) TS() time.Time {
	if e.Request != nil {
		return e.Request.TS
	}
	return e.Rotation.TS
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

		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return nil, fmt.Errorf("parsing timestamp: %w", err)
		}
		e.TS = t
		entries = append(entries, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}

	return entries, nil
}

// RecordRotation inserts a credential rotation event with the names of changed keys.
func (l *Log) RecordRotation(keys []string) error {
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generating id: %w", err)
	}
	keysJSON, err := json.Marshal(keys)
	if err != nil {
		return fmt.Errorf("marshaling keys: %w", err)
	}
	_, err = l.db.Exec(
		`INSERT INTO credential_rotations (id, keys, ts) VALUES (?, ?, ?)`,
		id.String(), string(keysJSON), time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("recording rotation: %w", err)
	}
	return nil
}

// QueryRotations returns rotation entries newest-first, optionally limited to the
// most recent tail rows and restricted to entries no older than since.
func (l *Log) QueryRotations(tail int, since time.Duration) ([]RotationEntry, error) {
	query := `SELECT id, keys, ts FROM credential_rotations`
	var args []any

	if since > 0 {
		cutoff := time.Now().UTC().Add(-since).Format(time.RFC3339Nano)
		query += ` WHERE ts >= ?`
		args = append(args, cutoff)
	}

	query += ` ORDER BY ts DESC`
	if tail > 0 {
		query += ` LIMIT ?`
		args = append(args, tail)
	}

	rows, err := l.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying rotations: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Error("failed to close rows", slog.Any("error", err))
		}
	}()

	var entries []RotationEntry
	for rows.Next() {
		var e RotationEntry
		var keysJSON, ts string
		if err := rows.Scan(&e.ID, &keysJSON, &ts); err != nil {
			return nil, fmt.Errorf("scanning rotation row: %w", err)
		}
		if err := json.Unmarshal([]byte(keysJSON), &e.Keys); err != nil {
			return nil, fmt.Errorf("parsing rotation keys: %w", err)
		}
		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return nil, fmt.Errorf("parsing rotation timestamp: %w", err)
		}
		e.TS = t
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rotation rows: %w", err)
	}
	return entries, nil
}

// QueryEvents returns request and rotation entries interleaved newest-first
// using a single UNION ALL query so the database index handles sorting and
// the LIMIT is applied across both tables.
//
// kind filters to "request" or "rotation"; an empty string returns both.
// tool filters request entries by tool name (ignored for rotation entries).
// tail limits the total result count (0 = no limit).
// since excludes entries older than the duration (0 = no filter).
func (l *Log) QueryEvents(tool string, tail int, since time.Duration, kind string) ([]Event, error) {
	var parts []string
	var args []any

	if kind != "rotation" {
		q := `SELECT 'request' AS kind, id, ts, tool, status, response, scrub_actions, ''
		      FROM requests WHERE 1=1`
		if tool != "" {
			q += ` AND tool = ?`
			args = append(args, tool)
		}
		if since > 0 {
			q += ` AND ts >= ?`
			args = append(args, time.Now().UTC().Add(-since).Format(time.RFC3339Nano))
		}
		parts = append(parts, q)
	}

	if kind != "request" {
		q := `SELECT 'rotation' AS kind, id, ts, '' AS tool, 0 AS status, '' AS response, 0 AS scrub_actions, keys
		      FROM credential_rotations WHERE 1=1`
		if since > 0 {
			q += ` AND ts >= ?`
			args = append(args, time.Now().UTC().Add(-since).Format(time.RFC3339Nano))
		}
		parts = append(parts, q)
	}

	if len(parts) == 0 {
		return nil, nil
	}

	query := strings.Join(parts, " UNION ALL ") + " ORDER BY ts DESC"
	if tail > 0 {
		query += " LIMIT ?"
		args = append(args, tail)
	}

	rows, err := l.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying events: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Error("failed to close rows", slog.Any("error", err))
		}
	}()

	var events []Event
	for rows.Next() {
		var kindStr, id, ts, toolName, response, keysJSON string
		var status, scrubActions int
		if err := rows.Scan(&kindStr, &id, &ts, &toolName, &status, &response, &scrubActions, &keysJSON); err != nil {
			return nil, fmt.Errorf("scanning event row: %w", err)
		}
		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return nil, fmt.Errorf("parsing event timestamp: %w", err)
		}
		var e Event
		if kindStr == "request" {
			e.Request = &Entry{
				ID:           id,
				TS:           t,
				Tool:         toolName,
				Status:       status,
				Response:     response,
				ScrubActions: scrubActions,
			}
		} else {
			var keys []string
			if keysJSON != "" {
				if err := json.Unmarshal([]byte(keysJSON), &keys); err != nil {
					return nil, fmt.Errorf("parsing rotation keys: %w", err)
				}
			}
			e.Rotation = &RotationEntry{ID: id, TS: t, Keys: keys}
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating events: %w", err)
	}
	return events, nil
}

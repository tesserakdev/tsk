package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tesserakdev/tsk/internal/activitylog"
	"github.com/tesserakdev/tsk/internal/store"
)

func setupLogsDB(t *testing.T) (string, *activitylog.Log) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "activity.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return dir, activitylog.New(db)
}

func TestRunLogs_NoEntries(t *testing.T) {
	dir, _ := setupLogsDB(t)

	var buf bytes.Buffer
	if err := runLogs(dir, 0, "", 0, &buf); err != nil {
		t.Fatalf("runLogs: %v", err)
	}

	if got := buf.String(); got != "No entries found.\n" {
		t.Errorf("got %q, want %q", got, "No entries found.\n")
	}
}

func TestRunLogs_ShowsEntries(t *testing.T) {
	dir, log := setupLogsDB(t)

	tools := []string{"tool_alpha", "tool_beta", "tool_gamma"}
	for _, name := range tools {
		if err := log.Record(activitylog.Entry{Tool: name, Status: 200}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	var buf bytes.Buffer
	if err := runLogs(dir, 0, "", 0, &buf); err != nil {
		t.Fatalf("runLogs: %v", err)
	}

	out := buf.String()
	for _, name := range tools {
		if !strings.Contains(out, name) {
			t.Errorf("output missing tool %q:\n%s", name, out)
		}
	}
}

func TestRunLogs_TailFilter(t *testing.T) {
	dir, log := setupLogsDB(t)

	for i := 0; i < 5; i++ {
		if err := log.Record(activitylog.Entry{Tool: "my_tool", Status: 200}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	var buf bytes.Buffer
	if err := runLogs(dir, 2, "", 0, &buf); err != nil {
		t.Fatalf("runLogs: %v", err)
	}

	out := buf.String()
	// Count data rows (skip header lines)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	dataLines := 0
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "ID") || strings.HasPrefix(l, "--") {
			continue
		}
		dataLines++
	}
	if dataLines != 2 {
		t.Errorf("expected 2 data rows, got %d\noutput:\n%s", dataLines, out)
	}
}

func TestRunLogs_ToolFilter(t *testing.T) {
	dir, log := setupLogsDB(t)

	for i := 0; i < 3; i++ {
		if err := log.Record(activitylog.Entry{Tool: "tool_a", Status: 200}); err != nil {
			t.Fatalf("Record tool_a: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := log.Record(activitylog.Entry{Tool: "tool_b", Status: 200}); err != nil {
			t.Fatalf("Record tool_b: %v", err)
		}
	}

	var buf bytes.Buffer
	if err := runLogs(dir, 0, "tool_a", 0, &buf); err != nil {
		t.Fatalf("runLogs: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "tool_b") {
		t.Errorf("output contains tool_b but should not:\n%s", out)
	}
	if !strings.Contains(out, "tool_a") {
		t.Errorf("output missing tool_a:\n%s", out)
	}
}

func TestRunLogs_SinceFilter(t *testing.T) {
	dir, log := setupLogsDB(t)

	// Insert an old entry (2 hours ago)
	if err := log.Record(activitylog.Entry{
		Tool:   "old_tool",
		Status: 200,
		TS:     time.Now().Add(-2 * time.Hour),
	}); err != nil {
		t.Fatalf("Record old entry: %v", err)
	}

	// Insert a recent entry (1 minute ago)
	if err := log.Record(activitylog.Entry{
		Tool:   "recent_tool",
		Status: 200,
		TS:     time.Now().Add(-1 * time.Minute),
	}); err != nil {
		t.Fatalf("Record recent entry: %v", err)
	}

	var buf bytes.Buffer
	if err := runLogs(dir, 0, "", 30*time.Minute, &buf); err != nil {
		t.Fatalf("runLogs: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "old_tool") {
		t.Errorf("output contains old_tool but should not (since=30m):\n%s", out)
	}
	if !strings.Contains(out, "recent_tool") {
		t.Errorf("output missing recent_tool:\n%s", out)
	}
}

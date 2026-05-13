package activitylog_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/tesserakdev/tsk/internal/activitylog"
	"github.com/tesserakdev/tsk/internal/store"
)

func openTemp(t *testing.T) *activitylog.Log {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "activity.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return activitylog.New(db)
}

func TestOpenAndRecord(t *testing.T) {
	l := openTemp(t)
	e := activitylog.Entry{
		Tool:         "my_tool",
		Params:       `{"k":"v"}`,
		Status:       200,
		Response:     `{"result":"ok"}`,
		ScrubActions: 1,
		TS:           time.Now(),
	}
	if err := l.Record(e); err != nil {
		t.Fatalf("Record: %v", err)
	}
}

func TestQuery_AllEntries(t *testing.T) {
	l := openTemp(t)
	for _, tool := range []string{"tool_a", "tool_b", "tool_a"} {
		if err := l.Record(activitylog.Entry{Tool: tool, TS: time.Now()}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	entries, err := l.Query("", 0, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
}

func TestQuery_FilterByTool(t *testing.T) {
	l := openTemp(t)
	for _, tool := range []string{"tool_a", "tool_b", "tool_a"} {
		if err := l.Record(activitylog.Entry{Tool: tool, TS: time.Now()}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	entries, err := l.Query("tool_a", 0, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries for tool_a, got %d", len(entries))
	}
	for _, e := range entries {
		if e.Tool != "tool_a" {
			t.Errorf("expected tool_a, got %q", e.Tool)
		}
	}
}

func TestQuery_Tail(t *testing.T) {
	l := openTemp(t)
	base := time.Now().Add(-5 * time.Second)
	for i := 0; i < 5; i++ {
		if err := l.Record(activitylog.Entry{Tool: "t", TS: base.Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	entries, err := l.Query("", 2, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries with tail=2, got %d", len(entries))
	}
}

func TestQuery_Since(t *testing.T) {
	l := openTemp(t)
	old := time.Now().Add(-2 * time.Hour)
	recent := time.Now().Add(-1 * time.Minute)
	for _, ts := range []time.Time{old, recent} {
		if err := l.Record(activitylog.Entry{Tool: "t", TS: ts}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	entries, err := l.Query("", 0, 30*time.Minute)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 recent entry, got %d", len(entries))
	}
}

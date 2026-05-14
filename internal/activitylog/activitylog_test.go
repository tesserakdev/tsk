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

func TestRecordAndQueryRotations(t *testing.T) {
	l := openTemp(t)

	keys := []string{"API_KEY", "SECRET_TOKEN"}
	if err := l.RecordRotation(keys); err != nil {
		t.Fatalf("RecordRotation: %v", err)
	}

	entries, err := l.QueryRotations(0, 0)
	if err != nil {
		t.Fatalf("QueryRotations: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 rotation entry, got %d", len(entries))
	}
	e := entries[0]
	if e.ID == "" {
		t.Error("ID should not be empty")
	}
	if e.TS.IsZero() {
		t.Error("TS should not be zero")
	}
	if len(e.Keys) != 2 || e.Keys[0] != "API_KEY" || e.Keys[1] != "SECRET_TOKEN" {
		t.Errorf("Keys = %v, want [API_KEY SECRET_TOKEN]", e.Keys)
	}
}

func TestQueryRotations_Tail(t *testing.T) {
	l := openTemp(t)

	for i := 0; i < 4; i++ {
		if err := l.RecordRotation([]string{"KEY"}); err != nil {
			t.Fatalf("RecordRotation: %v", err)
		}
	}

	entries, err := l.QueryRotations(2, 0)
	if err != nil {
		t.Fatalf("QueryRotations: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries with tail=2, got %d", len(entries))
	}
}

func TestQueryEvents_MixedAndFiltered(t *testing.T) {
	l := openTemp(t)

	if err := l.Record(activitylog.Entry{Tool: "my_tool", Status: 200, TS: time.Now().Add(-5 * time.Second)}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := l.RecordRotation([]string{"API_KEY"}); err != nil {
		t.Fatalf("RecordRotation: %v", err)
	}

	// All events, newest-first.
	all, err := l.QueryEvents("", 0, 0, "")
	if err != nil {
		t.Fatalf("QueryEvents all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 events, got %d", len(all))
	}
	if all[0].Kind() != "rotation" || all[1].Kind() != "request" {
		t.Errorf("expected rotation then request (newest-first), got %s then %s", all[0].Kind(), all[1].Kind())
	}

	// Filter to requests only.
	reqs, err := l.QueryEvents("", 0, 0, "request")
	if err != nil {
		t.Fatalf("QueryEvents request: %v", err)
	}
	if len(reqs) != 1 || reqs[0].Request == nil || reqs[0].Request.Tool != "my_tool" {
		t.Errorf("unexpected request results: %+v", reqs)
	}

	// Filter to rotations only.
	rots, err := l.QueryEvents("", 0, 0, "rotation")
	if err != nil {
		t.Fatalf("QueryEvents rotation: %v", err)
	}
	if len(rots) != 1 || rots[0].Rotation == nil || rots[0].Rotation.Keys[0] != "API_KEY" {
		t.Errorf("unexpected rotation results: %+v", rots)
	}

	// Tail applied across both tables.
	limited, err := l.QueryEvents("", 1, 0, "")
	if err != nil {
		t.Fatalf("QueryEvents tail=1: %v", err)
	}
	if len(limited) != 1 {
		t.Errorf("expected 1 event with tail=1, got %d", len(limited))
	}

	// Tool filter applies to requests only; rotations still appear.
	filtered, err := l.QueryEvents("my_tool", 0, 0, "")
	if err != nil {
		t.Fatalf("QueryEvents tool filter: %v", err)
	}
	if len(filtered) != 2 {
		t.Errorf("expected 2 events (1 request + 1 rotation), got %d", len(filtered))
	}
}

func TestQueryRotations_Since(t *testing.T) {
	l := openTemp(t)

	if err := l.RecordRotation([]string{"KEY"}); err != nil {
		t.Fatalf("RecordRotation: %v", err)
	}

	// The rotation was just recorded, so it should appear within a 30-minute window.
	entries, err := l.QueryRotations(0, 30*time.Minute)
	if err != nil {
		t.Fatalf("QueryRotations: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 recent rotation entry, got %d", len(entries))
	}
}

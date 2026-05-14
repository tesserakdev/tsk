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
	if err := runLogs(dir, 0, "", 0, "", &buf); err != nil {
		t.Fatalf("runLogs: %v", err)
	}

	if got := buf.String(); got != "No entries found.\n" {
		t.Errorf("got %q, want %q", got, "No entries found.\n")
	}
}

func TestRunLogs_ShowsRequests(t *testing.T) {
	dir, log := setupLogsDB(t)

	tools := []string{"tool_alpha", "tool_beta", "tool_gamma"}
	for _, name := range tools {
		if err := log.Record(activitylog.Entry{Tool: name, Status: 200}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	var buf bytes.Buffer
	if err := runLogs(dir, 0, "", 0, "", &buf); err != nil {
		t.Fatalf("runLogs: %v", err)
	}

	out := buf.String()
	for _, name := range tools {
		if !strings.Contains(out, name) {
			t.Errorf("output missing tool %q:\n%s", name, out)
		}
	}
}

func TestRunLogs_ShowsRotations(t *testing.T) {
	dir, log := setupLogsDB(t)

	if err := log.RecordRotation([]string{"API_KEY", "SECRET"}); err != nil {
		t.Fatalf("RecordRotation: %v", err)
	}

	var buf bytes.Buffer
	if err := runLogs(dir, 0, "", 0, "", &buf); err != nil {
		t.Fatalf("runLogs: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "rotation") {
		t.Errorf("output missing rotation event:\n%s", out)
	}
	if !strings.Contains(out, "API_KEY") {
		t.Errorf("output missing key name:\n%s", out)
	}
}

func TestRunLogs_MixedOrder(t *testing.T) {
	dir, log := setupLogsDB(t)

	base := time.Now().Add(-10 * time.Second)
	if err := log.Record(activitylog.Entry{Tool: "early_tool", Status: 200, TS: base}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	// Rotation logged after the request.
	if err := log.RecordRotation([]string{"MY_KEY"}); err != nil {
		t.Fatalf("RecordRotation: %v", err)
	}

	var buf bytes.Buffer
	if err := runLogs(dir, 0, "", 0, "", &buf); err != nil {
		t.Fatalf("runLogs: %v", err)
	}

	out := buf.String()
	rotIdx := strings.Index(out, "rotation")
	reqIdx := strings.Index(out, "early_tool")
	if rotIdx < 0 || reqIdx < 0 {
		t.Fatalf("missing events in output:\n%s", out)
	}
	// Newest-first: rotation (recorded now) should appear before the older request.
	if rotIdx > reqIdx {
		t.Errorf("rotation should appear before older request (newest-first):\n%s", out)
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
	if err := runLogs(dir, 2, "", 0, "", &buf); err != nil {
		t.Fatalf("runLogs: %v", err)
	}

	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines with tail=2, got %d\noutput:\n%s", len(lines), out)
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
	if err := runLogs(dir, 0, "tool_a", 0, "", &buf); err != nil {
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

	if err := log.Record(activitylog.Entry{
		Tool:   "old_tool",
		Status: 200,
		TS:     time.Now().Add(-2 * time.Hour),
	}); err != nil {
		t.Fatalf("Record old entry: %v", err)
	}
	if err := log.Record(activitylog.Entry{
		Tool:   "recent_tool",
		Status: 200,
		TS:     time.Now().Add(-1 * time.Minute),
	}); err != nil {
		t.Fatalf("Record recent entry: %v", err)
	}

	var buf bytes.Buffer
	if err := runLogs(dir, 0, "", 30*time.Minute, "", &buf); err != nil {
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

func TestRunLogs_TypeRequestFilter(t *testing.T) {
	dir, log := setupLogsDB(t)

	if err := log.Record(activitylog.Entry{Tool: "my_tool", Status: 200}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := log.RecordRotation([]string{"KEY"}); err != nil {
		t.Fatalf("RecordRotation: %v", err)
	}

	var buf bytes.Buffer
	if err := runLogs(dir, 0, "", 0, "request", &buf); err != nil {
		t.Fatalf("runLogs: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "rotation") {
		t.Errorf("--type=request should exclude rotation events:\n%s", out)
	}
	if !strings.Contains(out, "my_tool") {
		t.Errorf("output missing request entry:\n%s", out)
	}
}

func TestRunLogs_TypeRotationFilter(t *testing.T) {
	dir, log := setupLogsDB(t)

	if err := log.Record(activitylog.Entry{Tool: "my_tool", Status: 200}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := log.RecordRotation([]string{"API_KEY"}); err != nil {
		t.Fatalf("RecordRotation: %v", err)
	}

	var buf bytes.Buffer
	if err := runLogs(dir, 0, "", 0, "rotation", &buf); err != nil {
		t.Fatalf("runLogs: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "my_tool") {
		t.Errorf("--type=rotation should exclude request events:\n%s", out)
	}
	if !strings.Contains(out, "API_KEY") {
		t.Errorf("output missing rotation entry:\n%s", out)
	}
}

func TestRunLogs_ScrubActionsShown(t *testing.T) {
	dir, log := setupLogsDB(t)

	if err := log.Record(activitylog.Entry{Tool: "t", Status: 200, ScrubActions: 3}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	var buf bytes.Buffer
	if err := runLogs(dir, 0, "", 0, "", &buf); err != nil {
		t.Fatalf("runLogs: %v", err)
	}

	if !strings.Contains(buf.String(), "[3 scrubs]") {
		t.Errorf("expected [3 scrubs] in output:\n%s", buf.String())
	}
}

func TestRunLogs_ZeroScrubsHidden(t *testing.T) {
	dir, log := setupLogsDB(t)

	if err := log.Record(activitylog.Entry{Tool: "t", Status: 200, ScrubActions: 0}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	var buf bytes.Buffer
	if err := runLogs(dir, 0, "", 0, "", &buf); err != nil {
		t.Fatalf("runLogs: %v", err)
	}

	if strings.Contains(buf.String(), "scrubs") {
		t.Errorf("expected scrubs to be hidden when count is 0:\n%s", buf.String())
	}
}

func TestRunLogs_InvalidTypeReturnsError(t *testing.T) {
	dir, _ := setupLogsDB(t)

	var buf bytes.Buffer
	err := runLogs(dir, 0, "", 0, "badvalue", &buf)
	if err == nil {
		t.Fatal("expected error for unknown --type value, got nil")
	}
	if !strings.Contains(err.Error(), "badvalue") {
		t.Errorf("error should mention the bad value: %v", err)
	}
}

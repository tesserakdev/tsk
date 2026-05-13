package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInit_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "tsk")

	if err := runInit(target, &bytes.Buffer{}); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected a directory")
	}
	if got := info.Mode().Perm(); got != 0700 {
		t.Errorf("dir mode = %o, want 0700", got)
	}
}

func TestRunInit_CreatesSecretsFile(t *testing.T) {
	dir := t.TempDir()

	if err := runInit(dir, &bytes.Buffer{}); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	path := filepath.Join(dir, ".secrets")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat .secrets: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf(".secrets mode = %o, want 0600", got)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read .secrets: %v", err)
	}
	if !strings.Contains(string(data), "tsk secrets") {
		t.Errorf(".secrets content does not contain expected comment text")
	}
}

func TestRunInit_CreatesRulesFile(t *testing.T) {
	dir := t.TempDir()

	if err := runInit(dir, &bytes.Buffer{}); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	path := filepath.Join(dir, "rules.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rules.yaml: %v", err)
	}
	if !strings.Contains(string(data), "version: 1") {
		t.Errorf("rules.yaml does not contain 'version: 1'")
	}
}

func TestRunInit_Idempotent(t *testing.T) {
	dir := t.TempDir()

	if err := runInit(dir, &bytes.Buffer{}); err != nil {
		t.Fatalf("first runInit: %v", err)
	}

	// Write a marker to .secrets
	secretsPath := filepath.Join(dir, ".secrets")
	marker := "MARKER_VALUE=test123\n"
	if err := os.WriteFile(secretsPath, []byte(marker), 0600); err != nil {
		t.Fatalf("writing marker: %v", err)
	}

	// Second call should succeed and not overwrite the file
	if err := runInit(dir, &bytes.Buffer{}); err != nil {
		t.Fatalf("second runInit: %v", err)
	}

	data, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatalf("read .secrets after second init: %v", err)
	}
	if string(data) != marker {
		t.Errorf(".secrets was overwritten: got %q, want %q", string(data), marker)
	}
}

func TestRunInit_OutputContainsNextSteps(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer

	if err := runInit(dir, &buf); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "tsk initialized at") {
		t.Errorf("output missing 'tsk initialized at': %q", out)
	}
	if !strings.Contains(out, ".secrets") {
		t.Errorf("output missing '.secrets' reference: %q", out)
	}
	if !strings.Contains(out, "rules.yaml") {
		t.Errorf("output missing 'rules.yaml' reference: %q", out)
	}
	if !strings.Contains(out, "tsk run") {
		t.Errorf("output missing 'tsk run': %q", out)
	}
}

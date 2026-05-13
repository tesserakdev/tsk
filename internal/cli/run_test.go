package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTskDir(t *testing.T, rules, secrets string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rules.yaml"), []byte(rules), 0644); err != nil {
		t.Fatalf("write rules.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".secrets"), []byte(secrets), 0600); err != nil {
		t.Fatalf("write .secrets: %v", err)
	}
	return dir
}

func TestRunServer_MissingRulesFile(t *testing.T) {
	dir := t.TempDir()

	_, _, err := buildServer(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "loading config") {
		t.Errorf("error %q does not contain 'loading config'", err.Error())
	}
}

func TestRunServer_MissingSecretsFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rules.yaml"), []byte("version: 1\ntools: []\n"), 0644); err != nil {
		t.Fatalf("write rules.yaml: %v", err)
	}
	// No .secrets file

	_, _, err := buildServer(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "loading secrets") {
		t.Errorf("error %q does not contain 'loading secrets'", err.Error())
	}
}

func TestRunServer_InvalidConfig(t *testing.T) {
	dir := setupTskDir(t, "version: !!invalid yaml: [", "")

	_, _, err := buildServer(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "loading config") {
		t.Errorf("error %q does not contain 'loading config'", err.Error())
	}
}

func TestRunServer_UndefinedSecretRef(t *testing.T) {
	rules := `version: 1
tools:
  - name: my_tool
    description: "A tool that references an undefined secret."
    type: http
    endpoint: https://example.com/api
    method: GET
    auth: "Bearer ${MISSING_KEY}"
`
	dir := setupTskDir(t, rules, "# empty\n")

	_, _, err := buildServer(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "undefined") {
		t.Errorf("error %q does not contain 'undefined'", err.Error())
	}
}

func TestRunServer_ValidStartup(t *testing.T) {
	rules := "version: 1\ntools: []\n"
	dir := setupTskDir(t, rules, "# empty\n")

	server, cleanup, err := buildServer(context.Background(), dir)
	if err != nil {
		t.Fatalf("buildServer: %v", err)
	}
	defer cleanup()

	if server == nil {
		t.Fatal("expected non-nil server")
	}
}

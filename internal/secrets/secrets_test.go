package secrets_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tesserakdev/tsk/internal/secrets"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".secrets")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_ValidFile(t *testing.T) {
	path := writeFile(t, "FOO=bar\nBAZ=qux\n")
	m, err := secrets.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", m["FOO"], "bar")
	}
	if m["BAZ"] != "qux" {
		t.Errorf("BAZ = %q, want %q", m["BAZ"], "qux")
	}
}

func TestLoad_SkipsCommentsAndBlanks(t *testing.T) {
	path := writeFile(t, "# comment\n\nKEY=value\n  # another comment\n\n")
	m, err := secrets.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 1 {
		t.Errorf("expected 1 entry, got %d", len(m))
	}
	if m["KEY"] != "value" {
		t.Errorf("KEY = %q, want %q", m["KEY"], "value")
	}
}

func TestLoad_ValueWithEquals(t *testing.T) {
	path := writeFile(t, "TOKEN=abc=def=ghi\n")
	m, err := secrets.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["TOKEN"] != "abc=def=ghi" {
		t.Errorf("TOKEN = %q, want %q", m["TOKEN"], "abc=def=ghi")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := secrets.Load("/nonexistent/path/.secrets")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_InsecurePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".secrets")
	if err := os.WriteFile(path, []byte("KEY=value\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := secrets.Load(path)
	if err == nil {
		t.Fatal("expected error for 0644 permissions, got nil")
	}
	if !strings.Contains(err.Error(), "insecure permissions") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestLoad_SecurePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".secrets")
	if err := os.WriteFile(path, []byte("KEY=value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := secrets.Load(path)
	if err != nil {
		t.Fatalf("unexpected error for 0600 permissions: %v", err)
	}
	if m["KEY"] != "value" {
		t.Errorf("KEY = %q, want %q", m["KEY"], "value")
	}
}

func TestInterpolate_KnownKey(t *testing.T) {
	m := map[string]string{"TOKEN": "secret123"}
	got := secrets.Interpolate("bearer ${TOKEN}", m)
	want := "bearer secret123"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInterpolate_UnknownKey(t *testing.T) {
	m := map[string]string{}
	got := secrets.Interpolate("bearer ${UNKNOWN}", m)
	want := "bearer ${UNKNOWN}"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInterpolate_MultipleKeys(t *testing.T) {
	m := map[string]string{"A": "hello", "B": "world"}
	got := secrets.Interpolate("${A} ${B}!", m)
	want := "hello world!"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInterpolate_NoPlaceholders(t *testing.T) {
	m := map[string]string{"X": "y"}
	got := secrets.Interpolate("no placeholders here", m)
	want := "no placeholders here"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestValidateRefs_AllPresent(t *testing.T) {
	m := map[string]string{"TOKEN": "secret", "KEY": "value"}
	if err := secrets.ValidateRefs(m, "bearer ${TOKEN}", "https://api.example.com/${KEY}/v1"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateRefs_MissingKey(t *testing.T) {
	m := map[string]string{"TOKEN": "secret"}
	err := secrets.ValidateRefs(m, "bearer ${TOKEN}", "https://api.example.com/${MISSING_KEY}/v1")
	if err == nil {
		t.Fatal("expected error for missing secret reference, got nil")
	}
	if !strings.Contains(err.Error(), "MISSING_KEY") {
		t.Errorf("error should mention the missing key, got: %v", err)
	}
}

func TestValidateRefs_MultipleMissing(t *testing.T) {
	m := map[string]string{}
	err := secrets.ValidateRefs(m, "bearer ${A}", "${B} and ${C}")
	if err == nil {
		t.Fatal("expected error for multiple missing refs, got nil")
	}
	for _, key := range []string{"A", "B", "C"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error should mention %q, got: %v", key, err)
		}
	}
}

func TestValidateRefs_EmptyStrings(t *testing.T) {
	m := map[string]string{}
	if err := secrets.ValidateRefs(m, "", "no refs here"); err != nil {
		t.Errorf("unexpected error for strings with no refs: %v", err)
	}
}

func TestValidateRefs_NilMap(t *testing.T) {
	err := secrets.ValidateRefs(nil, "bearer ${TOKEN}")
	if err == nil {
		t.Fatal("expected error for nil map with ref, got nil")
	}
}

func TestFileProvider_Interpolate(t *testing.T) {
	path := writeFile(t, "TOKEN=secret123\n")
	p, err := secrets.NewFileProvider(path)
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}
	got, err := p.Interpolate(context.Background(), "bearer ${TOKEN}")
	if err != nil {
		t.Fatalf("Interpolate: %v", err)
	}
	if got != "bearer secret123" {
		t.Errorf("got %q, want %q", got, "bearer secret123")
	}
}

func TestFileProvider_Validate_Missing(t *testing.T) {
	path := writeFile(t, "# empty\n")
	p, err := secrets.NewFileProvider(path)
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}
	if err := p.Validate([]string{"bearer ${MISSING}"}); err == nil {
		t.Fatal("expected error for missing ref, got nil")
	}
}

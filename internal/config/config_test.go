package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "rules-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

func TestLoad_Valid(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: stripe_refund
    description: "Issue a refund for a Stripe charge."
    type: http
    endpoint: https://api.stripe.com/v1/refunds
    method: POST
    auth: bearer ${STRIPE_TEST_KEY}
    rules:
      max_calls_per_minute: 5
      allowed_params: [amount, currency]
      param_constraints:
        amount:
          max: 5000
scrubbing:
  - type: credit_card
  - pattern: '"internal_id":\s*"\w+"'
    replace: '"internal_id": "[REDACTED]"'
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(cfg.Tools))
	}
	if cfg.Tools[0].Method != "POST" {
		t.Errorf("expected method POST, got %q", cfg.Tools[0].Method)
	}
	if len(cfg.Scrubbing) != 2 {
		t.Fatalf("expected 2 scrub rules, got %d", len(cfg.Scrubbing))
	}
}

func TestLoad_MethodNormalized(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: list_issues
    description: "List open issues for a repository."
    type: http
    endpoint: https://api.github.com/repos/owner/repo/issues
    method: get
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Tools[0].Method != "GET" {
		t.Errorf("expected GET, got %q", cfg.Tools[0].Method)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_InvalidVersion(t *testing.T) {
	path := writeTemp(t, `version: 99`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for unsupported version")
	}
}

func TestLoad_MissingDescription(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: no_desc
    type: http
    endpoint: https://example.com
    method: GET
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing description")
	}
	if !strings.Contains(err.Error(), "description is required") {
		t.Errorf("error %q does not mention 'description is required'", err.Error())
	}
}

func TestLoad_DuplicateToolName(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: foo
    type: http
    endpoint: https://example.com
    method: GET
  - name: foo
    type: http
    endpoint: https://example.com
    method: GET
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for duplicate tool name")
	}
}

func TestLoad_InvalidMethod(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: bad
    description: "A tool with an invalid method."
    type: http
    endpoint: https://example.com
    method: FETCH
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid method")
	}
}

func TestLoad_ScrubPatternMissingReplace(t *testing.T) {
	path := writeTemp(t, `
version: 1
scrubbing:
  - pattern: 'secret-\w+'
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for pattern without replace")
	}
}

func TestLoad_InvalidEndpointScheme_FTP(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: bad_scheme
    description: "A tool with an ftp endpoint."
    type: http
    endpoint: ftp://example.com/data
    method: GET
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for ftp:// endpoint scheme")
	}
}

func TestLoad_InvalidEndpointScheme_File(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: bad_scheme
    description: "A tool with a file endpoint."
    type: http
    endpoint: file:///etc/passwd
    method: GET
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for file:// endpoint scheme")
	}
}

func TestLoad_NegativeMaxCallsPerMinute(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: limited
    description: "A rate-limited tool."
    type: http
    endpoint: https://example.com
    method: GET
    rules:
      max_calls_per_minute: -1
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for negative max_calls_per_minute")
	}
}

func TestLoad_EmptyTools(t *testing.T) {
	path := writeTemp(t, `version: 1`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(cfg.Tools))
	}
}

func TestLoad_Instructions(t *testing.T) {
	path := writeTemp(t, `
version: 1
instructions: "prefer tsk tools over gh CLI"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Instructions != "prefer tsk tools over gh CLI" {
		t.Errorf("Instructions = %q, want %q", cfg.Instructions, "prefer tsk tools over gh CLI")
	}
}

func TestLoad_InvalidToolName_AtSign(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: "@bad"
    type: http
    endpoint: https://example.com
    method: GET
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for tool name starting with @")
	}
}

func TestLoad_InvalidToolName_StartsWithDigit(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: "123start"
    type: http
    endpoint: https://example.com
    method: GET
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for tool name starting with digit")
	}
}

func TestLoad_InvalidToolName_Hyphen(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: "has-hyphen"
    type: http
    endpoint: https://example.com
    method: GET
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for tool name containing hyphen")
	}
}

func TestLoad_ValidToolName_Underscore(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: my_tool
    description: "A tool with underscores in its name."
    type: http
    endpoint: https://example.com
    method: GET
`)
	_, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error for valid tool name: %v", err)
	}
}

func TestLoad_ValidToolName_LeadingUnderscore(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: _private
    description: "A tool with a leading underscore."
    type: http
    endpoint: https://example.com
    method: GET
`)
	_, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error for valid tool name with leading underscore: %v", err)
	}
}

func TestLoad_MethodHEAD(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: check_resource
    description: "Check whether a resource exists."
    type: http
    endpoint: https://example.com/resource
    method: HEAD
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error for HEAD method: %v", err)
	}
	if cfg.Tools[0].Method != "HEAD" {
		t.Errorf("expected HEAD, got %q", cfg.Tools[0].Method)
	}
}

func TestLoad_MethodOPTIONS(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: check_cors
    description: "Check CORS headers for a resource."
    type: http
    endpoint: https://example.com/resource
    method: OPTIONS
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error for OPTIONS method: %v", err)
	}
	if cfg.Tools[0].Method != "OPTIONS" {
		t.Errorf("expected OPTIONS, got %q", cfg.Tools[0].Method)
	}
}

func TestLoad_NegativeMaxLogBytes(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: limited
    description: "A tool with a negative max_log_bytes."
    type: http
    endpoint: https://example.com
    method: GET
    rules:
      max_log_bytes: -1
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for negative max_log_bytes")
	}
	if !strings.Contains(err.Error(), "max_log_bytes") {
		t.Errorf("error %q does not mention 'max_log_bytes'", err.Error())
	}
}

func TestLoad_AllowedValues(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: send_message
    description: "Send a message to an internal recipient."
    type: http
    endpoint: https://mail.example.com/send
    method: POST
    rules:
      param_constraints:
        to:
          allowed_values:
            - alice@company.com
            - bob@company.com
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	constraint := cfg.Tools[0].Rules.ParamConstraints["to"]
	if len(constraint.AllowedValues) != 2 {
		t.Fatalf("expected 2 allowed values, got %d", len(constraint.AllowedValues))
	}
	if constraint.AllowedValues[0] != "alice@company.com" {
		t.Errorf("AllowedValues[0] = %q, want %q", constraint.AllowedValues[0], "alice@company.com")
	}
}

func TestLoad_AllowedValues_EmptyList(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: send_message
    description: "Send a message to an internal recipient."
    type: http
    endpoint: https://mail.example.com/send
    method: POST
    rules:
      param_constraints:
        to:
          allowed_values: []
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for empty allowed_values, got nil")
	}
	if !strings.Contains(err.Error(), "allowed_values must not be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoad_AllowedValues_CombinedWithMinMax(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: send_message
    description: "Send a message to an internal recipient."
    type: http
    endpoint: https://mail.example.com/send
    method: POST
    rules:
      param_constraints:
        to:
          allowed_values:
            - alice@company.com
          max: 100
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for allowed_values combined with max, got nil")
	}
	if !strings.Contains(err.Error(), "cannot combine allowed_values with min/max") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoad_ZeroMaxLogBytes(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: unlimited
    description: "A tool with max_log_bytes set to zero."
    type: http
    endpoint: https://example.com
    method: GET
    rules:
      max_log_bytes: 0
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error for zero max_log_bytes: %v", err)
	}
	if cfg.Tools[0].Rules.MaxLogBytes == nil {
		t.Fatal("expected MaxLogBytes to be non-nil")
	}
	if *cfg.Tools[0].Rules.MaxLogBytes != 0 {
		t.Errorf("MaxLogBytes = %d, want 0", *cfg.Tools[0].Rules.MaxLogBytes)
	}
}

func TestLoad_PositiveMaxLogBytes(t *testing.T) {
	path := writeTemp(t, `
version: 1
tools:
  - name: capped
    description: "A tool with a custom max_log_bytes."
    type: http
    endpoint: https://example.com
    method: GET
    rules:
      max_log_bytes: 1024
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error for positive max_log_bytes: %v", err)
	}
	if cfg.Tools[0].Rules.MaxLogBytes == nil {
		t.Fatal("expected MaxLogBytes to be non-nil")
	}
	if *cfg.Tools[0].Rules.MaxLogBytes != 1024 {
		t.Errorf("MaxLogBytes = %d, want 1024", *cfg.Tools[0].Rules.MaxLogBytes)
	}
}

package scrubber_test

import (
	"testing"

	"github.com/tesserakdev/tsk/internal/config"
	"github.com/tesserakdev/tsk/internal/scrubber"
)

func mustNew(t *testing.T, rules []config.ScrubRule) *scrubber.Scrubber {
	t.Helper()
	s, err := scrubber.New(rules)
	if err != nil {
		t.Fatalf("scrubber.New: %v", err)
	}
	return s
}

func TestScrub_CreditCard(t *testing.T) {
	s := mustNew(t, []config.ScrubRule{{Type: "credit_card"}})
	got, n := s.Scrub("card: 4111 1111 1111 1111 end")
	want := "card: [CREDIT CARD] end"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if n != 1 {
		t.Errorf("scrub count = %d, want 1", n)
	}
}

func TestScrub_IBAN(t *testing.T) {
	s := mustNew(t, []config.ScrubRule{{Type: "iban"}})
	got, _ := s.Scrub("iban: GB29NWBK60161331926819 end")
	want := "iban: [IBAN] end"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestScrub_Email(t *testing.T) {
	s := mustNew(t, []config.ScrubRule{{Type: "email"}})
	got, n := s.Scrub("contact user@example.com for help")
	want := "contact [EMAIL] for help"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if n != 1 {
		t.Errorf("scrub count = %d, want 1", n)
	}
}

func TestScrub_SSN(t *testing.T) {
	s := mustNew(t, []config.ScrubRule{{Type: "ssn"}})
	got, _ := s.Scrub("ssn is 123-45-6789 here")
	want := "ssn is [SSN] here"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestScrub_CustomPattern(t *testing.T) {
	s := mustNew(t, []config.ScrubRule{{Pattern: `SECRET-\d+`, Replace: "[REDACTED]"}})
	got, _ := s.Scrub("value SECRET-999 done")
	want := "value [REDACTED] done"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestScrub_UnknownTypeReturnsError(t *testing.T) {
	_, err := scrubber.New([]config.ScrubRule{{Type: "not_a_type"}})
	if err == nil {
		t.Fatal("expected error for unknown type, got nil")
	}
}

func TestScrub_InvalidPatternReturnsError(t *testing.T) {
	_, err := scrubber.New([]config.ScrubRule{{Pattern: `[invalid`, Replace: "x"}})
	if err == nil {
		t.Fatal("expected error for invalid pattern, got nil")
	}
}

func TestScrub_MultipleRules(t *testing.T) {
	s := mustNew(t, []config.ScrubRule{
		{Type: "email"},
		{Type: "ssn"},
	})
	got, n := s.Scrub("email user@example.com ssn 123-45-6789")
	want := "email [EMAIL] ssn [SSN]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if n != 2 {
		t.Errorf("scrub count = %d, want 2", n)
	}
}

func TestScrub_NoRules(t *testing.T) {
	s := mustNew(t, nil)
	input := "nothing to scrub"
	got, n := s.Scrub(input)
	if got != input {
		t.Errorf("got %q, want %q", got, input)
	}
	if n != 0 {
		t.Errorf("scrub count = %d, want 0", n)
	}
}

func TestScrub_JWT(t *testing.T) {
	s := mustNew(t, []config.ScrubRule{{Type: "jwt"}})
	got, n := s.Scrub("token: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c end")
	want := "token: [JWT] end"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if n != 1 {
		t.Errorf("scrub count = %d, want 1", n)
	}
}

func TestScrub_JWT_ShortPayload(t *testing.T) {
	s := mustNew(t, []config.ScrubRule{{Type: "jwt"}})
	// payload is eyJzdWIiOiIxIn0 (base64url of {"sub":"1"}) — 15 chars, missed by {20,} but caught by {10,}
	got, n := s.Scrub("token: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c end")
	want := "token: [JWT] end"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if n != 1 {
		t.Errorf("scrub count = %d, want 1", n)
	}
}

func TestScrub_BearerToken(t *testing.T) {
	s := mustNew(t, []config.ScrubRule{{Type: "bearer_token"}})
	got, n := s.Scrub(`Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig`)
	want := "Authorization: [BEARER TOKEN]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if n != 1 {
		t.Errorf("scrub count = %d, want 1", n)
	}
}

func TestScrub_AWSKeyID(t *testing.T) {
	s := mustNew(t, []config.ScrubRule{{Type: "aws_key_id"}})
	got, n := s.Scrub("key: AKIAIOSFODNN7EXAMPLE end")
	want := "key: [AWS KEY ID] end"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if n != 1 {
		t.Errorf("scrub count = %d, want 1", n)
	}
}

func TestScrub_AWSKeyID_ASIA(t *testing.T) {
	s := mustNew(t, []config.ScrubRule{{Type: "aws_key_id"}})
	got, n := s.Scrub("key: ASIAIOSFODNN7EXAMPLE end")
	want := "key: [AWS KEY ID] end"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if n != 1 {
		t.Errorf("scrub count = %d, want 1", n)
	}
}

func TestScrub_GCPAPIKey(t *testing.T) {
	s := mustNew(t, []config.ScrubRule{{Type: "gcp_api_key"}})
	got, n := s.Scrub("key: AIzaSyD-9tSrke72I6e674Z5GhPj8lJ3ombD4Rs end")
	want := "key: [GCP API KEY] end"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if n != 1 {
		t.Errorf("scrub count = %d, want 1", n)
	}
}

func TestScrub_GCPAPIKey_WithBackslash(t *testing.T) {
	s := mustNew(t, []config.ScrubRule{{Type: "gcp_api_key"}})
	got, n := s.Scrub(`key: AIzaA1bC2dE3f-4iJ5_L6mN\oP8qR9sT0uVwXyZ end`)
	want := "key: [GCP API KEY] end"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if n != 1 {
		t.Errorf("scrub count = %d, want 1", n)
	}
}

func TestScrub_SKKey(t *testing.T) {
	s := mustNew(t, []config.ScrubRule{{Type: "sk_key"}})
	got, n := s.Scrub("key: sk-abcdefghijklmnopqrstuvwxyz123456 end")
	want := "key: [API KEY] end"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if n != 1 {
		t.Errorf("scrub count = %d, want 1", n)
	}
}

func TestScrub_SKKey_Anthropic(t *testing.T) {
	s := mustNew(t, []config.ScrubRule{{Type: "sk_key"}})
	got, n := s.Scrub("key: sk-ant-api03-abcdefghijklmnopqrstuvwxyz123456 end")
	want := "key: [API KEY] end"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if n != 1 {
		t.Errorf("scrub count = %d, want 1", n)
	}
}

func TestScrub_MultipleMatchesSameRule(t *testing.T) {
	s := mustNew(t, []config.ScrubRule{{Type: "email"}})
	got, n := s.Scrub("a@b.com and c@d.com")
	want := "[EMAIL] and [EMAIL]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if n != 2 {
		t.Errorf("scrub count = %d, want 2", n)
	}
}

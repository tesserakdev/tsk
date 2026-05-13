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

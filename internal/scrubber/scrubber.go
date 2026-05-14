// Package scrubber redacts sensitive values from response strings using
// built-in pattern types and custom regular expressions.
package scrubber

import (
	"fmt"
	"regexp"

	"github.com/tesserakdev/tsk/internal/config"
)

// rule is a compiled scrub rule: a regex and its replacement string.
type rule struct {
	re      *regexp.Regexp
	replace string
}

// Scrubber applies a sequence of redaction rules to strings.
// Scrubber is safe for concurrent use.
type Scrubber struct {
	rules []rule
}

// builtins maps type name → (pattern, replacement).
var builtins = map[string][2]string{
	"credit_card":  {`\b(?:\d[ -]?){12,15}\d\b`, "[CREDIT CARD]"},
	"iban":         {`\b[A-Z]{2}\d{2}[A-Z0-9]{4}\d{7}(?:[A-Z0-9]{0,16})?\b`, "[IBAN]"},
	"email":        {`\b[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}\b`, "[EMAIL]"},
	"ssn":          {`\b\d{3}-\d{2}-\d{4}\b`, "[SSN]"},
	"jwt":          {`\b[A-Za-z0-9\-_]{10,}\.[A-Za-z0-9\-_]{10,}\.[A-Za-z0-9\-_]{10,}\b`, "[JWT]"},
	"bearer_token": {`(?i)bearer\s+[A-Za-z0-9\-._~+/]+=*`, "[BEARER TOKEN]"},
	"aws_key_id":   {`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`, "[AWS KEY ID]"},
	"gcp_api_key":  {`\bAIza[A-Za-z0-9\-_\\]{35}\b`, "[GCP API KEY]"},
	"sk_key":       {`\bsk-(?:ant-)?[A-Za-z0-9\-_]{20,}\b`, "[API KEY]"},
}

// New builds a Scrubber from config scrub rules.
// Returns an error if a custom regex fails to compile or a built-in type is unknown.
func New(rules []config.ScrubRule) (*Scrubber, error) {
	s := &Scrubber{}
	for i, r := range rules {
		if r.Type != "" {
			builtin, ok := builtins[r.Type]
			if !ok {
				return nil, fmt.Errorf("scrubbing[%d]: unknown built-in type %q", i, r.Type)
			}
			re, err := regexp.Compile(builtin[0])
			if err != nil {
				return nil, fmt.Errorf("scrubbing[%d]: compiling built-in %q: %w", i, r.Type, err)
			}
			s.rules = append(s.rules, rule{re: re, replace: builtin[1]})
		} else {
			re, err := regexp.Compile(r.Pattern)
			if err != nil {
				return nil, fmt.Errorf("scrubbing[%d]: compiling pattern: %w", i, err)
			}
			s.rules = append(s.rules, rule{re: re, replace: r.Replace})
		}
	}
	return s, nil
}

// Scrub applies all rules to input in order and returns the redacted string
// together with the total number of replacements made across all rules.
func (s *Scrubber) Scrub(input string) (string, int) {
	out := input
	total := 0
	for _, r := range s.rules {
		out = r.re.ReplaceAllStringFunc(out, func(match string) string {
			total++
			return r.replace
		})
	}
	return out, total
}

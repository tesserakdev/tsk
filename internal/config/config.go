// Package config loads and validates the tsk rules.yaml configuration file.
package config

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// validToolName matches identifiers that start with a letter or underscore
// and contain only letters, digits, and underscores.
var validToolName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// Config is the top-level structure parsed from rules.yaml.
type Config struct {
	Version      int         `yaml:"version"`
	Instructions string      `yaml:"instructions"`
	Tools        []Tool      `yaml:"tools"`
	Scrubbing    []ScrubRule `yaml:"scrubbing"`
}

// Tool describes a single MCP tool: its HTTP endpoint, authentication, and
// per-tool rules.
type Tool struct {
	Name        string    `yaml:"name"`
	Description string    `yaml:"description"`
	Type        string    `yaml:"type"`
	Endpoint    string    `yaml:"endpoint"`
	Method      string    `yaml:"method"`
	Auth        string    `yaml:"auth"`
	Rules       ToolRules `yaml:"rules"`
}

// ToolRules holds the rate limiting, parameter filtering, and constraint
// settings for a single tool.
type ToolRules struct {
	MaxCallsPerMinute int                        `yaml:"max_calls_per_minute"`
	AllowedParams     []string                   `yaml:"allowed_params"`
	ParamConstraints  map[string]ParamConstraint `yaml:"param_constraints"`
	// MaxLogBytes caps the number of bytes of the scrubbed response body stored
	// in the audit log. 0 means no truncation. Nil means use the server default.
	MaxLogBytes *int `yaml:"max_log_bytes"`
}

// ParamConstraint defines optional constraints for a single parameter.
// AllowedValues restricts the parameter to an explicit set of string values.
// Min and Max enforce numeric bounds.
type ParamConstraint struct {
	Max           *float64 `yaml:"max"`
	Min           *float64 `yaml:"min"`
	AllowedValues []string `yaml:"allowed_values"`
}

// ScrubRule describes one response-scrubbing rule, either a built-in named
// type (credit_card, iban, email, ssn) or a custom regex pattern with its
// replacement string.
type ScrubRule struct {
	// Built-in type: credit_card, iban, email, ssn
	Type string `yaml:"type"`
	// Custom regex pattern + replacement
	Pattern string `yaml:"pattern"`
	Replace string `yaml:"replace"`
}

var validMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true,
	"HEAD": true, "OPTIONS": true,
}

// Provider loads tsk configuration.
type Provider interface {
	Load(ctx context.Context) (*Config, error)
}

// FileProvider loads configuration from a rules.yaml file.
type FileProvider struct {
	path string
}

// NewFileProvider returns a Provider that reads from the YAML file at path.
func NewFileProvider(path string) *FileProvider {
	return &FileProvider{path: path}
}

// Load reads and validates the configuration file.
func (p *FileProvider) Load(_ context.Context) (*Config, error) {
	return Load(p.path)
}

// Load reads and validates the rules.yaml file at path, returning a parsed
// Config or an error if the file is missing, malformed, or invalid.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported version %d, only version 1 is supported", c.Version)
	}

	seen := make(map[string]bool)
	for i, t := range c.Tools {
		if t.Name == "" {
			return fmt.Errorf("tool[%d]: name is required", i)
		}
		if !validToolName.MatchString(t.Name) {
			return fmt.Errorf("tool %q: name must match ^[a-zA-Z_][a-zA-Z0-9_]*$", t.Name)
		}
		if seen[t.Name] {
			return fmt.Errorf("tool[%d]: duplicate tool name %q", i, t.Name)
		}
		seen[t.Name] = true

		if t.Description == "" {
			return fmt.Errorf("tool %q: description is required", t.Name)
		}

		if t.Type == "" {
			return fmt.Errorf("tool %q: type is required", t.Name)
		}
		if t.Type != "http" {
			return fmt.Errorf("tool %q: unsupported type %q (only \"http\" is supported)", t.Name, t.Type)
		}
		if t.Endpoint == "" {
			return fmt.Errorf("tool %q: endpoint is required", t.Name)
		}
		method := strings.ToUpper(t.Method)
		if !validMethods[method] {
			return fmt.Errorf("tool %q: invalid method %q (must be GET, POST, PUT, PATCH, DELETE, HEAD, or OPTIONS)", t.Name, t.Method)
		}
		c.Tools[i].Method = method

		if t.Rules.MaxCallsPerMinute < 0 {
			return fmt.Errorf("tool %q: max_calls_per_minute cannot be negative", t.Name)
		}
		if t.Rules.MaxLogBytes != nil && *t.Rules.MaxLogBytes < 0 {
			return fmt.Errorf("tool %q: max_log_bytes cannot be negative", t.Name)
		}

		for param, constraint := range t.Rules.ParamConstraints {
			if constraint.AllowedValues != nil && len(constraint.AllowedValues) == 0 {
				return fmt.Errorf("tool %q: param %q allowed_values must not be empty", t.Name, param)
			}
			if len(constraint.AllowedValues) > 0 && (constraint.Min != nil || constraint.Max != nil) {
				return fmt.Errorf("tool %q: param %q cannot combine allowed_values with min/max", t.Name, param)
			}
		}

		u, err := url.Parse(t.Endpoint)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("tool %q: endpoint must be a valid http or https URL", t.Name)
		}
	}

	for i, s := range c.Scrubbing {
		if s.Type == "" && s.Pattern == "" {
			return fmt.Errorf("scrubbing[%d]: either type or pattern is required", i)
		}
		if s.Type != "" && s.Pattern != "" {
			return fmt.Errorf("scrubbing[%d]: type and pattern are mutually exclusive", i)
		}
		if s.Pattern != "" && s.Replace == "" {
			return fmt.Errorf("scrubbing[%d]: replace is required when pattern is set", i)
		}
	}

	return nil
}

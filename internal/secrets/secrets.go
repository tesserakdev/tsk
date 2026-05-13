package secrets

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
)

// Provider resolves ${KEY} references in strings at call time.
type Provider interface {
	Interpolate(ctx context.Context, s string) (string, error)
}

// FileProvider loads secrets from a file and implements Provider.
type FileProvider struct {
	m map[string]string
}

// NewFileProvider reads the secrets file at path and returns a Provider.
func NewFileProvider(path string) (*FileProvider, error) {
	m, err := Load(path)
	if err != nil {
		return nil, err
	}
	return &FileProvider{m: m}, nil
}

// Interpolate replaces ${KEY} references using the loaded secrets.
func (p *FileProvider) Interpolate(_ context.Context, s string) (string, error) {
	return Interpolate(s, p.m), nil
}

// Validate checks that all ${KEY} references in the given strings are present
// in the loaded secrets. This is an OSS startup check; call it once after
// loading config.
func (p *FileProvider) Validate(refs []string) error {
	return ValidateRefs(p.m, refs...)
}

// MapProvider is an in-memory Provider backed by a plain map. Intended for
// tests and cases where secrets are already resolved before construction.
type MapProvider struct {
	m map[string]string
}

// NewMapProvider returns a Provider backed by m. A nil map is valid and causes
// all unknown references to be left as-is.
func NewMapProvider(m map[string]string) *MapProvider {
	return &MapProvider{m: m}
}

// Interpolate replaces ${KEY} references using the map.
func (p *MapProvider) Interpolate(_ context.Context, s string) (string, error) {
	return Interpolate(s, p.m), nil
}

// Load reads a KEY=value secrets file, skipping blank lines and # comments.
// It returns an error if the file has group- or world-readable permissions.
func Load(path string) (map[string]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat secrets file: %w", err)
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("secrets file %s has insecure permissions %o (must be 0600)", path, info.Mode().Perm())
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening secrets file: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			slog.Error("failed to close secrets file", slog.Any("error", err))
		}
	}()

	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			return nil, fmt.Errorf("secrets file line %d: invalid format (expected KEY=value)", lineNum)
		}
		key := strings.TrimSpace(line[:idx])
		value := line[idx+1:]
		if key == "" {
			return nil, fmt.Errorf("secrets file line %d: empty key", lineNum)
		}
		result[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading secrets file: %w", err)
	}
	return result, nil
}

// ValidateRefs checks that all ${KEY} references in the given strings are
// present in the secrets map. Returns an error listing any missing keys.
func ValidateRefs(secrets map[string]string, values ...string) error {
	missing := make(map[string]struct{})
	for _, s := range values {
		remaining := s
		for {
			start := strings.Index(remaining, "${")
			if start < 0 {
				break
			}
			rest := remaining[start+2:]
			end := strings.Index(rest, "}")
			if end < 0 {
				break
			}
			key := rest[:end]
			if _, ok := secrets[key]; !ok {
				missing[key] = struct{}{}
			}
			remaining = rest[end+1:]
		}
	}
	if len(missing) == 0 {
		return nil
	}
	keys := make([]string, 0, len(missing))
	for k := range missing {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return fmt.Errorf("undefined secret references: %s", strings.Join(keys, ", "))
}

// Interpolate replaces ${KEY} references in s with values from the secrets map.
// Unknown keys are left as-is.
func Interpolate(s string, secrets map[string]string) string {
	var b strings.Builder
	remaining := s
	for {
		start := strings.Index(remaining, "${")
		if start < 0 {
			b.WriteString(remaining)
			break
		}
		b.WriteString(remaining[:start])
		rest := remaining[start+2:]
		end := strings.Index(rest, "}")
		if end < 0 {
			// No closing brace — emit literally and stop
			b.WriteString("${")
			b.WriteString(rest)
			break
		}
		key := rest[:end]
		if val, ok := secrets[key]; ok {
			b.WriteString(val)
		} else {
			b.WriteString("${")
			b.WriteString(key)
			b.WriteString("}")
		}
		remaining = rest[end+1:]
	}
	return b.String()
}

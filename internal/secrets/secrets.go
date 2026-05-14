package secrets

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Provider resolves ${KEY} references in strings at call time.
type Provider interface {
	Interpolate(ctx context.Context, s string) (string, error)
}

// FileProvider loads secrets from a file and implements Provider.
// It reloads the file on each Interpolate call when the file's modification
// time has changed, enabling live credential rotation without restarting.
type FileProvider struct {
	path     string
	mu       sync.RWMutex
	m        map[string]string
	modTime  time.Time
	onRotate func([]string)
}

// NewFileProvider reads the secrets file at path and returns a Provider.
func NewFileProvider(path string) (*FileProvider, error) {
	m, err := Load(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat secrets file: %w", err)
	}
	return &FileProvider{path: path, m: m, modTime: info.ModTime()}, nil
}

// SetOnRotate registers a callback invoked with the names of changed, added,
// or removed keys whenever the secrets file is reloaded. The callback receives
// key names only — never values. It is called synchronously outside the lock.
func (p *FileProvider) SetOnRotate(fn func([]string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onRotate = fn
}

// maybeReload checks the file's modification time and reloads if it has changed.
// On any failure (stat error, bad permissions, parse error) the existing secrets
// are retained so that in-flight requests are not disrupted.
func (p *FileProvider) maybeReload() {
	info, err := os.Stat(p.path)
	if err != nil {
		return
	}

	p.mu.RLock()
	same := info.ModTime().Equal(p.modTime)
	p.mu.RUnlock()

	if same {
		return
	}

	p.mu.Lock()
	// Double-check after acquiring write lock in case another goroutine beat us.
	if info.ModTime().Equal(p.modTime) {
		p.mu.Unlock()
		return
	}

	newSecrets, err := Load(p.path)
	if err != nil {
		p.mu.Unlock()
		slog.Warn("secrets file changed but could not be reloaded; keeping existing secrets", slog.Any("error", err))
		return
	}

	changed := diffKeys(p.m, newSecrets)
	p.m = newSecrets
	p.modTime = info.ModTime()
	cb := p.onRotate
	p.mu.Unlock()

	if cb != nil && len(changed) > 0 {
		cb(changed)
	}
}

// Interpolate replaces ${KEY} references using the loaded secrets, reloading
// the file first if its modification time has changed.
func (p *FileProvider) Interpolate(_ context.Context, s string) (string, error) {
	p.maybeReload()
	p.mu.RLock()
	defer p.mu.RUnlock()
	return Interpolate(s, p.m), nil
}

// diffKeys returns the names of keys that were added, removed, or whose values
// changed between old and updated. Values are never included in the result.
func diffKeys(old, updated map[string]string) []string {
	seen := make(map[string]struct{}, len(updated))
	var changed []string
	for k, v := range updated {
		seen[k] = struct{}{}
		if old[k] != v {
			changed = append(changed, k)
		}
	}
	for k := range old {
		if _, ok := seen[k]; !ok {
			changed = append(changed, k)
		}
	}
	sort.Strings(changed)
	return changed
}

// Validate checks that all ${KEY} references in the given strings are present
// in the loaded secrets. This is an OSS startup check; call it once after
// loading config.
func (p *FileProvider) Validate(refs []string) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
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

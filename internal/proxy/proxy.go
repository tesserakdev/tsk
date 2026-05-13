// Package proxy performs HTTP tool calls on behalf of the MCP server.
package proxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/tesserakdev/tsk/internal/config"
	"github.com/tesserakdev/tsk/internal/secrets"
)

const (
	kb = 1024
	mb = 1024 * kb
)

const maxResponseSize = 10 * mb

// pathParamRe matches {param_name} placeholders in endpoint URLs.
var pathParamRe = regexp.MustCompile(`\{([^}]+)\}`)

// Executor executes HTTP tool calls.
type Executor struct {
	sec          secrets.Provider
	client       *http.Client
	allowPrivate bool // bypass SSRF check; for use in tests only
}

// New creates an Executor with the given secrets provider.
func New(sec secrets.Provider) *Executor {
	return &Executor{
		sec: sec,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DialContext:         (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
				TLSHandshakeTimeout: 10 * time.Second,
				MaxIdleConns:        100,
				IdleConnTimeout:     90 * time.Second,
				TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
			},
		},
	}
}

// NewWithPrivate creates an Executor that skips the SSRF check for private/loopback addresses.
// This is intended only for testing.
func NewWithPrivate(sec secrets.Provider) *Executor {
	e := New(sec)
	e.allowPrivate = true
	return e
}

// Result holds the outcome of a successful HTTP exchange.
// StatusCode is 0 when no HTTP response was obtained (pre-request or transport error).
type Result struct {
	Body       string
	StatusCode int
}

// Execute performs the HTTP call for tool.
func (e *Executor) Execute(ctx context.Context, tool config.Tool, params map[string]any) (Result, error) {
	endpoint, err := e.sec.Interpolate(ctx, tool.Endpoint)
	if err != nil {
		return Result{}, fmt.Errorf("interpolating endpoint: %w", err)
	}
	auth, err := e.sec.Interpolate(ctx, tool.Auth)
	if err != nil {
		return Result{}, fmt.Errorf("interpolating auth: %w", err)
	}

	paramsCopy := make(map[string]any, len(params))
	for k, v := range params {
		paramsCopy[k] = v
	}

	if len(tool.Rules.AllowedParams) > 0 {
		allowed := make(map[string]bool, len(tool.Rules.AllowedParams))
		for _, p := range tool.Rules.AllowedParams {
			allowed[p] = true
		}
		for k := range paramsCopy {
			if !allowed[k] {
				delete(paramsCopy, k)
			}
		}
	}

	for name, constraint := range tool.Rules.ParamConstraints {
		val, ok := paramsCopy[name]
		if !ok {
			continue
		}

		fval, ok := val.(float64)
		if !ok {
			// Try integer types that may come from callers.
			switch v := val.(type) {
			case int:
				fval = float64(v)
				ok = true
			case int64:
				fval = float64(v)
				ok = true
			}
		}
		if !ok {
			return Result{}, fmt.Errorf("param %q must be numeric (constraint defined)", name)
		}

		num := fval
		if constraint.Max != nil && num > *constraint.Max {
			return Result{}, fmt.Errorf("param %q value %v exceeds maximum %v", name, num, *constraint.Max)
		}

		if constraint.Min != nil && num < *constraint.Min {
			return Result{}, fmt.Errorf("param %q value %v is below minimum %v", name, num, *constraint.Min)
		}
	}

	endpoint = pathParamRe.ReplaceAllStringFunc(endpoint, func(match string) string {
		name := match[1 : len(match)-1]
		if val, ok := paramsCopy[name]; ok {
			delete(paramsCopy, name)
			return url.PathEscape(fmt.Sprintf("%v", val))
		}

		return match
	})

	if !e.allowPrivate && isPrivateEndpoint(endpoint) {
		return Result{}, fmt.Errorf("endpoint targets a private or reserved address")
	}

	method := strings.ToUpper(tool.Method)

	var reqQuery, reqBody map[string]any
	if method == http.MethodGet {
		reqQuery = paramsCopy
	} else {
		reqBody = paramsCopy
	}

	req, err := buildRequest(ctx, method, endpoint, reqQuery, reqBody)
	if err != nil {
		return Result{}, fmt.Errorf("building request: %w", err)
	}

	if auth != "" {
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			auth = "Bearer " + strings.TrimSpace(auth[7:])
		}

		req.Header.Set("Authorization", auth)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("executing request: %w", err)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Error("failed to close response body", slog.Any("error", err))
		}
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return Result{StatusCode: resp.StatusCode}, fmt.Errorf("reading response: %w", err)
	}

	if int64(len(body)) > maxResponseSize {
		return Result{StatusCode: resp.StatusCode}, fmt.Errorf("response exceeds maximum size of %d bytes", maxResponseSize)
	}

	return Result{Body: string(body), StatusCode: resp.StatusCode}, nil
}

// buildRequest builds an HTTP request with the given method, endpoint, query parameters, and body.
func buildRequest(ctx context.Context, method, endpoint string, query, body map[string]any) (*http.Request, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parsing endpoint: %w", err)
	}

	if len(query) > 0 {
		q := u.Query()
		for k, v := range query {
			q.Set(k, fmt.Sprintf("%v", v))
		}
		u.RawQuery = q.Encode()
	}

	var bodyReader io.Reader
	if len(body) > 0 {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshalling body: %w", err)
		}

		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), bodyReader)
	if err != nil {
		return nil, err
	}

	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, nil
}

// isPrivateEndpoint returns true if rawURL resolves to a loopback, private,
// or link-local address, or is a known cloud metadata hostname.
func isPrivateEndpoint(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	host := u.Hostname()
	ip := net.ParseIP(host)
	if ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}

	// Also block common metadata hostnames.
	blocked := []string{"localhost", "metadata.google.internal", "169.254.169.254"}
	for _, b := range blocked {
		if strings.EqualFold(host, b) {
			return true
		}
	}

	return false
}

package proxy_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tesserakdev/tsk/internal/config"
	"github.com/tesserakdev/tsk/internal/proxy"
	"github.com/tesserakdev/tsk/internal/secrets"
)

func TestExecute_GET(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		q := r.URL.Query()
		if q.Get("name") != "alice" {
			t.Errorf("query param name = %q, want %q", q.Get("name"), "alice")
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	e := proxy.NewWithPrivate(secrets.NewMapProvider(nil))
	tool := config.Tool{
		Name:     "test",
		Type:     "http",
		Method:   "GET",
		Endpoint: srv.URL + "/users",
	}
	result, err := e.Execute(context.Background(), tool, map[string]any{"name": "alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Body != `{"ok":true}` {
		t.Errorf("body = %q, want %q", result.Body, `{"ok":true}`)
	}
}

func TestExecute_POST(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var payload map[string]any
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &payload); err != nil {
			t.Errorf("could not parse body: %v", err)
		}
		if payload["amount"] != float64(42) {
			t.Errorf("amount = %v, want 42", payload["amount"])
		}
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	e := proxy.NewWithPrivate(secrets.NewMapProvider(nil))
	tool := config.Tool{
		Name:     "test",
		Type:     "http",
		Method:   "POST",
		Endpoint: srv.URL + "/pay",
	}
	result, err := e.Execute(context.Background(), tool, map[string]any{"amount": 42})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Body != `{"status":"ok"}` {
		t.Errorf("body = %q, want %q", result.Body, `{"status":"ok"}`)
	}
}

func TestExecute_PathParamSubstitution(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/42" {
			t.Errorf("path = %q, want /users/42", r.URL.Path)
		}
		// "id" should not appear in query string
		if r.URL.Query().Get("id") != "" {
			t.Errorf("id should not be in query string")
		}
		w.Write([]byte(`{"id":42}`))
	}))
	defer srv.Close()

	e := proxy.NewWithPrivate(secrets.NewMapProvider(nil))
	tool := config.Tool{
		Name:     "get_user",
		Type:     "http",
		Method:   "GET",
		Endpoint: srv.URL + "/users/{id}",
	}
	result, err := e.Execute(context.Background(), tool, map[string]any{"id": 42})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Body != `{"id":42}` {
		t.Errorf("body = %q, want %q", result.Body, `{"id":42}`)
	}
}

func TestExecute_PathParamSpecialChars(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RawPath
		if gotPath == "" {
			gotPath = r.URL.Path
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	e := proxy.NewWithPrivate(secrets.NewMapProvider(nil))
	tool := config.Tool{
		Name:     "get_item",
		Type:     "http",
		Method:   "GET",
		Endpoint: srv.URL + "/items/{id}",
	}
	// Value with a space and slash — both must be percent-encoded in the path.
	_, err := e.Execute(context.Background(), tool, map[string]any{"id": "hello world/test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// url.PathEscape encodes space as %20 and slash as %2F.
	want := "/items/hello%20world%2Ftest"
	if gotPath != want {
		t.Errorf("raw path = %q, want %q", gotPath, want)
	}
}

func TestExecute_AuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer my-token" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer my-token")
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	sec := map[string]string{"TOKEN": "my-token"}
	e := proxy.NewWithPrivate(secrets.NewMapProvider(sec))
	tool := config.Tool{
		Name:     "secure",
		Type:     "http",
		Method:   "GET",
		Endpoint: srv.URL + "/secure",
		Auth:     "bearer ${TOKEN}",
	}
	_, err := e.Execute(context.Background(), tool, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecute_SecretInterpolatedInEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`ok`))
	}))
	defer srv.Close()

	// We can't fully test domain interpolation with httptest, but we can confirm
	// the path is interpolated correctly.
	sec := map[string]string{"VERSION": "v2"}
	e := proxy.NewWithPrivate(secrets.NewMapProvider(sec))
	called := false
	inner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/v2/items" {
			t.Errorf("path = %q, want /v2/items", r.URL.Path)
		}
		w.Write([]byte(`done`))
	}))
	defer inner.Close()

	tool := config.Tool{
		Name:     "items",
		Type:     "http",
		Method:   "GET",
		Endpoint: inner.URL + "/${VERSION}/items",
	}
	_, err := e.Execute(context.Background(), tool, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("server was not called")
	}
	_ = srv
}

func TestExecute_SSRF_LoopbackIP(t *testing.T) {
	e := proxy.New(secrets.NewMapProvider(nil))
	tool := config.Tool{
		Name:     "ssrf",
		Type:     "http",
		Method:   "GET",
		Endpoint: "http://127.0.0.1/secret",
	}
	_, err := e.Execute(context.Background(), tool, nil)
	if err == nil {
		t.Fatal("expected SSRF error for loopback IP, got nil")
	}
	if !strings.Contains(err.Error(), "private or reserved") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestExecute_SSRF_Localhost(t *testing.T) {
	e := proxy.New(secrets.NewMapProvider(nil))
	tool := config.Tool{
		Name:     "ssrf",
		Type:     "http",
		Method:   "GET",
		Endpoint: "http://localhost/secret",
	}
	_, err := e.Execute(context.Background(), tool, nil)
	if err == nil {
		t.Fatal("expected SSRF error for localhost, got nil")
	}
	if !strings.Contains(err.Error(), "private or reserved") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestExecute_SSRF_PrivateRange(t *testing.T) {
	e := proxy.New(secrets.NewMapProvider(nil))
	tool := config.Tool{
		Name:     "ssrf",
		Type:     "http",
		Method:   "GET",
		Endpoint: "http://192.168.1.1/data",
	}
	_, err := e.Execute(context.Background(), tool, nil)
	if err == nil {
		t.Fatal("expected SSRF error for private IP range, got nil")
	}
	if !strings.Contains(err.Error(), "private or reserved") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestExecute_SSRF_MetadataEndpoint(t *testing.T) {
	e := proxy.New(secrets.NewMapProvider(nil))
	tool := config.Tool{
		Name:     "ssrf",
		Type:     "http",
		Method:   "GET",
		Endpoint: "http://metadata.google.internal/computeMetadata/v1/",
	}
	_, err := e.Execute(context.Background(), tool, nil)
	if err == nil {
		t.Fatal("expected SSRF error for metadata endpoint, got nil")
	}
	if !strings.Contains(err.Error(), "private or reserved") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestExecute_ResponseSizeLimit(t *testing.T) {
	// Return a response larger than 10MB.
	const responseSize = 10*1024*1024 + 1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write responseSize bytes of 'x'.
		chunk := make([]byte, 64*1024)
		for i := range chunk {
			chunk[i] = 'x'
		}
		remaining := responseSize
		for remaining > 0 {
			n := remaining
			if n > len(chunk) {
				n = len(chunk)
			}
			w.Write(chunk[:n])
			remaining -= n
		}
	}))
	defer srv.Close()

	e := proxy.NewWithPrivate(secrets.NewMapProvider(nil))
	tool := config.Tool{
		Name:     "big",
		Type:     "http",
		Method:   "GET",
		Endpoint: srv.URL + "/big",
	}
	_, err := e.Execute(context.Background(), tool, nil)
	if err == nil {
		t.Fatal("expected error for oversized response, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestExecute_StatusCode(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
	}{
		{"200 OK", http.StatusOK},
		{"404 Not Found", http.StatusNotFound},
		{"400 Bad Request", http.StatusBadRequest},
		{"500 Internal Server Error", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				w.Write([]byte(`{"error":"something"}`))
			}))
			defer srv.Close()

			e := proxy.NewWithPrivate(secrets.NewMapProvider(nil))
			tool := config.Tool{
				Name:     "status_tool",
				Type:     "http",
				Method:   "GET",
				Endpoint: srv.URL + "/data",
			}
			result, err := e.Execute(context.Background(), tool, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.StatusCode != tc.statusCode {
				t.Errorf("status = %d, want %d", result.StatusCode, tc.statusCode)
			}
		})
	}
}

func ptr(f float64) *float64 { return &f }

func TestExecute_ConstraintNonNumericParam(t *testing.T) {
	e := proxy.New(secrets.NewMapProvider(nil))
	tool := config.Tool{
		Name:     "constrained",
		Type:     "http",
		Method:   "POST",
		Endpoint: "https://example.com/pay",
		Rules: config.ToolRules{
			ParamConstraints: map[string]config.ParamConstraint{
				"amount": {Max: ptr(100)},
			},
		},
	}
	_, err := e.Execute(context.Background(), tool, map[string]any{"amount": "not-a-number"})
	if err == nil {
		t.Fatal("expected error for non-numeric constrained param, got nil")
	}
	if !strings.Contains(err.Error(), "must be numeric") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestExecute_AllowedParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("allowed") != "yes" {
			t.Errorf("allowed param missing or wrong: %q", q.Get("allowed"))
		}
		if q.Get("forbidden") != "" {
			t.Errorf("forbidden param should have been stripped, got %q", q.Get("forbidden"))
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	e := proxy.NewWithPrivate(secrets.NewMapProvider(nil))
	tool := config.Tool{
		Name:     "filtered",
		Type:     "http",
		Method:   "GET",
		Endpoint: srv.URL + "/data",
		Rules: config.ToolRules{
			AllowedParams: []string{"allowed"},
		},
	}
	_, err := e.Execute(context.Background(), tool, map[string]any{
		"allowed":   "yes",
		"forbidden": "no",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecute_ParamConstraints_MaxViolation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	e := proxy.New(secrets.NewMapProvider(nil))
	tool := config.Tool{
		Name:     "constrained",
		Type:     "http",
		Method:   "POST",
		Endpoint: srv.URL + "/pay",
		Rules: config.ToolRules{
			ParamConstraints: map[string]config.ParamConstraint{
				"amount": {Max: ptr(100)},
			},
		},
	}
	_, err := e.Execute(context.Background(), tool, map[string]any{"amount": float64(200)})
	if err == nil {
		t.Fatal("expected error for amount exceeding max, got nil")
	}
}

func TestExecute_ParamConstraints_MinViolation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	e := proxy.New(secrets.NewMapProvider(nil))
	tool := config.Tool{
		Name:     "constrained",
		Type:     "http",
		Method:   "POST",
		Endpoint: srv.URL + "/pay",
		Rules: config.ToolRules{
			ParamConstraints: map[string]config.ParamConstraint{
				"amount": {Min: ptr(10)},
			},
		},
	}
	_, err := e.Execute(context.Background(), tool, map[string]any{"amount": float64(1)})
	if err == nil {
		t.Fatal("expected error for amount below min, got nil")
	}
}

func TestExecute_ParamConstraints_WithinBounds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	e := proxy.NewWithPrivate(secrets.NewMapProvider(nil))
	tool := config.Tool{
		Name:     "constrained",
		Type:     "http",
		Method:   "POST",
		Endpoint: srv.URL + "/pay",
		Rules: config.ToolRules{
			ParamConstraints: map[string]config.ParamConstraint{
				"amount": {Min: ptr(1), Max: ptr(100)},
			},
		},
	}
	_, err := e.Execute(context.Background(), tool, map[string]any{"amount": float64(50)})
	if err != nil {
		t.Fatalf("unexpected error for valid amount: %v", err)
	}
}

func TestExecute_AllowedValues_Pass(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	e := proxy.NewWithPrivate(secrets.NewMapProvider(nil))
	tool := config.Tool{
		Name:     "send_msg",
		Type:     "http",
		Method:   "POST",
		Endpoint: srv.URL + "/messages",
		Rules: config.ToolRules{
			ParamConstraints: map[string]config.ParamConstraint{
				"to": {AllowedValues: []string{"alice@company.com", "bob@company.com"}},
			},
		},
	}
	_, err := e.Execute(context.Background(), tool, map[string]any{"to": "alice@company.com"})
	if err != nil {
		t.Fatalf("unexpected error for allowed value: %v", err)
	}
}

func TestExecute_AllowedValues_Reject(t *testing.T) {
	e := proxy.New(secrets.NewMapProvider(nil))
	tool := config.Tool{
		Name:     "send_msg",
		Type:     "http",
		Method:   "POST",
		Endpoint: "https://example.com/messages",
		Rules: config.ToolRules{
			ParamConstraints: map[string]config.ParamConstraint{
				"to": {AllowedValues: []string{"alice@company.com", "bob@company.com"}},
			},
		},
	}
	_, err := e.Execute(context.Background(), tool, map[string]any{"to": "attacker@evil.com"})
	if err == nil {
		t.Fatal("expected error for disallowed value, got nil")
	}
	if !strings.Contains(err.Error(), "not in the allowed list") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestExecute_AllowedValues_StringParamNoNumericRequired(t *testing.T) {
	e := proxy.New(secrets.NewMapProvider(nil))
	tool := config.Tool{
		Name:     "send_msg",
		Type:     "http",
		Method:   "POST",
		Endpoint: "https://example.com/messages",
		Rules: config.ToolRules{
			ParamConstraints: map[string]config.ParamConstraint{
				"to": {AllowedValues: []string{"alice@company.com"}},
			},
		},
	}
	_, err := e.Execute(context.Background(), tool, map[string]any{"to": "hacker@evil.com"})
	if err == nil {
		t.Fatal("expected rejection for disallowed value")
	}
	if strings.Contains(err.Error(), "must be numeric") {
		t.Errorf("string param with allowed_values should not produce a numeric error: %v", err)
	}
}

func TestExecute_AllowedValues_NonStringRejected(t *testing.T) {
	e := proxy.New(secrets.NewMapProvider(nil))
	tool := config.Tool{
		Name:     "send_msg",
		Type:     "http",
		Method:   "POST",
		Endpoint: "https://example.com/messages",
		Rules: config.ToolRules{
			ParamConstraints: map[string]config.ParamConstraint{
				"to": {AllowedValues: []string{"alice@company.com"}},
			},
		},
	}
	_, err := e.Execute(context.Background(), tool, map[string]any{"to": 42})
	if err == nil {
		t.Fatal("expected error for non-string value with allowed_values constraint")
	}
	if !strings.Contains(err.Error(), "must be a string") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestExecute_AllowedValues_ParamAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	e := proxy.NewWithPrivate(secrets.NewMapProvider(nil))
	tool := config.Tool{
		Name:     "send_msg",
		Type:     "http",
		Method:   "POST",
		Endpoint: srv.URL + "/messages",
		Rules: config.ToolRules{
			ParamConstraints: map[string]config.ParamConstraint{
				"to": {AllowedValues: []string{"alice@company.com"}},
			},
		},
	}
	_, err := e.Execute(context.Background(), tool, map[string]any{"subject": "hello"})
	if err != nil {
		t.Fatalf("absent constrained param should not cause error: %v", err)
	}
}

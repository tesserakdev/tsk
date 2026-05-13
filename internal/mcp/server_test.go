package mcp_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tesserakdev/tsk/internal/activitylog"
	"github.com/tesserakdev/tsk/internal/config"
	"github.com/tesserakdev/tsk/internal/mcp"
	"github.com/tesserakdev/tsk/internal/proxy"
	"github.com/tesserakdev/tsk/internal/ratelimit"
	"github.com/tesserakdev/tsk/internal/scrubber"
	"github.com/tesserakdev/tsk/internal/secrets"
	"github.com/tesserakdev/tsk/internal/store"
)

// newTestServer builds an mcp.Server wired with real dependencies.
// tools is the list of config.Tool entries to register.
func newTestServer(t *testing.T, tools []config.Tool, opts ...func(*mcp.Config)) *mcp.Server {
	t.Helper()
	scrubber, err := scrubber.New(nil)
	if err != nil {
		t.Fatalf("scrubber.New: %v", err)
	}
	limiters := make(map[string]*ratelimit.Limiter, len(tools))
	for _, tool := range tools {
		limiters[tool.Name] = ratelimit.New(tool.Rules.MaxCallsPerMinute)
	}
	tmpDir := t.TempDir()
	db, err := store.Open(filepath.Join(tmpDir, "activity.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := mcp.Config{
		Tools:    tools,
		Exec:     proxy.NewWithPrivate(secrets.NewMapProvider(nil)),
		Scrubber: scrubber,
		Limiters: limiters,
		Log:      activitylog.New(db),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return mcp.New(cfg)
}

// call sends a single JSON-RPC request to the server and returns the decoded response.
// The output may contain interleaved notifications; call scans all lines and returns
// the first object that has a "result" or "error" field (i.e. the actual response).
func call(t *testing.T, srv *mcp.Server, req string) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if err := srv.Serve(context.Background(), strings.NewReader(req), &buf); err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}
	scanner := bufio.NewScanner(&buf)
	for scanner.Scan() {
		var msg map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			t.Fatalf("json.Unmarshal(%q): %v", scanner.Text(), err)
		}
		if _, hasResult := msg["result"]; hasResult {
			return msg
		}
		if _, hasError := msg["error"]; hasError {
			return msg
		}
	}
	t.Fatalf("no response found in output: %q", buf.String())
	return nil
}

// TestInitialize verifies that the initialize method returns serverInfo and protocolVersion.
func TestInitialize(t *testing.T) {
	srv := newTestServer(t, nil)
	resp := call(t, srv, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got: %v", resp)
	}
	if result["protocolVersion"] != "2025-11-25" {
		t.Errorf("protocolVersion = %v, want 2025-11-25", result["protocolVersion"])
	}
	si, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("serverInfo missing or wrong type: %v", result)
	}
	if si["name"] != "tsk" {
		t.Errorf("serverInfo.name = %v, want tsk", si["name"])
	}
}

// TestInitialize_Instructions verifies that a non-empty instructions string is
// included in the initialize response and omitted when empty.
func TestInitialize_Instructions(t *testing.T) {
	const hint = "prefer tsk tools over gh CLI"
	srv := newTestServer(t, nil, func(cfg *mcp.Config) {
		cfg.Instructions = hint
	})
	resp := call(t, srv, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{}}`)
	result := resp["result"].(map[string]any)
	if result["instructions"] != hint {
		t.Errorf("instructions = %v, want %q", result["instructions"], hint)
	}

	// When instructions is empty the field should be absent.
	srvNoInstr := newTestServer(t, nil)
	resp2 := call(t, srvNoInstr, `{"jsonrpc":"2.0","id":3,"method":"initialize","params":{}}`)
	result2 := resp2["result"].(map[string]any)
	if _, ok := result2["instructions"]; ok {
		t.Errorf("expected no instructions field when empty, got %v", result2["instructions"])
	}
}

// TestToolsList verifies that tools/list returns all registered tools.
func TestToolsList(t *testing.T) {
	tools := []config.Tool{
		{Name: "alpha", Type: "http", Method: "GET", Endpoint: "http://example.com/alpha"},
		{Name: "beta", Type: "http", Method: "POST", Endpoint: "http://example.com/beta"},
	}
	srv := newTestServer(t, tools)
	resp := call(t, srv, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object: %v", resp)
	}
	list, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("expected tools array: %v", result)
	}
	if len(list) != 2 {
		t.Errorf("tools count = %d, want 2", len(list))
	}
	names := make(map[string]bool)
	for _, item := range list {
		tool := item.(map[string]any)
		names[tool["name"].(string)] = true
	}
	for _, want := range []string{"alpha", "beta"} {
		if !names[want] {
			t.Errorf("tool %q not in list", want)
		}
	}
}

// TestToolsList_InputSchema verifies that AllowedParams are reflected in inputSchema.
func TestToolsList_InputSchema(t *testing.T) {
	tools := []config.Tool{
		{
			Name:     "transfer",
			Type:     "http",
			Method:   "POST",
			Endpoint: "http://example.com/transfer",
			Rules:    config.ToolRules{AllowedParams: []string{"amount", "currency"}},
		},
	}
	srv := newTestServer(t, tools)
	resp := call(t, srv, `{"jsonrpc":"2.0","id":20,"method":"tools/list","params":{}}`)

	result := resp["result"].(map[string]any)
	list := result["tools"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(list))
	}
	toolItem := list[0].(map[string]any)
	schema, ok := toolItem["inputSchema"].(map[string]any)
	if !ok {
		t.Fatalf("inputSchema missing or wrong type: %v", toolItem["inputSchema"])
	}
	if schema["type"] != "object" {
		t.Errorf("schema type = %v, want object", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing or wrong type: %v", schema["properties"])
	}
	for _, param := range []string{"amount", "currency"} {
		if _, found := props[param]; !found {
			t.Errorf("expected param %q in properties", param)
		}
	}
	// No constraints: both params should be typed as string.
	for _, param := range []string{"amount", "currency"} {
		p := props[param].(map[string]any)
		if p["type"] != "string" {
			t.Errorf("param %q type = %v, want string (no constraint set)", param, p["type"])
		}
	}
}

// TestToolsList_ConstraintInSchema verifies that param_constraints are reflected in inputSchema.
func TestToolsList_ConstraintInSchema(t *testing.T) {
	maxVal := float64(5000)
	tools := []config.Tool{
		{
			Name:     "refund",
			Type:     "http",
			Method:   "POST",
			Endpoint: "http://example.com/refund",
			Rules: config.ToolRules{
				AllowedParams: []string{"amount", "currency"},
				ParamConstraints: map[string]config.ParamConstraint{
					"amount": {Max: &maxVal},
				},
			},
		},
	}
	srv := newTestServer(t, tools)
	resp := call(t, srv, `{"jsonrpc":"2.0","id":21,"method":"tools/list","params":{}}`)

	result := resp["result"].(map[string]any)
	list := result["tools"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(list))
	}
	toolItem := list[0].(map[string]any)
	schema := toolItem["inputSchema"].(map[string]any)
	props := schema["properties"].(map[string]any)

	// "amount" has a max constraint — should be typed as number with maximum.
	amountProp, ok := props["amount"].(map[string]any)
	if !ok {
		t.Fatalf("amount property missing or wrong type: %v", props["amount"])
	}
	if amountProp["type"] != "number" {
		t.Errorf("amount type = %v, want number", amountProp["type"])
	}
	if amountProp["maximum"] != float64(5000) {
		t.Errorf("amount maximum = %v, want 5000", amountProp["maximum"])
	}
	if _, hasMin := amountProp["minimum"]; hasMin {
		t.Errorf("amount should not have minimum field when not set")
	}

	// "currency" has no constraint — should remain typed as string.
	currencyProp, ok := props["currency"].(map[string]any)
	if !ok {
		t.Fatalf("currency property missing or wrong type: %v", props["currency"])
	}
	if currencyProp["type"] != "string" {
		t.Errorf("currency type = %v, want string", currencyProp["type"])
	}
}

// TestToolsCall_Execute exercises tools/call with a real httptest.Server.
func TestToolsCall_Execute(t *testing.T) {
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"result":"ok"}`))
	}))
	defer httpSrv.Close()

	tool := config.Tool{
		Name:     "ping",
		Type:     "http",
		Method:   "GET",
		Endpoint: httpSrv.URL + "/ping",
	}
	srv := newTestServer(t, []config.Tool{tool})
	resp := call(t, srv, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ping","arguments":{}}}`)

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object: %v", resp)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected content array: %v", result)
	}
	first := content[0].(map[string]any)
	if first["text"] != `{"result":"ok"}` {
		t.Errorf("text = %v, want {\"result\":\"ok\"}", first["text"])
	}
}

// TestToolsCall_Execute_ScrubsAndLogs exercises scrubbing and activity log on a call.
func TestToolsCall_Execute_ScrubsAndLogs(t *testing.T) {
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`hello user@example.com`))
	}))
	defer httpSrv.Close()

	tool := config.Tool{
		Name:     "email_tool",
		Type:     "http",
		Method:   "GET",
		Endpoint: httpSrv.URL + "/data",
	}

	// Build server with email scrub rule.
	scrubber, err := scrubber.New([]config.ScrubRule{{Type: "email"}})
	if err != nil {
		t.Fatalf("scrubber.New: %v", err)
	}
	limiters := map[string]*ratelimit.Limiter{
		"email_tool": ratelimit.New(0),
	}
	tmpDir := t.TempDir()
	db, err := store.Open(filepath.Join(tmpDir, "activity.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	srv := mcp.New(mcp.Config{
		Tools:    []config.Tool{tool},
		Exec:     proxy.NewWithPrivate(secrets.NewMapProvider(nil)),
		Scrubber: scrubber,
		Limiters: limiters,
		Log:      activitylog.New(db),
	})

	var buf bytes.Buffer
	if err := srv.Serve(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"email_tool","arguments":{}}}`), &buf); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	result := resp["result"].(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if strings.Contains(text, "user@example.com") {
		t.Errorf("email not scrubbed from response: %q", text)
	}
	if !strings.Contains(text, "[EMAIL]") {
		t.Errorf("expected [EMAIL] placeholder in response: %q", text)
	}

	// Verify the activity log recorded the call with scrubbed response.
	alog := activitylog.New(db)
	entries, err := alog.Query("email_tool", 0, 0)
	if err != nil {
		t.Fatalf("log.Query: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one log entry")
	}
	if strings.Contains(entries[0].Response, "user@example.com") {
		t.Errorf("email not scrubbed from logged response: %q", entries[0].Response)
	}
	if entries[0].ScrubActions == 0 {
		t.Error("expected ScrubActions > 0 to indicate sensitive data was present")
	}
}

// TestToolsCall_UnknownTool expects -32602 for an unregistered tool name.
func TestToolsCall_UnknownTool(t *testing.T) {
	srv := newTestServer(t, nil)
	resp := call(t, srv, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nonexistent","arguments":{}}}`)

	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object: %v", resp)
	}
	if code := errObj["code"].(float64); code != -32602 {
		t.Errorf("error code = %v, want -32602", code)
	}
}

// TestToolsCall_RateLimited expects -32000 when the rate limit is exhausted.
func TestToolsCall_RateLimited(t *testing.T) {
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer httpSrv.Close()

	tool := config.Tool{
		Name:     "limited",
		Type:     "http",
		Method:   "GET",
		Endpoint: httpSrv.URL + "/x",
		Rules:    config.ToolRules{MaxCallsPerMinute: 1},
	}

	// Build server manually so we can use a pre-exhausted limiter.
	scrubber, _ := scrubber.New(nil)
	limiter := ratelimit.New(1)
	limiter.Allow() // consume the one allowed call
	limiters := map[string]*ratelimit.Limiter{"limited": limiter}

	tmpDir := t.TempDir()
	db, _ := store.Open(filepath.Join(tmpDir, "activity.db"))
	defer db.Close()

	srv := mcp.New(mcp.Config{
		Tools:    []config.Tool{tool},
		Exec:     proxy.NewWithPrivate(secrets.NewMapProvider(nil)),
		Scrubber: scrubber,
		Limiters: limiters,
		Log:      activitylog.New(db),
	})

	resp := call(t, srv, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"limited","arguments":{}}}`)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object: %v", resp)
	}
	if code := errObj["code"].(float64); code != -32000 {
		t.Errorf("error code = %v, want -32000", code)
	}
}

// TestUnknownMethod expects -32601.
func TestUnknownMethod(t *testing.T) {
	srv := newTestServer(t, nil)
	resp := call(t, srv, `{"jsonrpc":"2.0","id":6,"method":"no_such_method","params":{}}`)

	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object: %v", resp)
	}
	if code := errObj["code"].(float64); code != -32601 {
		t.Errorf("error code = %v, want -32601", code)
	}
}

// TestInvalidJSONRPCVersion expects -32600 when jsonrpc is not "2.0".
func TestInvalidJSONRPCVersion(t *testing.T) {
	srv := newTestServer(t, nil)
	resp := call(t, srv, `{"jsonrpc":"1.0","id":7,"method":"initialize","params":{}}`)

	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object: %v", resp)
	}
	if code := errObj["code"].(float64); code != -32600 {
		t.Errorf("error code = %v, want -32600", code)
	}
}

// TestToolsCall_LogsNonOKStatus verifies that non-200 HTTP responses from the
// upstream are recorded in the activity log with the actual status code.
func TestToolsCall_LogsNonOKStatus(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
	}{
		{"404", http.StatusNotFound},
		{"400", http.StatusBadRequest},
		{"500", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				w.Write([]byte(`{"error":"upstream error"}`))
			}))
			defer httpSrv.Close()

			tool := config.Tool{
				Name:     "upstream_tool",
				Type:     "http",
				Method:   "GET",
				Endpoint: httpSrv.URL + "/data",
			}

			sc, err := scrubber.New(nil)
			if err != nil {
				t.Fatalf("scrubber.New: %v", err)
			}
			limiters := map[string]*ratelimit.Limiter{
				"upstream_tool": ratelimit.New(0),
			}
			tmpDir := t.TempDir()
			db, err := store.Open(filepath.Join(tmpDir, "activity.db"))
			if err != nil {
				t.Fatalf("store.Open: %v", err)
			}
			defer db.Close()
			alog := activitylog.New(db)

			srv := mcp.New(mcp.Config{
				Tools:    []config.Tool{tool},
				Exec:     proxy.NewWithPrivate(secrets.NewMapProvider(nil)),
				Scrubber: sc,
				Limiters: limiters,
				Log:      alog,
			})

			var buf bytes.Buffer
			req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"upstream_tool","arguments":{}}}`
			if err := srv.Serve(context.Background(), strings.NewReader(req), &buf); err != nil {
				t.Fatalf("Serve: %v", err)
			}

			entries, err := alog.Query("upstream_tool", 1, 0)
			if err != nil {
				t.Fatalf("log.Query: %v", err)
			}
			if len(entries) == 0 {
				t.Fatal("expected a log entry")
			}
			if entries[0].Status != tc.statusCode {
				t.Errorf("logged status = %d, want %d", entries[0].Status, tc.statusCode)
			}
			if entries[0].Response != `{"error":"upstream error"}` {
				t.Errorf("logged response = %q, want %q", entries[0].Response, `{"error":"upstream error"}`)
			}
		})
	}
}

// TestToolsCall_MaxLogBytes_DefaultTruncation verifies that responses exceeding
// 4 KB are truncated in the activity log when max_log_bytes is not configured.
func TestToolsCall_MaxLogBytes_DefaultTruncation(t *testing.T) {
	body := strings.Repeat("a", 5000)
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer httpSrv.Close()

	tool := config.Tool{
		Name:     "big_tool",
		Type:     "http",
		Method:   "GET",
		Endpoint: httpSrv.URL + "/data",
		// MaxLogBytes nil → use server default of 4 KB
	}

	tmpDir := t.TempDir()
	db, err := store.Open(filepath.Join(tmpDir, "activity.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()
	alog := activitylog.New(db)

	sc, _ := scrubber.New(nil)
	srv := mcp.New(mcp.Config{
		Tools:    []config.Tool{tool},
		Exec:     proxy.NewWithPrivate(secrets.NewMapProvider(nil)),
		Scrubber: sc,
		Limiters: map[string]*ratelimit.Limiter{"big_tool": ratelimit.New(0)},
		Log:      alog,
	})

	var buf bytes.Buffer
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"big_tool","arguments":{}}}`
	if err := srv.Serve(context.Background(), strings.NewReader(req), &buf); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	entries, err := alog.Query("big_tool", 1, 0)
	if err != nil {
		t.Fatalf("log.Query: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected a log entry")
	}
	// Logged response must be shorter than the original 5000-byte body.
	if len(entries[0].Response) >= len(body) {
		t.Errorf("expected truncation: logged %d bytes, body was %d bytes", len(entries[0].Response), len(body))
	}
	if !strings.HasSuffix(entries[0].Response, "…") {
		t.Errorf("expected truncated response to end with '…', got: %q", entries[0].Response[len(entries[0].Response)-10:])
	}
}

// TestToolsCall_MaxLogBytes_ExplicitLimit verifies that a per-tool max_log_bytes
// overrides the server default.
func TestToolsCall_MaxLogBytes_ExplicitLimit(t *testing.T) {
	body := strings.Repeat("b", 500)
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer httpSrv.Close()

	limit := 50
	tool := config.Tool{
		Name:     "capped_tool",
		Type:     "http",
		Method:   "GET",
		Endpoint: httpSrv.URL + "/data",
		Rules:    config.ToolRules{MaxLogBytes: &limit},
	}

	tmpDir := t.TempDir()
	db, err := store.Open(filepath.Join(tmpDir, "activity.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()
	alog := activitylog.New(db)

	sc, _ := scrubber.New(nil)
	srv := mcp.New(mcp.Config{
		Tools:    []config.Tool{tool},
		Exec:     proxy.NewWithPrivate(secrets.NewMapProvider(nil)),
		Scrubber: sc,
		Limiters: map[string]*ratelimit.Limiter{"capped_tool": ratelimit.New(0)},
		Log:      alog,
	})

	var buf bytes.Buffer
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"capped_tool","arguments":{}}}`
	if err := srv.Serve(context.Background(), strings.NewReader(req), &buf); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	entries, err := alog.Query("capped_tool", 1, 0)
	if err != nil {
		t.Fatalf("log.Query: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected a log entry")
	}
	// Response must start with 50 'b' chars and end with the ellipsis.
	got := entries[0].Response
	if !strings.HasPrefix(got, strings.Repeat("b", limit)) {
		t.Errorf("expected first %d chars to be 'b', got: %q", limit, got[:min(len(got), limit+5)])
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected truncated response to end with '…', got suffix: %q", got[max(0, len(got)-10):])
	}
}

// TestToolsCall_MaxLogBytes_Zero verifies that max_log_bytes = 0 disables truncation.
func TestToolsCall_MaxLogBytes_Zero(t *testing.T) {
	body := strings.Repeat("c", 5000)
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer httpSrv.Close()

	zero := 0
	tool := config.Tool{
		Name:     "unlimited_tool",
		Type:     "http",
		Method:   "GET",
		Endpoint: httpSrv.URL + "/data",
		Rules:    config.ToolRules{MaxLogBytes: &zero},
	}

	tmpDir := t.TempDir()
	db, err := store.Open(filepath.Join(tmpDir, "activity.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()
	alog := activitylog.New(db)

	sc, _ := scrubber.New(nil)
	srv := mcp.New(mcp.Config{
		Tools:    []config.Tool{tool},
		Exec:     proxy.NewWithPrivate(secrets.NewMapProvider(nil)),
		Scrubber: sc,
		Limiters: map[string]*ratelimit.Limiter{"unlimited_tool": ratelimit.New(0)},
		Log:      alog,
	})

	var buf bytes.Buffer
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"unlimited_tool","arguments":{}}}`
	if err := srv.Serve(context.Background(), strings.NewReader(req), &buf); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	entries, err := alog.Query("unlimited_tool", 1, 0)
	if err != nil {
		t.Fatalf("log.Query: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected a log entry")
	}
	if entries[0].Response != body {
		t.Errorf("expected full %d-byte body, got %d bytes", len(body), len(entries[0].Response))
	}
}

// TestInvalidJSON expects -32700.
func TestInvalidJSON(t *testing.T) {
	srv := newTestServer(t, nil)
	var buf bytes.Buffer
	if err := srv.Serve(context.Background(), strings.NewReader(`{not valid json}`), &buf); err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object: %v", resp)
	}
	if code := errObj["code"].(float64); code != -32700 {
		t.Errorf("error code = %v, want -32700", code)
	}
}

// Package mcp implements a JSON-RPC 2.0 MCP server that dispatches tool calls
// through the proxy executor and logs activity.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/tesserakdev/tsk/internal/activitylog"
	"github.com/tesserakdev/tsk/internal/config"
	"github.com/tesserakdev/tsk/internal/proxy"
	"github.com/tesserakdev/tsk/internal/ratelimit"
	"github.com/tesserakdev/tsk/internal/scrubber"
	"github.com/tesserakdev/tsk/internal/version"
)

// maxLoggedResponse is the maximum number of bytes of a scrubbed response body
// stored in the activity log. Responses exceeding this are truncated.
const maxLoggedResponse = 4 * 1024

// Server is an MCP JSON-RPC 2.0 server that proxies tool calls to external
// HTTP APIs, enforcing rate limits and scrubbing responses.
type Server struct {
	tools        []config.Tool
	toolIdx      map[string]config.Tool
	exec         *proxy.Executor
	scrubber     *scrubber.Scrubber
	limiters     map[string]*ratelimit.Limiter
	log          activitylog.Logger
	instructions string
}

// Config holds the dependencies injected into a Server at construction time.
type Config struct {
	Tools        []config.Tool
	Exec         *proxy.Executor
	Scrubber     *scrubber.Scrubber
	Limiters     map[string]*ratelimit.Limiter
	Log          *activitylog.Log
	Instructions string
}

// New constructs a Server from the provided Config.
func New(cfg Config) *Server {
	idx := make(map[string]config.Tool, len(cfg.Tools))
	for _, t := range cfg.Tools {
		idx[t.Name] = t
	}
	return &Server{
		tools:        cfg.Tools,
		toolIdx:      idx,
		exec:         cfg.Exec,
		scrubber:     cfg.Scrubber,
		limiters:     cfg.Limiters,
		log:          cfg.Log,
		instructions: cfg.Instructions,
	}
}

// Serve reads newline-delimited JSON-RPC 2.0 requests from r and writes
// responses to w until ctx is cancelled or r returns EOF.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	enc := json.NewEncoder(w)

	writeResp := func(resp response) {
		if err := enc.Encode(resp); err != nil {
			slog.Error("failed to write response", "err", err)
		}
	}
	writeErr := func(id any, code int, msg string) {
		writeResp(response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
	}

	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			writeErr(nil, -32700, "parse error")
			continue
		}
		if req.JSONRPC != "2.0" {
			writeErr(req.ID, -32600, "invalid request: jsonrpc version must be 2.0")
			continue
		}

		switch req.Method {
		case "initialize":
			var initParams struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(req.Params, &initParams)
			proto := "2025-11-25"
			supported := map[string]bool{"2025-11-25": true, "2024-11-05": true}
			if supported[initParams.ProtocolVersion] {
				proto = initParams.ProtocolVersion
			}
			initResult := map[string]any{
				"serverInfo":      map[string]string{"name": "tsk", "version": version.Version},
				"protocolVersion": proto,
				"capabilities": map[string]any{
					"tools":   map[string]any{},
					"logging": map[string]any{},
				},
			}
			if s.instructions != "" {
				initResult["instructions"] = s.instructions
			}
			writeResp(response{JSONRPC: "2.0", ID: req.ID, Result: initResult})

		case "ping":
			writeResp(response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}})

		case "tools/list":
			defs := make([]toolDef, 0, len(s.tools))
			for _, t := range s.tools {
				var inputSchema map[string]any
				if len(t.Rules.AllowedParams) > 0 {
					props := make(map[string]any, len(t.Rules.AllowedParams))
					for _, p := range t.Rules.AllowedParams {
						if c, ok := t.Rules.ParamConstraints[p]; ok {
							prop := map[string]any{"type": "number"}
							if c.Min != nil {
								prop["minimum"] = *c.Min
							}
							if c.Max != nil {
								prop["maximum"] = *c.Max
							}
							props[p] = prop
						} else {
							props[p] = map[string]string{"type": "string"}
						}
					}
					inputSchema = map[string]any{
						"type":       "object",
						"properties": props,
					}
				} else {
					inputSchema = map[string]any{"type": "object"}
				}
				desc := t.Description
				if desc == "" {
					desc = fmt.Sprintf("%s %s", t.Method, t.Endpoint)
				}
				defs = append(defs, toolDef{
					Name:        t.Name,
					Description: desc,
					InputSchema: inputSchema,
				})
			}
			writeResp(response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  map[string]any{"tools": defs},
			})

		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				writeErr(req.ID, -32602, "invalid params")
				continue
			}
			tool, ok := s.toolIdx[p.Name]
			if !ok {
				writeErr(req.ID, -32602, fmt.Sprintf("unknown tool %q", p.Name))
				continue
			}
			if !s.limiters[tool.Name].Allow() {
				slog.Warn("rate limit exceeded", "tool", tool.Name)
				writeErr(req.ID, -32000, "rate limit exceeded")
				continue
			}

			result, execErr := s.exec.Execute(ctx, tool, p.Arguments)
			status := result.StatusCode
			if execErr != nil {
				slog.Error("tool execution failed", "tool", tool.Name, "err", execErr)
				if status == 0 {
					status = 500
				}
			}

			scrubbed, scrubActions := s.scrubber.Scrub(result.Body)

			logLimit := maxLoggedResponse
			if tool.Rules.MaxLogBytes != nil {
				logLimit = *tool.Rules.MaxLogBytes
			}
			loggedResponse := scrubbed
			if logLimit > 0 && len(loggedResponse) > logLimit {
				loggedResponse = loggedResponse[:logLimit] + "…"
			}

			paramsJSON, err := json.Marshal(p.Arguments)
			if err != nil {
				slog.Error("failed to marshal params for log", "tool", tool.Name, "err", err)
				paramsJSON = []byte(`{"error":"unable to serialize"}`)
			}
			scrubbedParams, _ := s.scrubber.Scrub(string(paramsJSON))
			_ = s.log.Record(activitylog.Entry{
				Tool:         tool.Name,
				Params:       scrubbedParams,
				Status:       status,
				Response:     loggedResponse,
				ScrubActions: scrubActions,
			})

			text := scrubbed
			isError := execErr != nil || result.StatusCode >= 400
			if execErr != nil {
				text = execErr.Error()
			}
			writeResp(response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": text},
					},
					"isError": isError,
				},
			})

		default:
			if req.ID != nil {
				writeErr(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading input: %w", err)
	}
	return nil
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

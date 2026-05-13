# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
task build      # Build binary to ./bin/tsk
task test       # go test ./...
task lint       # go vet ./...
```

Run a single test package:
```bash
go test ./internal/mcp/...
```

Run a single test:
```bash
go test -run TestName ./internal/mcp/...
```

Running `go build ./cmd/tsk` directly produces a binary with version `"dev"`. Use `task build` to embed the correct version via ldflags.

## Architecture

**tsk** is a local MCP server that sits between an AI agent and external APIs. It holds credentials, enforces per-tool rules, scrubs sensitive data from responses, and logs all activity locally.

```
LLM agent  →  tsk (rules, credentials, scrubbing)  →  External APIs
```

tsk communicates with agents over `stdio` using standard MCP (JSON-RPC 2.0). Credentials live in `~/.tsk/.secrets` and never leave the machine. Tool definitions and rules are configured in `~/.tsk/rules.yaml`.

**Package structure:**
- `cmd/tsk` — one-line main, calls `cli.NewRootCmd().Execute()`
- `internal/cli` — Cobra command constructors (`NewRootCmd`, `newInitCmd`, `newRunCmd`, `newLogsCmd`)
- `internal/mcp` — JSON-RPC 2.0 MCP server (`Server`, `Serve(ctx, r, w)`)
- `internal/config` — rules.yaml parser
- `internal/secrets` — `.secrets` file loader and `${KEY}` interpolation
- `internal/proxy` — HTTP tool execution with param filtering and constraint enforcement
- `internal/scrubber` — response scrubbing (built-in types + custom regex)
- `internal/ratelimit` — per-tool sliding-window rate limiter
- `internal/activitylog` — SQLite activity log
- `internal/version` — version string set via ldflags at build time

**Key runtime paths:**
- `~/.tsk/.secrets` — credential store (never read by the agent)
- `~/.tsk/rules.yaml` — tool definitions, rate limits, param constraints, scrubbing rules
- `~/.tsk/activity.db` — local SQLite log of every tool call

**rules.yaml structure:**
- `tools[]` — each entry defines an MCP tool: name, HTTP endpoint/method, credential reference (`${SECRET_NAME}`), rate limits, allowed params, param constraints
- `scrubbing[]` — built-in types (`credit_card`, `iban`, `email`, `ssn`) or custom regex patterns with replacement strings

**CLI commands:**
- `tsk init` — creates `~/.tsk/` with default secrets file and rules file
- `tsk run` — starts the MCP server (`--dir` overrides the tsk directory)
- `tsk logs` — queries the activity log (`--tail`, `--tool`, `--since`, `--dir` flags)

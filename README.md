# tsk

[![Go 1.26+](https://img.shields.io/badge/Go-1.26+-00ADD8.svg)](https://go.dev/)
[![CI](https://github.com/tesserakdev/tsk/actions/workflows/ci.yml/badge.svg)](https://github.com/tesserakdev/tsk/actions/workflows/ci.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

**tsk** is a local MCP server that sits between your AI agent and external APIs. It holds your credentials, enforces per-tool call rules, scrubs sensitive data from API responses before they reach the model's context, and keeps a queryable local log of everything your agent actually did.

Your agent never sees a raw API key. Your terminal never echoes a credit card number. Your debug log tells you exactly what happened.

## The Problem

The standard local agent setup looks like this:

```
LLM agent  -->  .env (STRIPE_KEY, AWS_SECRET)  -->  API
```

The agent has direct access to every credential in the file. A hallucination, a bad prompt, or a prompt injection can issue real API calls — refunds, deletions, writes — with no interception layer and no record of what happened.

tsk replaces that with:

```
LLM agent  -->  tsk (rules, credentials, scrubbing)  -->  API
```

## How It Works

1. tsk runs as a local MCP server over `stdio`.
2. Your credentials live in `~/.tsk/.secrets`, never in your project directory.
3. You define exactly what the agent is allowed to call in `~/.tsk/rules.yaml` — methods, endpoints, rate limits.
4. tsk dynamically exposes only those tools to the agent via MCP.
5. When the agent calls a tool, tsk injects the credential, executes the request, scrubs the response, and logs the exchange to a local SQLite database.

The agent receives only what you allow it to receive.

## Installation

```bash
curl -fsSL https://tesserak.dev/install.sh | sh
```

The installer verifies the SHA-256 checksum automatically. If [`gh`](https://cli.github.com) is
installed, it also verifies the SLSA build provenance attestation. To verify manually:

```bash
gh attestation verify tsk_linux_amd64.tar.gz --repo tesserakdev/tsk
```

SBOMs for each release artifact are attached to the [GitHub releases page](https://github.com/tesserakdev/tsk/releases/latest).

## Quick Start

Initialize your local tsk environment:

```bash
tsk init
```

This creates `~/.tsk/` with a secrets file and a default rules file.

### 1. Add credentials

```bash
# ~/.tsk/.secrets
STRIPE_TEST_KEY=sk_test_...
GITHUB_TOKEN=ghp_...
```

This file never leaves your machine. It is not read by the agent.

### 2. Define allowed tools

```yaml
# ~/.tsk/rules.yaml
version: 1
tools:
  - name: stripe_refund
    description: "Issue a refund for a Stripe charge. Use when the user asks to refund a payment."
    type: http
    endpoint: https://api.stripe.com/v1/refunds
    method: POST
    auth: bearer ${STRIPE_TEST_KEY}
    rules:
      max_calls_per_minute: 5

  - name: github_list_issues
    description: "List open issues for a GitHub repository. Returns issue titles, authors, and URLs."
    type: http
    endpoint: https://api.github.com/repos/{owner}/{repo}/issues
    method: GET
    auth: bearer ${GITHUB_TOKEN}
```

The agent can only call what is listed here. Everything else is blocked at the tsk layer.

### 3. Run

```bash
tsk run
```

tsk starts as a local MCP server. Your agent connects to it and discovers only the tools you defined.

### 4. Connect your agent

**Claude Desktop (`claude_desktop_config.json`):**

```json
{
  "mcpServers": {
    "tsk": {
      "command": "tsk",
      "args": ["run"]
    }
  }
}
```

**Claude Code:**

```bash
claude mcp add --transport stdio tsk -- tsk run
```

**Custom agent subprocess:**

```bash
tsk run
```

Any MCP-compatible agent works. tsk speaks standard MCP over `stdio`.

## Response Scrubbing

API responses often contain data you do not want in your model's context window: card numbers, IBANs, email addresses, internal IDs. Once that data is in context, it can appear in completions, get logged by your observability stack, or end up in a screenshot.

tsk intercepts every API response before it reaches the agent and redacts configured patterns.

```yaml
# ~/.tsk/rules.yaml
scrubbing:
  - type: credit_card
  - type: iban
  - type: email
  - pattern: '"internal_id":\s*"\w+"'
    replace: '"internal_id": "[REDACTED]"'
```

The agent receives the scrubbed response. The original is retained only in the local log, accessible to you, not to the model.

## Local Activity Log

Every tool request routed through tsk is written to a local SQLite database at `~/.tsk/activity.db`.

```bash
# Tail recent activity
tsk logs --tail 20

# Filter by tool
tsk logs --tool stripe_refund

# Filter by time range
tsk logs --since 1h

# Raw SQL access
sqlite3 ~/.tsk/activity.db "SELECT * FROM requests ORDER BY ts DESC LIMIT 10"
```

Each log entry records the tool name, request parameters (with credentials stripped), response status, scrubbing actions applied, and the scrubbed response body (truncated to 4 KB by default). Useful for debugging what your agent actually called vs. what you expected it to call.

You can disable truncation or set a tighter cap per tool:

```yaml
rules:
  max_log_bytes: 0     # retain full response body (no truncation)
  max_log_bytes: 8192  # explicit cap in bytes
  # omit to use the default 4 KB cap
```

## Agent Tool Preference

By default, agents with access to both tsk and native CLI tools (like `gh` or `stripe`) will often reach for the CLI first. The `instructions` field lets you inject guidance into the agent's system context via the MCP `initialize` handshake, before any tool call is made.

```yaml
# ~/.tsk/rules.yaml
instructions: |
  When a tsk tool is available for an operation, always use it instead of CLI
  alternatives (gh, stripe, curl, etc.). tsk enforces credential isolation,
  rate limits, and audit logging — bypassing it via CLI defeats those controls.
```

Pair this with directive language in each tool's `description` field:

```yaml
- name: github_list_prs
  description: >
    List open GitHub pull requests. Prefer this over the gh CLI — credentials
    are scoped and every call is audited. Returns PR titles, authors, and URLs.
```

## Configuration Reference

```yaml
version: 1

instructions: string             # Injected into the agent's system context on connect — use to steer tool preference

tools:
  - name: string                  # MCP tool name exposed to the agent
    description: string           # Human-readable description shown to the agent — tells it when and why to call this tool
    type: http
    endpoint: string              # Target URL, supports {path_params}
    method: GET | POST | PUT | PATCH | DELETE
    auth: bearer ${SECRET_NAME}   # Credential injected at call time
    rules:
      max_calls_per_minute: int   # Per-tool rate limit
      allowed_params:             # Restrict which request params the agent can set
        - amount
        - currency
      param_constraints:          # Hard limits on parameter values
        amount:
          max: 5000
      max_log_bytes: int          # Audit log body cap: 0 = no truncation, omit = default 4 KB

scrubbing:
  - type: credit_card | iban | email | ssn
  - pattern: string               # Custom regex
    replace: string               # Replacement string
```

## License

Apache License 2.0. See [LICENSE](LICENSE).

# Security Policy

## Reporting a Vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

Report them privately via [GitHub Security Advisories](https://github.com/tesserakdev/tsk/security/advisories/new) or by emailing **info@hirt.cz**.

Include:
- Description of the vulnerability and its potential impact
- Steps to reproduce or a proof-of-concept
- Affected version(s)

## Response Timeline

| Stage | Target |
|---|---|
| Initial acknowledgement | 2 business days |
| Triage and severity assessment | 5 business days |
| Fix or mitigation | Depends on severity |

We follow coordinated disclosure. We ask that you give us reasonable time to address the issue before any public disclosure.

## Supported Versions

Only the latest released version receives security fixes.

## Scope

tsk runs entirely on your local machine and never transmits credentials or activity logs externally. The primary attack surface is:

- **rules.yaml / .secrets parsing** — malformed or malicious config files
- **HTTP proxy layer** — SSRF, header injection, or constraint bypass
- **Response scrubbing** — regex bypass or incomplete redaction
- **Activity log** — local SQLite, path traversal or injection via tool names/params

Out of scope: vulnerabilities in third-party APIs that tsk proxies to.

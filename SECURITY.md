# Security Policy

## Reporting

Report suspected vulnerabilities privately through GitHub Security Advisories for
this repository. If GHSA is unavailable to you, email security@openclaw.ai.

Do not open public issues for vulnerabilities or include secrets, private
deployment details, database rows, tokens, or exploit details in public reports.

## Scope

In scope:

- ClickClack API, SQLite/Postgres access, generated SQL, and filesystem handling
- Cloudflare deployment configuration and container/runtime boundaries
- SDK and web client behavior that can cross an authentication, data, or tenant boundary
- command output, logs, or artifacts that could disclose private data or tokens
- dependency or workflow behavior that materially affects service integrity

Out of scope:

- upstream service outages or API changes outside ClickClack control
- compromise of a trusted local account, shell, filesystem, or deployment operator
- scanner-only findings without a reachable exploit path in supported usage

## Expectations

We prioritize reachable issues that affect service integrity, private data,
credentials, or safe execution. Include the affected version or commit, platform,
minimal reproduction steps, and sanitized impact details.

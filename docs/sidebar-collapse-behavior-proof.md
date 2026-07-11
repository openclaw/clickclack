# Collapsible sidebar behavior proof

## Environment

- Candidate branch: `codex/collapsible-sections-pr`
- ClickClack server: locally built production binary serving the existing `/tmp/clickclack-data` database on port `18082`
- OpenClaw: `2026.7.2`; gateway `/healthz` and `/readyz` passed after the ClickClack restart
- Connected accounts: blackbird, clipper, dragon-lady, mustang, and nighthawk all reported `enabled, configured, running`
- Recording: `Screen Recording 2026-07-11 at 16.12.48.mov`, captured against the running candidate

The live server binary and SQLite database were backed up before the candidate was started. No schema migration or data reset was required.

## Recorded behavior

The eight-second recording demonstrates the real sidebar with the existing ClickClack workspace, channels, connected status, and Blackbird person entry. It shows:

1. Channels, Direct messages, and People remain separate sidebar sections.
2. Each section heading is an interactive disclosure with a directional caret.
3. Collapsing a section removes only that section's rows.
4. Other sections remain visible and interactive.
5. Create-channel and start-DM controls remain available independently of disclosure state.
6. The controls and spacing remain usable in the narrow sidebar layout.

The recording is attached directly to the pull request so reviewers can inspect the interaction at native resolution.

## Automated behavior coverage

`tests/e2e/sidebar-collapse.spec.ts` provides deterministic coverage for behavior that is difficult to show in one short recording:

- all sections default expanded with `aria-expanded=true` and stable `aria-controls` targets;
- each section collapses independently;
- Channels and Direct messages expose aggregate unread counts while their rows are hidden, including the `99+` cap;
- add-channel and add-DM actions remain available while collapsed;
- state survives reloads and remains isolated per workspace;
- direct URL navigation does not override an explicit collapsed preference;
- malformed persisted state falls back to all-expanded;
- the same disclosure behavior works in the mobile navigation drawer.

## Reproduction

```sh
pnpm install --frozen-lockfile
pnpm build
go build -o /tmp/clickclack-sidebar ./apps/api/cmd/clickclack
/tmp/clickclack-sidebar serve --addr :18082 --data /tmp/clickclack-data --dev-bootstrap=true
```

Open `http://localhost:18082/app`, collapse any section, reload, and verify the selected workspace restores that section's state. Switch workspaces to verify the disclosure preferences are independent.

The feature is client-only. It does not change ClickClack APIs, storage schemas, message delivery, or the OpenClaw channel contract.

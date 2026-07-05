---
read_when:
  - changing how requests are authenticated
  - touching magic-link, GitHub OAuth, or session cookie code
  - adding a new auth provider or revising the bootstrap flow
---

# Auth

ClickClack accepts four ways to identify a caller, in order of precedence. The
resolver lives in `apps/api/internal/httpapi/server.go` (`currentActor`).

1. `Authorization: Bearer <token>` — bearer session token or `ccb_...` bot
   token. Bot tokens resolve to the bot user plus token workspace/scopes.
2. `cc_session` cookie — HTTP-only session cookie set by magic-link consume and
   GitHub OAuth callback.
3. `X-ClickClack-User: usr_...` header — explicit user impersonation for local
   development and tests, accepted only from loopback clients using local
   request hosts.
4. Dev fallback — the very first user in the database. Enabled only by
   `clickclack serve --dev-bootstrap=true` so a fresh checkout can boot into a
   working app without any token plumbing, and accepted only from local
   requests.

The dev fallback must stay off in any non-local deployment. `--dev-bootstrap=false`
is the default; require real sessions.

## Local owner bootstrap

`clickclack serve --dev-bootstrap=true` calls `Store.EnsureBootstrap`.
That helper:

- Returns the first user if one exists.
- Otherwise creates a `Local Captain` user, a `ClickClack` workspace, and a
  `general` channel, then returns the new user.

To pin the owner identity instead, run the CLI before serving:

```sh
clickclack admin bootstrap --name "Peter" --email steipete@gmail.com
```

That prints the new `usr_...` ID. Pass it back via `X-ClickClack-User` or use
the magic-link flow to mint a session.

## Magic links

Magic-link tokens are short-lived bearer credentials. In local dev mode the
HTTP request endpoint returns the token for convenience. With dev auth disabled,
the request endpoint is disabled until SMTP delivery exists; create tokens with
the admin CLI instead. The consume endpoint exchanges a token for a durable
session. In local dev mode, the HTTP request endpoint returns the token only
for loopback clients using local request hosts.

```http
POST /api/auth/magic/request
{ "email": "steipete@gmail.com", "display_name": "Peter" }

POST /api/auth/magic/consume
{ "token": "<token>" }
```

Magic-link consume requests must use `Content-Type: application/json`. Browser
requests with cross-site `Origin` or `Sec-Fetch-Site` headers are rejected so a
foreign site cannot force a browser session onto another account.

For production-style deployments, use the CLI delivery path until SMTP delivery
exists:

```sh
clickclack admin magic-link create --email steipete@gmail.com --name "Peter"
```

The client CLI can consume that token directly:

```sh
clickclack login --magic-token mgt_...
```

For remote human-operated clients, use the resulting bearer session token. For
hosted bots, prefer a scoped `ccb_...` bot token from `admin bot create`. The
CLI will not send a stored bearer token to a different `--server`, and it skips
stored bearer tokens when `--user` / `CLICKCLACK_USER_ID` is set without an
explicit `--token`.

`ConsumeMagicLink` returns `{user, session, token}` and sets `cc_session` as an
HTTP-only cookie. Browsers can drop the body; non-browser clients should hold
the `session.token` for the `Authorization` header. Session cookies default to
`Secure` outside local dev HTTP, even if a reverse proxy omits HTTPS headers.

## GitHub OAuth (optional)

GitHub OAuth is opt-in. Set the public URL, client ID, and client secret (or
the equivalent config keys) before serving:

```sh
CLICKCLACK_PUBLIC_URL=https://chat.example.com
CLICKCLACK_GITHUB_CLIENT_ID=...
CLICKCLACK_GITHUB_CLIENT_SECRET=...
# Optional org gate:
# CLICKCLACK_GITHUB_ALLOWED_ORG=openclaw
# Optional moderator org for open guest login:
# CLICKCLACK_GITHUB_MODERATOR_ORG=openclaw
```

Without a client ID and client secret, `GET /api/auth/github/start` returns `501`.

Flow:

1. `GET /api/auth/github/start` sets a state cookie and redirects to GitHub.
2. GitHub redirects back to `GET /api/auth/github/callback?code&state`.
3. The handler exchanges the code, fetches `/user` and primary `/user/emails`,
   checks org membership when `CLICKCLACK_GITHUB_ALLOWED_ORG` is set, checks
   moderator org membership when configured, upserts a user keyed by
   `(provider="github", provider_subject=<github id>)`, joins the org-gated
   default workspace or the open `Guests` workspace, creates a session, sets
   `cc_session`, redirects to `/`.

The desktop app uses the same GitHub callback through a system-browser handoff:

1. The app creates a high-entropy verifier and opens
   `GET /api/auth/github/desktop/start?code_challenge=<SHA-256 challenge>` in the
   default browser.
2. After the normal GitHub callback succeeds, the server redirects to
   `clickclack://auth/callback?code=<opaque one-time grant>`. No GitHub or
   ClickClack session token is placed in the URL.
3. The app posts the grant and its verifier to
   `POST /api/auth/github/desktop/consume`. The exact initiating server sets the
   Electron session cookie, invalidates the grant, and the app reloads `/app`.

Desktop state expires after ten minutes; completed grants expire after five.
The verifier binding prevents another local application from redeeming a custom
protocol callback it intercepts. Pending challenges live only in the initiating
browser's HTTP-only state cookies, so abandoned public starts do not consume
server-side state or block other users from beginning sign-in.

The redirect URL is derived from `CLICKCLACK_PUBLIC_URL` when set, otherwise
from the request scheme/host. Configure GitHub with `<public-url>/api/auth/github/callback`.

Without `CLICKCLACK_GITHUB_ALLOWED_ORG`, any GitHub account can sign in and is
automatically joined to an isolated `Guests` workspace. When
`CLICKCLACK_GITHUB_MODERATOR_ORG` is set, non-members of that org start as
waiting-room guests with a three-post daily budget until a moderator promotes
them to `member`; matching GitHub org members become moderators in the guest
workspace. If the moderator org is unset, open-login users join as normal
members so the workspace cannot become ownerless.

Open guest deployments with a moderator org, and org-gated deployments, request
`read:org`. GitHub only returns private org membership after the user grants
that scope, so team-only hosting should set `CLICKCLACK_GITHUB_ALLOWED_ORG`.

Guest restrictions and moderator controls are documented in
[moderation.md](moderation.md).

## Authorization

Every store mutation that touches a workspace runs `requireMembership` (or the
in-tx variant). API handlers do not duplicate that check — trust the store
layer for it. WebSocket subscriptions revalidate `GetWorkspace` before
upgrading.

Workspace roles are `owner`, `moderator`, `member`, `guest`, and `bot`. The
store enforces guest room visibility, guest post budgets, timeout/block state,
and moderator rank before writes. HTTP handlers still call store methods
rather than duplicating those checks.

Bot tokens add a second layer on top of membership: scope checks and a token
workspace check. See [bots.md](bots.md).

## Sessions

`sessions` are bearer tokens with an `expires_at`. `GetSessionUser` resolves the
token to a `User`. There is no refresh flow — issue a new session when one
expires.

## What is intentionally missing

- Email/password login.
- Password reset.
- SMTP delivery for magic links (V0 prints the token; V1 will add delivery).
- Per-channel ACLs and a historical moderation audit log.

---
read_when:
  - changing how requests are authenticated
  - touching magic-link, GitHub OAuth, or session cookie code
  - adding a new auth provider or revising the bootstrap flow
---

# Auth

ClickClack accepts five ways to identify a caller, in order of precedence. The
resolver lives in `apps/api/internal/httpapi/server.go` (`currentActor`).

1. `Authorization: Bearer <token>` — bearer session token or `ccb_...` bot
   token. Bot tokens resolve to the bot user plus token workspace/scopes.
2. Session cookie — `cc_session` by default, or the configured namespaced
   cookie. It is HTTP-only and set by magic-link consume, password login, and
   GitHub OAuth.
3. Cloudflare Access assertion — when trusted-proxy authentication is
   configured, a valid `Cf-Access-Jwt-Assertion` provisions or resolves the
   asserted email and creates a normal ClickClack session.
4. `X-ClickClack-User: usr_...` header — explicit user impersonation for local
   development and tests, accepted only from loopback clients using local
   request hosts.
5. Dev fallback — the very first user in the database. Enabled only by
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

`ConsumeMagicLink` returns `{user, session, token}` and sets the configured
HTTP-only session cookie. Browsers can drop the body; non-browser clients
should hold the `session.token` for the `Authorization` header. Session cookies
default to `Secure` outside local dev HTTP, even if a reverse proxy omits HTTPS
headers. Duplicate cookies with the active session-cookie name are rejected
instead of relying on cookie ordering.

## Trusted proxy (Cloudflare Access)

Team deployments may put ClickClack behind a Cloudflare Tunnel and Cloudflare
Access. Configure the Access team HTTPS origin and the application's audience
tag together:

```sh
CLICKCLACK_ACCESS_TEAM_DOMAIN=https://openclaw.cloudflareaccess.com
CLICKCLACK_ACCESS_AUD=<application-audience-tag>
```

The equivalent serve flags are `--access-team-domain` and `--access-aud`; the
JSON config keys are `access_team_domain` and `access_aud`. Setting only one
value fails startup validation. Leaving both unset disables this auth path, and
the Access assertion header is ignored.

For a request without a valid ClickClack session cookie, the server verifies
the `Cf-Access-Jwt-Assertion` as an RS256 JWT. It fetches keys only from
`<team-domain>/cdn-cgi/access/certs`, does not follow redirects, caches the JWKS
with bounded expiry, and refreshes an unknown key ID at most once every 30
seconds. The issuer must exactly match the configured team domain, the audience
must include the configured tag, expiration and issued-at claims must be valid,
and the email claim is required.

Cloudflare Access service tokens produce assertions without an email claim, so
this path rejects them. Automation must use ClickClack bot tokens instead.

After verification, ClickClack resolves or creates the human user by normalized
email and joins the default workspace. The first Access user becomes its owner;
later users join as members. The current request is authenticated immediately,
and the response sets the same configured HTTP-only session cookie used by
magic-link and GitHub OAuth sign-in. Later HTTP and WebSocket requests use that
cookie without revalidating Access on every request. Unsafe requests carrying
either an Access assertion or a ClickClack session cookie remain subject to the
existing same-origin and `X-ClickClack-CSRF` checks, including the first SSO
request before a local cookie exists.

This mode trusts Cloudflare Access as the deployment's identity boundary. Do
not expose the ClickClack origin directly around Access, and do not configure
these settings for a proxy that can pass client-supplied assertion headers.

## GitHub OAuth (optional)

GitHub OAuth is opt-in. Set the public URL, client ID, and client secret (or
the equivalent config keys) before serving:

```sh
CLICKCLACK_PUBLIC_URL=https://chat.example.com
# Optional only when trusted instances share one hostname:
# CLICKCLACK_COOKIE_NAMESPACE=production
CLICKCLACK_GITHUB_CLIENT_ID=...
CLICKCLACK_GITHUB_CLIENT_SECRET=...
# Optional org gate:
# CLICKCLACK_GITHUB_ALLOWED_ORG=openclaw
# Optional moderator org for open guest login:
# CLICKCLACK_GITHUB_MODERATOR_ORG=openclaw
```

Without a client ID and client secret, `GET /api/auth/github/start` returns `501`.

Flow:

1. `GET /api/auth/github/start` creates a database-backed, ten-minute OAuth
   transaction, sets an HTTP-only browser-binding cookie, and redirects to
   GitHub with a SHA-256 PKCE challenge.
2. GitHub redirects back to `GET /api/auth/github/callback?code&state`.
3. The handler atomically consumes the state only when the browser binding
   matches, exchanges the code with the stored PKCE verifier and exact redirect
   URI, fetches `/user` and primary `/user/emails`, checks configured org
   membership, and upserts the GitHub identity.
4. The handler joins the appropriate workspace, creates a session, sets the
   configured session cookie, and redirects to `/`.

GitHub accounts are matched only by provider and GitHub user ID, never linked
by email. Concurrent first sign-ins for the same identity resolve to one user
in both SQLite and PostgreSQL. Identity lookup, creation, and email/avatar
reconciliation share one transaction. SQLite uses its immediate write
transaction; PostgreSQL serializes the provider/subject pair before lookup and
locks an existing user while reconciling its profile.

Later sign-ins fill an empty identity email but keep an existing email and
user-edited display name. A provider avatar can replace a blank or email-derived
fallback avatar; explicit avatar overrides remain intact. Returned users retain
their notification settings.

The server stores hashes of state and browser-binding values. The short-lived
PKCE verifier is stored because the callback must send it to GitHub. State is
single-use, survives process restarts and multiple replicas, and permits up to
eight concurrent starts for one browser. Expired rows are removed during new
starts. Global pending-row limits bound abandoned or hostile starts.

The desktop app uses the same GitHub callback through a system-browser handoff:

1. The app creates a high-entropy verifier and opens
   `GET /api/auth/github/desktop/start?code_challenge=<SHA-256 challenge>&desktop_protocol=2`
   in the default browser.
2. After the normal GitHub callback succeeds, the server redirects to
   `chat.clickclack.desktop:/auth/callback?code=<opaque one-time grant>`. No
   GitHub or ClickClack session token is placed in the URL.
3. The app posts the grant and its verifier to
   `POST /api/auth/github/desktop/consume`. The exact initiating server
   atomically invalidates the grant, creates a session, and sets the configured
   cookie in Electron's persistent session.
4. The app calls `/api/me` through that same Electron session and loads `/app`
   only after the server confirms the authenticated user. The desktop client
   never depends on a particular cookie name.

Desktop transactions expire after ten minutes; completed grants expire after
five. Grants are persisted so the callback and redemption can hit different
replicas or a restarted process. The verifier binding prevents another local
application from redeeming a custom-protocol callback it intercepts. Grant
codes are stored only as hashes and are single-use. Protocol-1 callbacks carry
a 32-character lowercase hexadecimal grant; protocol-2 callbacks carry a
43-character unpadded base64url grant. The consume endpoint accepts exactly
those two formats during the compatibility window.

Protocol-1 desktop clients remain compatible with deployments using the default
cookie names and receive the legacy `clickclack://auth/callback` handoff.
Namespaced deployments return HTTP 426 before redirecting an old client to
GitHub, because that client cannot verify a namespaced session. Protocol-2
clients accept both callback formats so they can sign in to old and new
servers.

The redirect URL is always derived from `CLICKCLACK_PUBLIC_URL` in production.
Request-host derivation exists only for explicit loopback development. Configure
GitHub with `<public-url>/api/auth/github/callback`.

OAuth starts are public endpoints. The database enforces global and per-browser
pending-row bounds. A deployment may also rate-limit
`/api/auth/github/start`, `/api/auth/github/desktop/start`, and
`/api/auth/github/desktop/consume` at an edge that has a trustworthy client
identity. Do not derive security limits from untrusted forwarded IP headers or
add a naive application-level IP limiter. See the deployment documentation for
[hosted edge ownership](../deployment.md#hosted-deployment) and the
[optional self-hosted Nginx example](../deployment.md#optional-self-hosted-nginx-example),
capacity limits, monitoring, and sensitive logging requirements.

When metrics are enabled, `clickclack_github_oauth_events_total` exposes only a
fixed event category, including starts, state rejection, provider failure,
capacity rejection, desktop protocol mismatch, and successful handoff. It never
labels state, grants, users, cookies, callback parameters, or tokens.

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

## OpenClaw ID (optional)

OpenClaw ID is the first-party OIDC provider at `https://id.openclaw.ai`
(issuer `https://id.openclaw.ai/api/auth`). ClickClack is registered as a
confidential OAuth 2.1 client. Configure the server through the environment:

```sh
OPENCLAW_ID_CLIENT_ID=...
OPENCLAW_ID_CLIENT_SECRET=...
# Optional issuer override for staging or tests:
# OPENCLAW_ID_ISSUER=https://id.openclaw.ai/api/auth
```

Without a client ID and client secret, `GET /api/auth/openclaw/start` returns
`501`. Both credentials must be configured together, and the flow requires
`CLICKCLACK_PUBLIC_URL`.

Flow:

1. `GET /api/auth/openclaw/start` reuses the GitHub OAuth transaction store: a
   database-backed, ten-minute transaction, the same HTTP-only browser-binding
   cookie, and a SHA-256 PKCE challenge, then redirects to
   `<issuer>/oauth2/authorize` with `scope=openid profile email`.
2. OpenClaw ID redirects back to `GET /api/auth/openclaw/callback?code&state`.
3. The handler atomically consumes the state only when the browser binding
   matches, exchanges the code at `<issuer>/oauth2/token` using
   `client_secret_basic` plus the stored PKCE verifier, and reads the returned
   `id_token`. The token arrives directly from the issuer over TLS on an
   authenticated confidential-client exchange, so no local JWKS signature check
   runs; issuer, audience, expiry, and a verified email claim are enforced.
4. The handler links or creates the user by lowercased email exactly like the
   magic-link path (`GetOrCreateUserByEmail`), joins the default workspace,
   creates a normal session, sets the configured session cookie, and redirects
   to `/`.

Accounts without a verified email (`email_verified`) are rejected with `403`.
There is no desktop handoff for OpenClaw ID yet; the web login shows the
button only in the browser. Register the exact redirect URI
`<public-url>/api/auth/openclaw/callback` with the identity provider.

When metrics are enabled, `clickclack_openclaw_id_oauth_events_total` exposes
the same bounded event categories as the GitHub counter.

## Local passwords (optional)

Password login exists for fully offline or self-hosted deployments that cannot
reach GitHub and do not want to hand out magic-link tokens by hand. It is
opt-in and off by default:

```sh
CLICKCLACK_PASSWORD_AUTH_ENABLED=true
# or: clickclack serve --password-auth
# or: {"password_auth_enabled": true} in the config file
```

With the flag off, `POST /api/auth/password/login` returns `501` and the web
sign-in screen does not render the form.

There is no registration endpoint. An operator enables password sign-in for one
account at a time, and the command reads the secret from a prompt or piped
stdin so it never lands in a process listing or a shell history file:

```sh
clickclack admin user set-password --email maggie@example.com
printf '%s' "$PASSWORD" | clickclack admin user set-password --email maggie@example.com
clickclack admin user set-password --email maggie@example.com --clear
```

Setting a password on an account that has none enables password login for it.
`--clear` disables it again. `--user usr_...` selects an account by ID when an
email is ambiguous or absent.

The usual shape is a handover: the operator sets a temporary password, tells the
person once, and the person replaces it from the app with
`POST /api/auth/password/change` (below). The operator never learns the
replacement.

Flow and guarantees:

1. `POST /api/auth/password/login` takes `identifier` (an identity email or a
   handle, matched case-insensitively) and `password`.
2. It enforces the same `Origin`/`Sec-Fetch-Site` rejections as magic-link
   consume, then mints a session that is identical to the one every other
   method produces. The insert is conditional on the stored hash still being
   the one this request verified: argon2 verification is slow on purpose and
   runs outside the write, so a password change can commit in between, and a
   login that loses that race returns the same `401` as any other bad password
   instead of a live session for a replaced secret. A lost race does not spend
   the account's rate-limit budget.
3. Hashes are argon2id in PHC string format (`apps/api/internal/passwordauth`).
   Verification is constant time.
4. A wrong password, an unknown identifier, and an account with no password on
   file all return the same `401` body, and all three pay for one key
   derivation, so the endpoint does not disclose which accounts are enrolled.
5. Attempts are rate limited per client address and per identifier. The
   per-identifier budget is the narrow one and its window is long, which is
   what makes online guessing impractical.
6. Bot users are never reachable through this endpoint.

### Changing a password

`POST /api/auth/password/change` takes `current_password` and `new_password`
from an authenticated caller, session cookie or bearer session token alike:

```http
POST /api/auth/password/change
{ "current_password": "<temporary>", "new_password": "<theirs>" }
```

- It only ever replaces a password. An account with none on file gets `409` and
  stays unenrolled, so enabling password sign-in remains an operator decision.
- The current password is verified against the stored argon2id hash in constant
  time, and the new one has to satisfy the same length rules the admin command
  enforces.
- It carries the same `Content-Type`, `Origin`, and `Sec-Fetch-Site` rejections
  as password login, on top of the session CSRF header every unsafe cookie
  request already needs.
- Wrong current passwords are rate limited per account, five in fifteen minutes.
  Only failures spend the budget, so rotating a password repeatedly never locks
  the owner out.
- Bot tokens are rejected. Bots have no password.
- **A successful change revokes every other session for the account** and keeps
  the caller's own, so a lost or borrowed device is signed out by changing the
  password. The account owner stays signed in where they made the change. There
  was no prior house precedent for revoking sessions on a credential change
  (`admin user set-password` does not), so this endpoint sets it, matching the
  conventional safe default.
- **The replacement and the revocations commit together, or not at all**
  (`Store.ChangeUserPassword`). The transaction is conditional on the state the
  handler checked before it started: the stored hash must still be the snapshot
  the current-password check verified, and the caller's own session must still
  be live. Argon2 runs outside the transaction, which leaves a wide window, and
  without those conditions the loser of two concurrent changes would overwrite
  the winner's password and revoke the session the winner kept, while reporting
  success. A stale snapshot returns `409` and a revoked calling session returns
  `401`; both mean nothing was written, and the account is exactly as the
  winning change left it.
- The `501` behaviour matches login: with `CLICKCLACK_PASSWORD_AUTH_ENABLED`
  off, the endpoint is not available and the settings form does not render.

`GET /api/me` reports `password_enrolled` for the signed-in account. The web
settings modal renders its Change Password form only when that flag is true and
the runtime config advertises `password` among the enabled auth methods. The
flag describes the account, never the deployment, and is reported only to the
account itself, so it discloses nothing about who else is enrolled.

`POST /api/auth/logout` revokes the session the caller authenticated with,
bearer token or cookie, in the precedence `currentActor` uses, and expires the
cookie. Login returns a bearer-usable session token and the API accepts bearer
authentication, so a bearer-only caller signs out through the same endpoint. It
is idempotent, so a stale browser can always return to a signed-out state. A bot
token revokes nothing here: bot tokens are not sessions.

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

Revocation reaches connections that are already open, not just the next request.
A realtime WebSocket captures its actor at accept, so it revalidates that
session against the store before delivering, and on an idle timer for quiet
workspaces; a revoked session closes with `1008`. The check reads the database
rather than an in-process signal because a deployment runs replicas, and the
request that revokes a session is routinely in a different process from the
socket holding it. Connections that authenticated some other way, such as a bot
token or a trusted-proxy assertion, have no session to revalidate.

## What is intentionally missing

- Self-service registration, and password reset for someone who has forgotten
  theirs. An operator re-issues a temporary password with
  `clickclack admin user set-password`; changing a known password is
  self-service.
- SMTP delivery for magic links (V0 prints the token; V1 will add delivery).
- Per-channel ACLs and a historical moderation audit log.

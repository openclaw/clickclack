---
read_when:
  - embedding a ClickClack thread or channel in another application
  - configuring frame-ancestors for embedded pages
---

# Embedded threads and channels

ClickClack exposes a minimal thread-only page for docking a conversation in an
iframe. It contains the root message, replies, the normal thread composer, and
live updates, but none of the workspace rail, sidebar, or full-app top bar.

It also exposes a channel embed with the shared message renderer, paginated
history, nonce-idempotent composer, and realtime cursor recovery. Its slim
header names the channel and links back to the canonical full ClickClack view.
Authenticated users can edit their own thread roots, replies, and channel
messages inline; the embeds use the same draft, error, and realtime
reconciliation behavior as the full app.

## URL

Use the public route IDs from a normal ClickClack thread URL:

```text
https://chat.example.com/embed/thread/{workspace_route_id}/{message_route_id}
```

For a whole channel, use the workspace and channel public route IDs:

```text
https://chat.example.com/embed/channel/{workspace_route_id}/{channel_route_id}
```

For example:

```text
https://chat.example.com/embed/thread/T01KR3EXAMPLE1234/M01KR3EXAMPLE1234
```

The message route ID must identify a thread-root message. The viewer resolves
both public IDs through the normal route API, then uses the existing thread and
realtime endpoints. Humans authenticate with their normal ClickClack session
cookie. A signed-out frame offers a link that opens ClickClack in a new tab and
automatically retries when focus returns after sign-in.

The channel route ID must be a public `C...` channel route. The channel embed
uses the same membership and guest-channel visibility checks as the full app;
archiving a channel does not invalidate its embed URL.

## Allowing a host to frame ClickClack

Only `/embed/*` HTML responses receive a frame policy. With no configuration,
the response is restricted to:

```http
Content-Security-Policy: frame-ancestors 'self'
```

Add the exact HTTP(S) origins that may host the iframe with the environment
variable or serve flag:

```sh
CLICKCLACK_EMBED_FRAME_ANCESTORS=https://control.example.com,https://ops.example.com
```

```sh
clickclack serve \
  --embed-frame-ancestors https://control.example.com,https://ops.example.com
```

The JSON config-file equivalent is:

```json
{
  "embed_frame_ancestors": [
    "https://control.example.com",
    "https://ops.example.com"
  ]
}
```

ClickClack validates each value as an origin without credentials, a path,
query, or fragment. The resulting policy always retains `'self'`. Other SPA,
API, upload, and asset responses keep their existing frame behavior.

## Cookie and deployment topology

An iframe is authenticated only when the browser sends the ClickClack session
cookie in that embedding context. ClickClack's cookie SameSite mode follows the
configured `CLICKCLACK_PUBLIC_URL` and `CLICKCLACK_PUBLIC_API_URL`: same-host
deployments use `SameSite=Lax`, while different HTTPS frontend/API hostnames
use `SameSite=None; Secure`. Browser third-party-cookie controls can still
block cookies in an unrelated-site iframe even when the cookie uses
`SameSite=None`. Allowing an unrelated origin in `frame-ancestors` does not
change this cookie setting; an embed on an unrelated site will not authenticate
when the session cookie is `SameSite=Lax`.

The recommended deployment keeps ClickClack and the host application under the
same parent domain, such as `chat.example.com` and `control.example.com`. That
topology is same-site, avoids depending on third-party-cookie exceptions, and
still allows each service to keep its own origin. Do not loosen CORS, cookie,
or global frame policy for embedding; configure only the host origins that need
the `/embed/*` view.

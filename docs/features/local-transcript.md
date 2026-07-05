---
read_when:
  - configuring the local Claude Code transcript bridge
  - changing /talk or /api/cc/transcript
---

# Local transcript bridge

ClickClack can render recent text from a local Claude Code session at `/talk`.
The bridge is a developer-only inspection surface: disabled by default,
available only through loopback, and never exposed to bot tokens.

## Security boundary

All of these conditions must be true before ClickClack reads a transcript:

- The server process has `CLICKCLACK_CC_TRUTH_STATUS_PATH` set.
- The request is authenticated as a human session.
- The request's socket address, host, browser origin, and forwarding headers
  are local. Reverse-proxied and remote requests fail closed.
- The configured status command selects a transcript file.

The successful API response omits the transcript filename. It includes local
session status and working-directory metadata because `/talk` uses those fields
to explain which session was selected. Treat the page as a view into private
local development data; do not proxy it to another machine.

## Configure it

Set the environment variable to an absolute path for a Python status script,
then start ClickClack normally:

```sh
CLICKCLACK_CC_TRUTH_STATUS_PATH=/absolute/path/to/cc_truth_status.py \
  clickclack serve --addr 127.0.0.1:8080 --data ./data
```

The script is invoked as:

```sh
python3 /absolute/path/to/cc_truth_status.py --json
```

It must print a JSON array. Each row can contain:

```json
{
  "lane": "now",
  "truth_status": "active",
  "api_status": "busy",
  "session_id": "session-id",
  "cwd": "/absolute/project/path",
  "transcript": "/absolute/session.jsonl",
  "last_output_age_seconds": 2.4,
  "last_output_role": "assistant",
  "last_output_snippet": "Short operator-safe status text",
  "why": "Selected active session"
}
```

`lane`, `truth_status`, `session_id`, `cwd`, and `transcript` are the useful
minimum. Other status fields are optional.

Open `http://127.0.0.1:8080/talk`. With local dev auth enabled, ClickClack uses
the bootstrap human automatically. When dev auth is disabled, sign in normally
from the same loopback origin.

## Selection and parsing

`GET /api/cc/transcript` accepts:

- `cwd` — exact normalized working directory to prefer.
- `limit` — number of recent text messages, from 1 to 200; default 20.

Without `cwd`, selection prefers `truth_status=active`, then
`api_status=busy`, `truth_status=api_busy_unverified`, `truth_status=idle`, and
finally the first reported session.

The reader streams the JSONL file and keeps only recent user/assistant text.
Metadata, tool-use, attachment, and malformed rows are ignored. If the selected
file has no readable text, the API returns an honest empty message list.

## Failure behavior

| Status | Meaning |
| --- | --- |
| `401` | No valid human session. |
| `403` | Bot token or non-loopback request. |
| `503` | Bridge unset, status script failed, output was invalid, or transcript could not be read. |

An unset bridge does not fall back to a machine-specific path. It returns
`503` with `cc transcript bridge is not configured`.

## Coordination SDK

The transcript viewer and the SDK coordination helpers serve related but
separate roles. `/talk` is a local observation surface. `buildMessageEnvelope`
and `buildCoordinationHandoff` create transport-neutral JSON for routing a
ClickClack message elsewhere; they do not read transcripts or send data.

See [TypeScript SDK](../sdk.md#coordination-envelopes) for examples.

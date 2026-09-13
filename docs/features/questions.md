---
read_when:
  - changing bot-authored questions, answers, or their lifecycle
  - wiring an agent runtime's ask-user flow into ClickClack
---

# Questions

A bot can attach a structured question to one of its messages. The web app
renders it as a card in the conversation, people answer it there, and the bot
records what happened. The card then becomes a compact receipt that stays in
history.

Questions work on channel messages, direct messages, and thread replies. The
message body is still required and is the readable fallback for clients,
notifications, quotes, and search results that do not render questions.

## Asking

`POST /api/channels/{channel_id}/messages`, `POST /api/dms/{conversation_id}/messages`,
and `POST /api/messages/{message_id}/thread/replies` accept an optional
`question` from bot tokens on ordinary messages. Human sessions receive `403`,
and agent activity kinds (`agent_commentary`, `agent_tool`) receive `400`.

```jsonc
{
  "body": "Agent needs input: which day do we ship?",
  "nonce": "runtime-question-1",
  "question": {
    "external_id": "ask_2f6c…",
    "title": "Three details before the draft",
    "expires_at": "2026-09-12T15:57:00Z",
    "responder_user_ids": ["usr_…"],
    "allow_skip": true,
    "items": [
      {
        "id": "ship_date",
        "header": "Date",
        "prompt": "Which day do we ship?",
        "options": [{ "label": "Mon 15", "description": "AA 2231" }, { "label": "Tue 16" }],
        "allow_other": true
      },
      { "id": "boxes", "header": "Boxes", "prompt": "How many boxes?" },
      {
        "id": "extras",
        "header": "Extras",
        "prompt": "Anything else?",
        "multi_select": true,
        "options": [{ "label": "Sleeve" }, { "label": "UPC label" }]
      }
    ]
  }
}
```

| Field | Rule |
|---|---|
| `external_id` | Optional opaque identifier from the bot's runtime, at most 128 characters |
| `title` | Optional, at most 300 characters |
| `expires_at` | Required RFC 3339 time between 10 seconds and 7 days from now |
| `responder_user_ids` | Optional, at most 50 people who can read the conversation. Empty lets anyone in the conversation answer |
| `allow_skip` | Defaults to `true` |
| `items` | 1 to 5 questions |
| `items[].id` | Matches `^[a-z][a-z0-9_]{0,63}$` and is unique |
| `items[].header` | 1 to 24 characters |
| `items[].prompt` | 1 to 1000 characters, rendered as Markdown |
| `items[].url` | Optional `http(s)` link of at most 2048 characters, shown as an action that does not answer |
| `items[].options` | Up to 10 unique labels of at most 80 characters, with optional descriptions of at most 200 |
| `items[].multi_select` | Needs at least two options |
| `items[].allow_other` | Accepts one free-text answer besides the options. An item without options is free text |
| `items[].other_placeholder` | Optional hint for the free-text field, at most 60 characters |

Invalid questions return `400`. Create responses include the stored question
and the `X-ClickClack-Questions: supported` header. Servers that predate this
feature ignore the field, so clients detect support from the echoed
`message.question`. Listed responders are added to the message's resolved
mentions, so a mentions-only channel still alerts them. A nonce replay returns
the original message only when the question is identical, even after the
deadline has moved inside the minimum lifetime; the deadline and responders are
checked only when a question is first created.

## Reading

Message payloads include the question wherever messages are hydrated: channel
and DM pages, thread pages, single messages, pins, and create responses.

```jsonc
"question": {
  "status": "submitted",
  "external_id": "ask_2f6c…",
  "expires_at": "2026-09-12T15:57:00Z",
  "allow_skip": true,
  "items": [ /* as created */ ],
  "responder_user_ids": ["usr_…"],
  "response": {
    "answers": { "ship_date": ["Mon 15"], "boxes": ["120"], "extras": ["UPC label"] },
    "source": "clickclack",
    "responder": { "id": "usr_…", "display_name": "Angela" },
    "responded_at": "2026-09-12T15:44:10Z"
  },
  "version": 2
}
```

`status` is `open`, `submitted`, `answered`, `cancelled`, `expired`, or `failed`.
An open question past `expires_at` reads as `expired` before the bot records
it. `version` increases with every change.

## Answering

```http
POST /api/messages/{message_id}/question/answers
```

People send `{"answers": {...}, "nonce": "…", "expected_version": 1}` or
`{"skip": true, "nonce": "…", "expected_version": 1}`. Bot tokens receive `403`.

- The caller must be able to read the conversation and must not be timed out or
  blocked. Direct messages also require the ability to send there.
- When `responder_user_ids` is set, other people receive `403`.
- Every item needs at least one value. Single-choice items take exactly one.
  Values that match a declared label are stored as that label; other values
  are accepted once per item where free text is allowed.
- Skipping requires `allow_skip`.
- The first valid answer wins. Later answers, and answers to resolved or expired
  questions, return `409`. Replaying the same nonce as the same person returns
  the recorded answer without new events.
- `expected_version` is the version the person saw. When the question changed
  since then, for example because the bot reopened it after a lost response, the
  answer returns `409` instead of submitting it again. The web app always sends
  it.

A successful answer moves the question to `submitted`, returns
`{message, events}`, and appends two durable events: `message.updated`, so every
client refreshes the card, and `question.submitted`, whose payload carries
`message_id`, `root_message_id`, optional `direct_conversation_id`,
`external_id`, `responder_id`, `skipped`, and `version`. Neither event copies the
answers; the bot reads them from the message. Answers do not count against the
guest post budget.

## Resolving

```http
POST /api/messages/{message_id}/question/resolution
```

Only the bot that asked can resolve a question, using a token for that
workspace with `messages:write` (and `dms:write` in a direct message).

```json
{ "status": "answered", "note": "", "answers": {}, "expected_version": 2 }
```

| From | To | Meaning |
|---|---|---|
| `open` | `answered` | Answered outside ClickClack. Optional `answers` are validated and stored with `source: "external"` |
| `open` | `cancelled`, `expired`, `failed` | The bot stopped waiting |
| `submitted` | `answered`, `cancelled`, `failed` | The bot used the answer, applied a skip, or could not use it |
| `submitted` | `open` | Reopen after a rejected answer; requires `note` and clears the response |

`note` is at most 200 characters and appears on the card. A stale
`expected_version` or a transition the current status does not allow returns
`409`. Repeating the recorded terminal status returns `200` without an event;
other changes append `message.updated`.

## Reconciling

```http
GET /api/bots/self/questions?after=&limit=
```

Bot tokens with `messages:read` list their own `open` and `submitted` questions
in the token's workspace, ordered by message ID, with `{questions, next_cursor}`.
Questions in direct messages are included only when the token also has
`dms:read`. `limit` accepts 1 to 200 (default 100). A runtime uses it after a
restart to settle questions whose events it missed or whose waiting agent no
longer exists.

## Web app

The card replaces the message body in the timeline and in thread panels.

- Options are keycaps with number shortcuts. A question with one single-choice
  item and no free text answers on the first tap; other questions fill in and
  submit with **Send answers**.
- **Other...** opens an inline text field. `Enter` submits and `Escape` closes it.
- A countdown shows the time left and turns to the warning color in the last two
  minutes. At the deadline the card closes; the server stays authoritative.
- People outside `responder_user_ids` see disabled options and who can answer.
- After an answer the card shows `Sent · waiting for <bot>`, then a receipt with
  each header and answer. Skipped, cancelled, expired, and undelivered questions
  show their state and the bot's note.
- Drafts survive virtualized rows and panel switches for the same question
  version.

The TypeScript SDK exposes `client.questions.answer`, `client.questions.skip`,
`client.questions.resolve`, and `client.questions.listUnresolved`, and message
create inputs accept `question`.

---
read_when:
  - changing upload storage, attachment endpoints, or upload limits
  - moving uploads off local disk
---

# Uploads

Uploads are files keyed by an `upl_...` ID and attached to messages through a
join table. The default backend is local disk; Cloudflare R2 can store bytes
for ephemeral container deployments. The server streams uploads back to
authenticated workspace members and serves common safe preview types inline.

## Endpoints

```http
POST /api/uploads?workspace_id=...                 # multipart: file; form workspace_id also supported
GET  /api/uploads/{upload_id}                      # streams the file
POST /api/messages/{message_id}/attachments        # { upload_id }
```

- Upload size cap: `64 MiB` per request.
- The file is written to the configured upload backend under a random
  `upload-*` key. The original filename is recorded in the `uploads` row but
  not used as the storage key.
- `Content-Type` falls back to `application/octet-stream` when the client
  doesn't send one.
- Guests cannot upload or attach files while they are still in the waiting
  room. Timed-out and blocked users are also upload-restricted.

## Attaching to a message

`POST /api/messages/{message_id}/attachments` records a row in
`message_attachments`. The store hydrates attachments on
`ListMessages`/`GetThread`, so subsequent reads include the attachment list
without an extra round-trip.

The handler checks that the requester can read the upload, can access the
message workspace, and is the message author before linking. Bot tokens need
both `uploads:write` and `messages:write`.

The web client renders common previewable types in compact attachment cards:

- `image/*` inline, preserving recorded dimensions when available.
- `video/*` as inline native players with controls.
- `audio/*` as inline native audio controls.
- `application/pdf` as a first-page thumbnail card with the filename, size,
  and authenticated download link.
- `text/plain` as a lightweight text-file card.

Other content types appear as authenticated download cards that link to
`/api/uploads/{upload_id}`. The server sends `Content-Disposition: inline` only
for the safe preview set (`image`, `video`, `audio`, `text/plain`, and
`application/pdf`) and keeps `X-Content-Type-Options: nosniff` plus a sandbox
content-security policy on upload responses.

### Artifact viewer

Attached code, text, Markdown, PDF, DOCX, and HTML files open in a read-only
artifact pane without leaving the conversation. The pane replaces the thread
or profile pane on desktop and fills the viewport on mobile. Images retain the
existing lightbox; audio and video retain their inline controls.

Classification uses the upload's recorded filename and original content type,
not the response `Content-Type`. This lets the client recognize DOCX and HTML
while the server continues to serve those types as hardened downloads.

- Code and text render as escaped source. Markdown offers sanitized preview and
  source modes.
- PDFs render one page at a time with page and zoom controls.
- DOCX files are converted to semantic HTML in a terminable browser worker and
  sanitized. Before conversion, ClickClack rejects ZIP64 packages, archives
  with more than 2,048 entries, more than 32 MiB of declared expanded content,
  or a compression ratio above 100:1. Conversion stops after 10 seconds and
  converted HTML is capped at 4 MiB. The preview does not promise pixel-perfect
  Word layout.
- Uploaded HTML runs in an opaque-origin sandbox with a document CSP that
  blocks scripts, forms, navigation, and network requests.
- Text, code, Markdown, and HTML previews are limited to 2 MiB; DOCX to 16 MiB;
  PDF to the server's 64 MiB upload cap. Larger or malformed files fall back to
  an authenticated download.

Artifact viewing does not mutate upload bytes. Collaborative Markdown editing
requires a future first-class, revisioned artifact model rather than changing
an immutable message attachment in place.

## Storage layout

Local disk:

```
<data>/
  clickclack.db
  uploads/
    upload-XXXXXXXX
    ...
  logs/
```

Configure `<data>` with `--data` or `CLICKCLACK_DATA`. The server creates
`uploads/` on demand when the first request arrives.

R2:

```sh
CLICKCLACK_UPLOADS=r2://clickclack-uploads/prod
CLICKCLACK_R2_ACCOUNT_ID=...
CLICKCLACK_R2_ACCESS_KEY_ID=...
CLICKCLACK_R2_SECRET_ACCESS_KEY=...
```

R2 keys are stored in the database as `r2://bucket/prefix/upload-...`.
Download requests are still authenticated by ClickClack before the object is
fetched from R2.

## What is intentionally missing

- Server-side image thumbnailing/transcoding.
- Virus scanning.
- Per-workspace quotas.

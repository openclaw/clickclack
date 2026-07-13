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
POST /api/uploads?workspace_id=...&nonce=...       # multipart: file; nonce is optional
GET  /api/uploads/by-nonce?workspace_id=...&nonce=...
GET  /api/uploads/{upload_id}                      # streams the file
POST /api/messages/{message_id}/attachments        # { upload_id }
```

- Upload size cap: `64 MiB` per request.
- The file is written to the configured upload backend under a random
  `upload-*` key. The original filename is recorded in the `uploads` row but
  not used as the storage key.
- `Content-Type` falls back to `application/octet-stream` when the client
  doesn't send one.
- Clients that may retry an upload can send a nonce of up to 128 characters.
  The `workspace_id` query parameter is required when a nonce is present. The
  first request returns `201`; later requests from the same uploader return the
  original upload with `200` without reading or storing the multipart body.
  The nonce lookup endpoint returns the same metadata without downloading the
  file and includes `X-ClickClack-Upload-Nonce: supported`, including on a
  `404` lookup miss. Reusing the nonce for another workspace returns `409`.
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
- `application/pdf` as a document card with the filename, size, authenticated
  download link, and an explicit open action.
- `text/plain` as a lightweight text-file card.

Other content types appear as authenticated download cards that link to
`/api/uploads/{upload_id}`. The server sends `Content-Disposition: inline` only
for the safe preview set (`image`, `video`, `audio`, `text/plain`, and
`application/pdf`) and keeps `X-Content-Type-Options: nosniff` plus a sandbox
content-security policy on upload responses.

### Artifact viewer

Attached code, text, Markdown, PDF, Open XML spreadsheets and slide decks, and
HTML files open in a read-only artifact pane without leaving the conversation. DOCX stays download-only because ZIP
metadata cannot hard-bound decompression before a browser conversion library
allocates the expanded document. The pane temporarily covers the thread or
profile pane on desktop and fills the viewport on mobile; closing it restores
the underlying pane and route. Images retain the existing lightbox; audio and
video retain their inline controls.

Classification uses the upload's recorded filename and original content type,
not the response `Content-Type`. This lets the client recognize HTML while the
server continues to serve it as a hardened download.

- Code and text render as escaped source. Known code languages up to 256 KiB
  are highlighted in a terminable worker with a two-second timeout and a 2 MiB
  output cap; larger source remains escaped plain text. Markdown offers
  preview and source modes. Preview uses a positive allowlist of structural
  text, code, list, heading, quote, and table tags with no attributes; links,
  images, media, raw containers, and CSS remain visible in source mode only.
- PDFs load only after the user opens the document, render one page at a time,
  and provide page and zoom controls. Actual response bytes, load time, render
  time, embedded-image pixels, worker canvas bytes, each DPR-scaled backing
  dimension, and total backing pixels are capped. Files or pages outside those
  limits fall back to the authenticated download.
- XLSX-family workbooks render bounded raw cached cell values in a scrollable
  grid with worksheet tabs. Number, currency, and date formatting is not
  reconstructed, and hidden or non-worksheet sheets are omitted. Formulas use
  their cached values; macros are never executed.
- PPTX-family decks render a text outline with previous and next controls.
  Visuals, layout, animations, speaker notes, and hidden slides are omitted.
  The original remains available for download when visual fidelity matters.
- Office parsing runs in a terminable worker with a five-second timeout. ZIP
  paths and relationships are validated, document parts are streamed through
  bounded decompression and namespace-aware XML parsing, and limits apply per
  entry and across the whole document. The preview caps archive entries,
  expanded bytes, XML elements, cell text, total spreadsheet text, worksheets,
  cells, slides, paragraphs, and total slide text before rendering.
- DOCX files never enter a browser parser. Normal, malformed, compressed-bomb,
  and oversized DOCX uploads all use the same authenticated download-only path.
- Uploaded HTML is parsed only in an inert template; scripts, forms, frames,
  styles, and fetchable URLs are stripped before the safe fragment enters the
  keyboard-scrollable preview DOM. The original remains available in Source.
- Text, code, Markdown, and HTML previews are limited to 2 MiB; Office Open XML
  previews to 24 MiB compressed and 12 MiB expanded; PDF to the
  server's 64 MiB upload cap. The client checks streamed response bytes rather
  than trusting metadata alone. Larger or malformed files fall back to an
  authenticated download.

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

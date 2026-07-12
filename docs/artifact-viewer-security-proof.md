# Artifact viewer browser-security proof

The hostile-HTML browser fixture exercises the running ClickClack upload and viewer path with:

- a script that attempts to set a marker on the parent window;
- a form submission and nested frame;
- external navigation, image, stylesheet import, and CSS image URLs.

The Playwright test verifies in the live browser that inert-template sanitization leaves the script marker absent, no request reaches the sentinel host, scripts/forms/frames are absent from the preview DOM, and external URL-bearing attributes and CSS references are stripped before the fragment enters the rendered document.

The Markdown fixture separately covers link, image `src`, raw-HTML `srcset`,
media `poster`, inline CSS, stylesheet CSS, legacy `background`, and script
attempts. Its positive allowlist retains only structural Markdown tags with no
attributes, then observes zero sentinel-host requests.

Run the diagnostic capture from the repository root:

```sh
CAPTURE_ARTIFACT_PROOF=1 pnpm exec playwright test tests/e2e/artifact-viewer.spec.ts -g "opens safe code"
```

The capture is written to `docs/proof/artifact-viewer-html-isolation.png`. The diagnostic panel in the image is populated from browser-observed request counts, sanitized DOM state, and the parent-window script marker after the hostile artifact is opened.

## PDF canvas allocation limit

The oversized-page fixture is a small valid PDF with a 20,000 × 20,000-point
page. The running viewer rejects its DPR-scaled backing dimensions before
assigning either canvas axis, removes the canvas presentation, and shows the
authenticated download fallback.

An additional small-page fixture declares a 25-megapixel embedded raster. The
PDF.js document options reject image allocations above the same 16-megapixel
budget even when the final page canvas itself is small.

Run the PDF diagnostic capture from the repository root:

```sh
CAPTURE_PDF_LIMIT_PROOF=1 pnpm exec playwright test tests/e2e/artifact-viewer.spec.ts -g "shows local fallbacks"
```

The capture is written to `docs/proof/artifact-viewer-pdf-canvas-limit.png`.
Its diagnostic panel is populated after the browser observes the fallback,
confirms the download action is visible, and counts zero allocated viewer
canvases.

## DOCX parser exclusion

DOCX attachments are deliberately download-only. Browser tests upload both a
malformed package and an oversized package, verify that neither receives an
Open action, and verify that both retain authenticated download actions. This
keeps ZIP decompression and Word conversion outside ClickClack's browser trust
boundary.

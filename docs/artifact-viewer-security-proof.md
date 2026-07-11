# Artifact viewer browser-security proof

The hostile-HTML browser fixture exercises the running ClickClack upload and viewer path with:

- a script that attempts to set a marker on the parent window;
- a form submission and nested frame;
- external navigation, image, stylesheet import, and CSS image URLs.

The Playwright test verifies in the live browser that the iframe has an empty sandbox token list, the script marker remains absent, no request reaches the sentinel host, scripts/forms/frames are absent from the preview DOM, and external URL-bearing attributes and CSS references are stripped.

Run the diagnostic capture from the repository root:

```sh
CAPTURE_ARTIFACT_PROOF=1 pnpm exec playwright test tests/e2e/artifact-viewer.spec.ts -g "opens safe code"
```

The capture is written to [`docs/proof/artifact-viewer-html-isolation.png`](proof/artifact-viewer-html-isolation.png). The diagnostic panel in the image is populated from browser-observed request counts, DOM state, iframe attributes, and the parent-window script marker after the hostile artifact is opened.

## PDF canvas allocation limit

The oversized-page fixture is a small valid PDF with a 20,000 × 20,000-point
page. The running viewer rejects its DPR-scaled backing dimensions before
assigning either canvas axis, removes the canvas presentation, and shows the
authenticated download fallback.

Run the PDF diagnostic capture from the repository root:

```sh
CAPTURE_PDF_LIMIT_PROOF=1 pnpm exec playwright test tests/e2e/artifact-viewer.spec.ts -g "shows local fallbacks"
```

The capture is written to
[`docs/proof/artifact-viewer-pdf-canvas-limit.png`](proof/artifact-viewer-pdf-canvas-limit.png).
Its diagnostic panel is populated after the browser observes the fallback,
confirms the download action is visible, and counts zero allocated viewer
canvases.

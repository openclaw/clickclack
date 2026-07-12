<script lang="ts">
  import DOMPurify from "dompurify";
  import { onDestroy } from "svelte";
  import type {
    PDFDocumentLoadingTask,
    PDFDocumentProxy,
    PDFPageProxy,
    PDFWorker,
    RenderTask,
    TextContent,
  } from "pdfjs-dist";
  import {
    artifactKindLabel,
    artifactLanguage,
    artifactPreviewLimit,
    classifyArtifact,
    type ArtifactKind,
  } from "../../lib/artifacts";
  import { markdown } from "../../lib/format";
  import { highlightCodeInWorker } from "../../lib/highlight";
  import {
    parsePresentation,
    parseSpreadsheet,
    type PresentationPreview,
    type SpreadsheetPreview,
  } from "../../lib/office";
  import {
    assertSafePDFCanvas,
    PDF_CANVAS_PIXEL_LIMIT,
    PDF_LOAD_TIMEOUT_MS,
    PDF_RENDER_TIMEOUT_MS,
  } from "../../lib/pdf";
  import type { Upload } from "../../lib/types";
  import { formatBytes, uploadURL } from "../../lib/uploads";
  import pdfWorkerURL from "pdfjs-dist/build/pdf.worker.mjs?url";

  type Props = {
    upload: Upload;
    onClose: () => void;
  };

  let { upload, onClose }: Props = $props();

  let canvasEl: HTMLCanvasElement | null = $state(null);
  let kind: ArtifactKind = $derived(classifyArtifact(upload));
  let mode: "preview" | "source" = $state("preview");
  let status: "idle" | "loading" | "ready" | "error" = $state("idle");
  let errorMessage = $state("");
  let source = $state("");
  let renderedHTML = $state("");
  let structuredPreviewAvailable = $state(false);
  let highlightedSource = $state("");
  let pdfDocument: PDFDocumentProxy | null = $state(null);
  let pdfPage = $state(1);
  let pdfScale = $state(1);
  let pdfRendering = $state(false);
  let pdfText = $state("");
  let cleanupPDF: (() => void) | null = null;
  let pdfImageLimitExceeded = false;
  let spreadsheet: SpreadsheetPreview | null = $state(null);
  let activeSheet = $state(0);
  let presentation: PresentationPreview | null = $state(null);
  let activeSlide = $state(0);

  const STRUCTURED_TOKEN_LIMIT = 10_000;
  const STRUCTURED_SOURCE_LIMIT = 64 * 1024;
  const RENDERED_HTML_LIMIT = 4 * 1024 * 1024;
  const PDF_TEXT_CHARACTER_LIMIT = 64 * 1024;
  const PDF_TEXT_ITEM_LIMIT = 5_000;

  class StructuredPreviewLimitError extends Error {}

  let label = $derived(artifactKindLabel(kind));
  let url = $derived(uploadURL(upload));
  let canToggleSource = $derived(
    (kind === "markdown" || kind === "html") &&
      status === "ready" &&
      structuredPreviewAvailable,
  );

  function previewTooLargeMessage(limit: number): string {
    return `This ${label.toLowerCase()} is ${formatBytes(upload.byte_size)}. Preview is limited to ${formatBytes(limit)}.`;
  }

  function htmlDocument(body: string): string {
    assertStructuredComplexity(body);
    const template = document.createElement("template");
    template.innerHTML = body;
    const sanitizedContent = DOMPurify.sanitize(template.content, {
      RETURN_DOM_FRAGMENT: true,
      USE_PROFILES: { html: true },
      FORBID_TAGS: [
        "base",
        "embed",
        "form",
        "iframe",
        "link",
        "meta",
        "object",
        "script",
        "style",
      ],
      FORBID_ATTR: ["action", "formaction", "srcset", "style", "xlink:href"],
    });
    for (const element of sanitizedContent.querySelectorAll<HTMLElement>("*")) {
      for (const attribute of Array.from(element.attributes)) {
        element.removeAttribute(attribute.name);
      }
    }
    const container = document.createElement("div");
    container.append(sanitizedContent);
    const safeBody = container.innerHTML;
    assertRenderedComplexity(safeBody);
    return `<!doctype html>${safeBody}`;
  }

  function markdownDocument(body: string): string {
    assertStructuredComplexity(body);
    const rendered = markdown(body);
    assertRenderedComplexity(rendered);
    return DOMPurify.sanitize(rendered, {
      ALLOWED_TAGS: [
        "blockquote",
        "br",
        "code",
        "del",
        "em",
        "h1",
        "h2",
        "h3",
        "h4",
        "h5",
        "h6",
        "hr",
        "li",
        "ol",
        "p",
        "pre",
        "strong",
        "table",
        "tbody",
        "td",
        "th",
        "thead",
        "tr",
        "ul",
      ],
      ALLOWED_ATTR: [],
    });
  }

  function assertStructuredComplexity(value: string) {
    if (value.length > STRUCTURED_SOURCE_LIMIT) {
      throw new StructuredPreviewLimitError("Structured preview exceeded the safe source limit.");
    }
    let tokens = 0;
    for (const character of value) {
      if ("<[]>*_`-+.!#>|~".includes(character)) {
        tokens += 1;
        if (tokens > STRUCTURED_TOKEN_LIMIT) {
          throw new StructuredPreviewLimitError("Structured preview exceeded the safe complexity limit.");
        }
      }
    }
  }

  function assertRenderedComplexity(value: string) {
    if (value.length > RENDERED_HTML_LIMIT) {
      throw new StructuredPreviewLimitError("Rendered preview exceeded the safe output limit.");
    }
    let elements = 0;
    for (const character of value) {
      if (character === "<" && ++elements > STRUCTURED_TOKEN_LIMIT * 2) {
        throw new StructuredPreviewLimitError("Rendered preview exceeded the safe element limit.");
      }
    }
  }

  function resourceLimitMessage(limit: number): string {
    return `Preview data exceeded the ${formatBytes(limit)} safety limit.`;
  }

  async function responseBytes(signal: AbortSignal, limit: number): Promise<Uint8Array> {
    const response = await fetch(url, { credentials: "same-origin", signal });
    if (!response.ok) {
      if (response.status === 401 || response.status === 403) {
        throw new Error("You no longer have access to this artifact.");
      }
      if (response.status === 404) throw new Error("This artifact is no longer available.");
      throw new Error(`Could not load this artifact (${response.status}).`);
    }
    const declaredLength = Number(response.headers.get("Content-Length"));
    if (Number.isFinite(declaredLength) && declaredLength > limit) {
      throw new Error(resourceLimitMessage(limit));
    }
    if (!response.body) {
      const bytes = new Uint8Array(await response.arrayBuffer());
      if (bytes.byteLength > limit) throw new Error(resourceLimitMessage(limit));
      return bytes;
    }

    const reader = response.body.getReader();
    const chunks: Uint8Array[] = [];
    let total = 0;
    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        total += value.byteLength;
        if (total > limit) {
          await reader.cancel();
          throw new Error(resourceLimitMessage(limit));
        }
        chunks.push(value);
      }
    } finally {
      reader.releaseLock();
    }
    const bytes = new Uint8Array(total);
    let offset = 0;
    for (const chunk of chunks) {
      bytes.set(chunk, offset);
      offset += chunk.byteLength;
    }
    return bytes;
  }

  async function loadText(signal: AbortSignal, limit: number): Promise<string> {
    return new TextDecoder("utf-8", { fatal: false }).decode(await responseBytes(signal, limit));
  }

  async function loadArtifact(signal: AbortSignal) {
    cleanupPDF?.();
    cleanupPDF = null;
    pdfDocument = null;
    source = "";
    renderedHTML = "";
    structuredPreviewAvailable = false;
    highlightedSource = "";
    pdfPage = 1;
    pdfScale = 1;
    pdfText = "";
    pdfImageLimitExceeded = false;
    spreadsheet = null;
    activeSheet = 0;
    presentation = null;
    activeSlide = 0;
    mode = "preview";
    errorMessage = "";

    const limit = artifactPreviewLimit(kind);
    if (limit !== undefined && upload.byte_size > limit) {
      status = "error";
      errorMessage = previewTooLargeMessage(limit);
      return;
    }
    if (kind === "unsupported") {
      status = "ready";
      return;
    }

    status = "loading";
    try {
      if (kind === "pdf") {
        const pdfjs = await import("pdfjs-dist");
        if (signal.aborted) return;
        pdfjs.GlobalWorkerOptions.workerSrc = pdfWorkerURL;
        const pdfController = new AbortController();
        const abortPDF = () => pdfController.abort();
        signal.addEventListener("abort", abortPDF, { once: true });
        let pdfWorker: PDFWorker | null = null;
        let loadTimer = 0;
        const loadTimeout = new Promise<never>((_, reject) => {
          loadTimer = window.setTimeout(() => {
            pdfController.abort();
            pdfWorker?.destroy();
            reject(new Error("PDF preview took too long and was stopped."));
          }, PDF_LOAD_TIMEOUT_MS);
        });
        let bytes: Uint8Array;
        try {
          bytes = await Promise.race([responseBytes(pdfController.signal, limit!), loadTimeout]);
          if (signal.aborted) throw new DOMException("Aborted", "AbortError");
          pdfWorker = new pdfjs.PDFWorker();
          await Promise.race([pdfWorker.promise, loadTimeout]);
        } catch (error) {
          clearTimeout(loadTimer);
          signal.removeEventListener("abort", abortPDF);
          throw error;
        }
        if (signal.aborted || !pdfWorker) {
          clearTimeout(loadTimer);
          signal.removeEventListener("abort", abortPDF);
          pdfWorker?.destroy();
          return;
        }
        const workerPort = pdfWorker.port;
        const onWorkerMessage = (event: MessageEvent<unknown>) => {
          const message = event.data;
          if (
            typeof message === "object" &&
            message !== null &&
            "reason" in message &&
            typeof message.reason === "object" &&
            message.reason !== null &&
            "message" in message.reason &&
            message.reason.message === "Image exceeded maximum allowed size and was removed."
          ) {
            pdfImageLimitExceeded = true;
            cleanupPDF?.();
            status = "error";
            errorMessage =
              "PDF page content could not be rendered completely within safety limits.";
          }
        };
        workerPort.addEventListener("message", onWorkerMessage);
        let loadingTask: PDFDocumentLoadingTask | null = null;
        let pdfCleaned = false;
        cleanupPDF = () => {
          if (pdfCleaned) return;
          pdfCleaned = true;
          workerPort.removeEventListener("message", onWorkerMessage);
          if (loadingTask) void loadingTask.destroy();
          pdfWorker.destroy();
          pdfDocument = null;
        };
        const task = pdfjs.getDocument({
          data: bytes,
          maxImageSize: PDF_CANVAS_PIXEL_LIMIT,
          canvasMaxAreaInBytes: PDF_CANVAS_PIXEL_LIMIT * 4,
          stopAtErrors: true,
          worker: pdfWorker,
        }) as PDFDocumentLoadingTask;
        loadingTask = task;
        try {
          pdfDocument = await Promise.race([task.promise, loadTimeout]);
        } finally {
          clearTimeout(loadTimer);
          signal.removeEventListener("abort", abortPDF);
        }
      } else if (kind === "spreadsheet" || kind === "presentation") {
        const bytes = await responseBytes(signal, limit!);
        if (signal.aborted) return;
        if (kind === "spreadsheet") {
          const preview = await parseSpreadsheet(bytes, signal);
          if (signal.aborted) return;
          spreadsheet = preview;
        } else {
          const preview = await parsePresentation(bytes, signal);
          if (signal.aborted) return;
          presentation = preview;
        }
      } else {
        source = await loadText(signal, limit!);
        if (signal.aborted) return;
        if (kind === "code") {
          highlightedSource = await highlightCodeInWorker(source, artifactLanguage(upload), signal);
        }
        try {
          if (kind === "markdown") {
            renderedHTML = markdownDocument(source);
            structuredPreviewAvailable = true;
          }
          if (kind === "html") {
            renderedHTML = htmlDocument(source);
            structuredPreviewAvailable = true;
          }
        } catch (error) {
          if (!(error instanceof StructuredPreviewLimitError)) throw error;
          renderedHTML = "";
          mode = "source";
        }
      }
      if (!signal.aborted) status = "ready";
    } catch (error) {
      if (signal.aborted || (error instanceof Error && error.name === "AbortError")) return;
      cleanupPDF?.();
      cleanupPDF = null;
      status = "error";
      errorMessage = error instanceof Error ? error.message : "Could not preview this artifact.";
    }
  }

  $effect(() => {
    const controller = new AbortController();
    void loadArtifact(controller.signal);
    return () => controller.abort();
  });

  $effect(() => {
    if (kind !== "pdf" || !pdfDocument || !canvasEl || status !== "ready") return;
    const document = pdfDocument;
    const pageNumber = pdfPage;
    const scale = pdfScale;
    let cancelled = false;
    let renderTask: RenderTask | null = null;
    let page: PDFPageProxy | null = null;
    let textReader: ReadableStreamDefaultReader<TextContent> | null = null;
    let renderTimer = 0;
    pdfRendering = true;
    pdfText = "";

    const render = async () => {
      try {
        renderTimer = window.setTimeout(() => {
          if (cancelled) return;
          renderTask?.cancel();
          void textReader?.cancel();
          cleanupPDF?.();
          status = "error";
          errorMessage = "PDF page rendering took too long and was stopped.";
        }, PDF_RENDER_TIMEOUT_MS);
        page = await document.getPage(pageNumber);
        if (cancelled || !canvasEl) return;
        let extractedText = "";
        let textItems = 0;
        textReader = page.streamTextContent().getReader();
        try {
          while (!cancelled) {
            const { done, value } = await textReader.read();
            if (done) break;
            for (const item of value.items) {
              textItems += 1;
              if (textItems > PDF_TEXT_ITEM_LIMIT) {
                await textReader.cancel();
                extractedText = `${extractedText}…`;
                break;
              }
              if (!("str" in item) || !item.str) continue;
              const next = `${extractedText}${extractedText ? " " : ""}${item.str}`;
              if (next.length > PDF_TEXT_CHARACTER_LIMIT) {
                await textReader.cancel();
                extractedText = `${next.slice(0, PDF_TEXT_CHARACTER_LIMIT)}…`;
                break;
              }
              extractedText = next;
            }
            if (textItems > PDF_TEXT_ITEM_LIMIT || extractedText.endsWith("…")) break;
          }
        } finally {
          textReader.releaseLock();
          textReader = null;
        }
        if (cancelled) return;
        pdfText = extractedText || "This page has no extractable text.";
        const viewport = page.getViewport({ scale });
        const dpr = Math.min(window.devicePixelRatio || 1, 2);
        const backingWidth = Math.max(1, Math.floor(viewport.width * dpr));
        const backingHeight = Math.max(1, Math.floor(viewport.height * dpr));
        assertSafePDFCanvas(backingWidth, backingHeight);
        const context = canvasEl.getContext("2d");
        if (!context) throw new Error("PDF canvas is unavailable.");
        canvasEl.width = backingWidth;
        canvasEl.height = backingHeight;
        canvasEl.style.width = `${viewport.width}px`;
        canvasEl.style.height = `${viewport.height}px`;
        context.setTransform(dpr, 0, 0, dpr, 0, 0);
        renderTask = page.render({ canvasContext: context, viewport });
        await renderTask.promise;
        if (pdfImageLimitExceeded) {
          throw new Error("PDF page content could not be rendered completely within safety limits.");
        }
      } catch (error) {
        if (!cancelled && !(error instanceof Error && error.name === "RenderingCancelledException")) {
          cleanupPDF?.();
          status = "error";
          errorMessage =
            error instanceof Error ? error.message : "Could not render this PDF page.";
        }
      } finally {
        clearTimeout(renderTimer);
        page?.cleanup();
        page = null;
        if (!cancelled) pdfRendering = false;
      }
    };

    void render();
    return () => {
      cancelled = true;
      clearTimeout(renderTimer);
      renderTask?.cancel();
      void textReader?.cancel();
    };
  });

  onDestroy(() => cleanupPDF?.());

  function columnNumber(reference: string): number {
    const letters = reference.match(/^[A-Z]+/i)?.[0]?.toUpperCase() || "A";
    let value = 0;
    for (const letter of letters) value = value * 26 + letter.charCodeAt(0) - 64;
    return value;
  }

  function rowNumber(reference: string): number {
    return Number(reference.match(/\d+$/)?.[0] || 1);
  }

  function columnName(value: number): string {
    let name = "";
    for (let current = value; current > 0; current = Math.floor((current - 1) / 26)) {
      name = String.fromCharCode(65 + ((current - 1) % 26)) + name;
    }
    return name;
  }

  let spreadsheetGrid = $derived.by(() => {
    const sheet = spreadsheet?.sheets[activeSheet];
    if (!sheet)
      return {
        columns: [] as string[],
        rows: [] as { number: number; values: string[] }[],
        clipped: false,
      };
    const actualMaxColumn = Math.max(
      1,
      ...sheet.cells.map((cell) => columnNumber(cell.reference)),
    );
    const actualMaxRow = Math.max(1, ...sheet.cells.map((cell) => rowNumber(cell.reference)));
    const maxColumn = Math.min(100, actualMaxColumn);
    const maxRow = Math.min(1_000, Math.floor(10_000 / maxColumn), actualMaxRow);
    const values = new Map(sheet.cells.map((cell) => [cell.reference.toUpperCase(), cell.value]));
    const columns = Array.from({ length: maxColumn }, (_, index) => columnName(index + 1));
    const rows = Array.from({ length: maxRow }, (_, index) => ({
      number: index + 1,
      values: columns.map((column) => values.get(`${column}${index + 1}`) || ""),
    }));
    return {
      columns,
      rows,
      clipped: actualMaxColumn > maxColumn || actualMaxRow > maxRow,
    };
  });
</script>

<header class="artifact-viewer__header">
  <div class="artifact-viewer__identity">
    <span>{label}</span>
    <strong title={upload.filename}>{upload.filename}</strong>
    <small>{formatBytes(upload.byte_size)}</small>
  </div>
  <div class="artifact-viewer__actions">
    {#if canToggleSource}
      <div class="artifact-viewer__segmented" aria-label={`${label} view`}>
        <button type="button" class:active={mode === "preview"} aria-pressed={mode === "preview"} onclick={() => (mode = "preview")}>Preview</button>
        <button type="button" class:active={mode === "source"} aria-pressed={mode === "source"} onclick={() => (mode = "source")}>Source</button>
      </div>
    {/if}
    <a href={url} download={upload.filename} aria-label={`Download ${upload.filename}`} title="Download">
      <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true"><path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M12 4v12m0 0 4-4m-4 4-4-4M5 20h14" /></svg>
    </a>
    <button type="button" aria-label="Close artifact viewer" title="Close" onclick={onClose}>×</button>
  </div>
</header>

<!-- svelte-ignore a11y_no_noninteractive_tabindex (scrollable dialog content must be keyboard-focusable) -->
<div
  class="artifact-viewer__body"
  class:is-pdf={kind === "pdf"}
  class:is-office={kind === "spreadsheet" || kind === "presentation"}
  role="region"
  tabindex="0"
  aria-label="Artifact content"
>
  {#if status === "loading"}
    <div class="artifact-viewer__state" role="status">
      <span class="artifact-viewer__spinner" aria-hidden="true"></span>
      <strong>Opening {label.toLowerCase()}</strong>
      <p>Preparing a safe preview.</p>
    </div>
  {:else if status === "error"}
    <div class="artifact-viewer__state artifact-viewer__state--error" role="alert">
      <svg viewBox="0 0 24 24" width="28" height="28" aria-hidden="true"><path d="M12 3 2.8 20h18.4L12 3Z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M12 9v5m0 3h.01" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg>
      <strong>Preview unavailable</strong>
      <p>{errorMessage}</p>
      <a href={url} download={upload.filename}>Download original</a>
    </div>
  {:else if kind === "unsupported"}
    <div class="artifact-viewer__state">
      <svg viewBox="0 0 24 24" width="32" height="32" aria-hidden="true"><path d="M6 3h8l4 4v14H6V3Z" fill="none" stroke="currentColor" stroke-width="1.6"/><path d="M14 3v5h5" fill="none" stroke="currentColor" stroke-width="1.6"/></svg>
      <strong>No preview for this file type</strong>
      <p>You can still download the original file.</p>
      <a href={url} download={upload.filename}>Download original</a>
    </div>
  {:else if kind === "pdf" && pdfDocument}
    <div class="artifact-viewer__pdf-toolbar" aria-label="PDF controls">
      <button type="button" disabled={pdfPage <= 1 || pdfRendering} onclick={() => (pdfPage -= 1)}>Previous</button>
      <span>Page {pdfPage} of {pdfDocument.numPages}</span>
      <button type="button" disabled={pdfPage >= pdfDocument.numPages || pdfRendering} onclick={() => (pdfPage += 1)}>Next</button>
      <span class="artifact-viewer__toolbar-divider" aria-hidden="true"></span>
      <button type="button" aria-label="Zoom out" disabled={pdfScale <= 0.6 || pdfRendering} onclick={() => (pdfScale = Math.max(0.6, pdfScale - 0.2))}>−</button>
      <span>{Math.round(pdfScale * 100)}%</span>
      <button type="button" aria-label="Zoom in" disabled={pdfScale >= 2 || pdfRendering} onclick={() => (pdfScale = Math.min(2, pdfScale + 0.2))}>+</button>
    </div>
    <!-- svelte-ignore a11y_no_noninteractive_tabindex (zoomed PDF stage must be keyboard-scrollable) -->
    <div
      class="artifact-viewer__pdf-stage"
      class:is-rendering={pdfRendering}
      role="region"
      tabindex="0"
      aria-label={`PDF page ${pdfPage} visual preview`}
    >
      <canvas bind:this={canvasEl} aria-hidden="true"></canvas>
      <section class="artifact-viewer__pdf-text" aria-label={`PDF page ${pdfPage} text`}>
        {pdfText}
      </section>
    </div>
  {:else if kind === "spreadsheet" && spreadsheet}
    <div class="artifact-viewer__office-toolbar" aria-label="Workbook controls">
      <span>{spreadsheet.sheets.length} {spreadsheet.sheets.length === 1 ? "sheet" : "sheets"}</span>
    </div>
    <div class="artifact-viewer__spreadsheet" role="region" aria-label={`${spreadsheet.sheets[activeSheet].name} worksheet`} tabindex="0">
      <table>
        <thead><tr><th class="row-heading" aria-label="Row numbers"></th>{#each spreadsheetGrid.columns as column}<th scope="col">{column}</th>{/each}</tr></thead>
        <tbody>{#each spreadsheetGrid.rows as row}<tr><th class="row-heading" scope="row">{row.number}</th>{#each row.values as value}<td>{value}</td>{/each}</tr>{/each}</tbody>
      </table>
      {#if spreadsheetGrid.clipped || spreadsheet.sheets[activeSheet].truncated}
        <p class="artifact-viewer__grid-note">Preview is limited to 10,000 cells, the first 1,000 rows, and the first 100 columns. Download the original to inspect omitted cells.</p>
      {/if}
    </div>
    <div class="artifact-viewer__sheet-tabs" role="tablist" aria-label="Worksheets">
      {#each spreadsheet.sheets as sheet, index}
        <button type="button" role="tab" aria-selected={activeSheet === index} class:active={activeSheet === index} onclick={() => (activeSheet = index)}>{sheet.name}</button>
      {/each}
    </div>
  {:else if kind === "presentation" && presentation}
    <div class="artifact-viewer__office-toolbar" aria-label="Slide controls">
      <button type="button" disabled={activeSlide === 0} onclick={() => (activeSlide -= 1)}>Previous</button>
      <span>Slide {activeSlide + 1} of {presentation.slides.length}</span>
      <button type="button" disabled={activeSlide >= presentation.slides.length - 1} onclick={() => (activeSlide += 1)}>Next</button>
    </div>
    <div class="artifact-viewer__slide-stage">
      <article class="artifact-viewer__slide" aria-label={`Slide ${activeSlide + 1}: ${presentation.slides[activeSlide].title}`}>
        {#each presentation.slides[activeSlide].paragraphs as paragraph, index}
          {#if index === 0}<h2>{paragraph}</h2>{:else}<p>{paragraph}</p>{/if}
        {/each}
      </article>
      {#if presentation.truncated}<p class="artifact-viewer__office-note">Only the first 200 slides are available in preview.</p>{/if}
    </div>
  {:else if kind === "html" && mode === "preview" && renderedHTML}
    <article class="artifact-viewer__document artifact-viewer__html">{@html renderedHTML}</article>
  {:else if kind === "markdown" && mode === "preview"}
    <article class="artifact-viewer__document artifact-viewer__markdown">{@html renderedHTML}</article>
  {:else if kind === "code"}
    <pre class="artifact-viewer__source"><code class="hljs">{@html highlightedSource}</code></pre>
  {:else}
    <pre class="artifact-viewer__source"><code>{source}</code></pre>
  {/if}
</div>

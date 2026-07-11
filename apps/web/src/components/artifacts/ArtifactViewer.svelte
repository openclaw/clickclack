<script lang="ts">
  import DOMPurify from "dompurify";
  import { onDestroy } from "svelte";
  import type {
    PDFDocumentLoadingTask,
    PDFDocumentProxy,
    PDFPageProxy,
    RenderTask,
  } from "pdfjs-dist";
  import {
    artifactKindLabel,
    artifactLanguage,
    artifactPreviewLimit,
    classifyArtifact,
    type ArtifactKind,
  } from "../../lib/artifacts";
  import { markdown } from "../../lib/format";
  import { convertDocxInWorker } from "../../lib/docx";
  import { highlightCodeInWorker } from "../../lib/highlight";
  import type { Upload } from "../../lib/types";
  import { formatBytes, uploadURL } from "../../lib/uploads";

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
  let highlightedSource = $state("");
  let pdfDocument: PDFDocumentProxy | null = $state(null);
  let pdfPage = $state(1);
  let pdfScale = $state(1);
  let pdfRendering = $state(false);
  let cleanupPDF: (() => void) | null = null;

  let label = $derived(artifactKindLabel(kind));
  let url = $derived(uploadURL(upload));
  let canToggleMarkdown = $derived(kind === "markdown" && status === "ready");

  function previewTooLargeMessage(limit: number): string {
    return `This ${label.toLowerCase()} is ${formatBytes(upload.byte_size)}. Preview is limited to ${formatBytes(limit)}.`;
  }

  function htmlDocument(body: string): string {
    const policy = [
      "default-src 'none'",
      "img-src data: blob:",
      "font-src data:",
      "style-src 'unsafe-inline'",
      "media-src data: blob:",
      "form-action 'none'",
      "base-uri 'none'",
    ].join("; ");
    const csp = `<meta http-equiv="Content-Security-Policy" content="${policy}">`;
    const base = '<base target="_self">';
    const sanitized = DOMPurify.sanitize(body, {
      WHOLE_DOCUMENT: true,
      FORBID_TAGS: ["base", "embed", "form", "iframe", "link", "object", "script"],
      FORBID_ATTR: ["action", "formaction", "srcset"],
    });
    const documentNode = new DOMParser().parseFromString(sanitized, "text/html");
    const localReference = /^(?:#|data:|blob:)/i;
    for (const element of documentNode.querySelectorAll<HTMLElement>("[src], [href], [poster]")) {
      for (const attribute of ["src", "href", "poster"] as const) {
        const value = element.getAttribute(attribute)?.trim();
        if (value && !localReference.test(value)) element.removeAttribute(attribute);
      }
    }
    const stripExternalCSS = (value: string) =>
      value
        .replace(/@import\s+(?:url\()?[^;]+;?/gi, "")
        .replace(/url\((?!\s*['"]?(?:data:|blob:))[^)]+\)/gi, "none");
    for (const element of documentNode.querySelectorAll<HTMLElement>("[style]")) {
      element.setAttribute("style", stripExternalCSS(element.getAttribute("style") || ""));
    }
    for (const style of documentNode.querySelectorAll("style")) {
      style.textContent = stripExternalCSS(style.textContent || "");
    }
    const safeBody = documentNode.documentElement.outerHTML;
    if (/<head[\s>]/i.test(safeBody)) {
      return `<!doctype html>${safeBody.replace(/<head([^>]*)>/i, `<head$1>${csp}${base}`)}`;
    }
    return `<!doctype html><html><head>${csp}${base}</head><body>${safeBody}</body></html>`;
  }

  async function responseBytes(signal: AbortSignal): Promise<ArrayBuffer> {
    const response = await fetch(url, { credentials: "same-origin", signal });
    if (!response.ok) {
      if (response.status === 401 || response.status === 403) {
        throw new Error("You no longer have access to this artifact.");
      }
      if (response.status === 404) throw new Error("This artifact is no longer available.");
      throw new Error(`Could not load this artifact (${response.status}).`);
    }
    return response.arrayBuffer();
  }

  async function loadText(signal: AbortSignal): Promise<string> {
    return new TextDecoder("utf-8", { fatal: false }).decode(await responseBytes(signal));
  }

  async function loadArtifact(signal: AbortSignal) {
    cleanupPDF?.();
    cleanupPDF = null;
    pdfDocument = null;
    source = "";
    renderedHTML = "";
    highlightedSource = "";
    pdfPage = 1;
    pdfScale = 1;
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
        const [pdfjs, worker] = await Promise.all([
          import("pdfjs-dist"),
          import("pdfjs-dist/build/pdf.worker.mjs?url"),
        ]);
        if (signal.aborted) return;
        pdfjs.GlobalWorkerOptions.workerSrc = worker.default;
        const loadingTask = pdfjs.getDocument({ url, withCredentials: true }) as PDFDocumentLoadingTask;
        cleanupPDF = () => {
          void loadingTask.destroy();
          pdfDocument = null;
        };
        pdfDocument = await loadingTask.promise;
      } else if (kind === "docx") {
        const bytes = await responseBytes(signal);
        if (signal.aborted) return;
        const html = await convertDocxInWorker(bytes, signal);
        renderedHTML = DOMPurify.sanitize(html, {
          FORBID_TAGS: ["form", "iframe", "object", "embed", "script", "style"],
          FORBID_ATTR: ["formaction"],
        });
      } else {
        source = await loadText(signal);
        if (signal.aborted) return;
        if (kind === "code") {
          highlightedSource = await highlightCodeInWorker(source, artifactLanguage(upload), signal);
        }
        if (kind === "markdown") renderedHTML = markdown(source);
        if (kind === "html") renderedHTML = htmlDocument(source);
      }
      if (!signal.aborted) status = "ready";
    } catch (error) {
      if (signal.aborted || (error instanceof Error && error.name === "AbortError")) return;
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
    let cancelled = false;
    let renderTask: RenderTask | null = null;
    pdfRendering = true;

    const render = async () => {
      try {
        const page: PDFPageProxy = await pdfDocument!.getPage(pdfPage);
        if (cancelled || !canvasEl) return;
        const viewport = page.getViewport({ scale: pdfScale });
        const dpr = Math.min(window.devicePixelRatio || 1, 2);
        const context = canvasEl.getContext("2d");
        if (!context) throw new Error("PDF canvas is unavailable.");
        canvasEl.width = Math.max(1, Math.floor(viewport.width * dpr));
        canvasEl.height = Math.max(1, Math.floor(viewport.height * dpr));
        canvasEl.style.width = `${viewport.width}px`;
        canvasEl.style.height = `${viewport.height}px`;
        context.setTransform(dpr, 0, 0, dpr, 0, 0);
        renderTask = page.render({ canvasContext: context, viewport });
        await renderTask.promise;
      } catch (error) {
        if (!cancelled && !(error instanceof Error && error.name === "RenderingCancelledException")) {
          status = "error";
          errorMessage = "Could not render this PDF page.";
        }
      } finally {
        if (!cancelled) pdfRendering = false;
      }
    };

    void render();
    return () => {
      cancelled = true;
      renderTask?.cancel();
    };
  });

  onDestroy(() => cleanupPDF?.());
</script>

<header class="artifact-viewer__header">
  <div class="artifact-viewer__identity">
    <span>{label}</span>
    <strong title={upload.filename}>{upload.filename}</strong>
    <small>{formatBytes(upload.byte_size)}</small>
  </div>
  <div class="artifact-viewer__actions">
    {#if canToggleMarkdown}
      <div class="artifact-viewer__segmented" aria-label="Markdown view">
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

<div class="artifact-viewer__body" class:is-pdf={kind === "pdf"} aria-live="polite">
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
    <div class="artifact-viewer__pdf-stage" class:is-rendering={pdfRendering}>
      <canvas bind:this={canvasEl} aria-label={`PDF page ${pdfPage}`}></canvas>
    </div>
  {:else if kind === "docx"}
    <article class="artifact-viewer__document">{@html renderedHTML}</article>
  {:else if kind === "html"}
    <iframe class="artifact-viewer__web" title={`Preview of ${upload.filename}`} sandbox="" srcdoc={renderedHTML}></iframe>
  {:else if kind === "markdown" && mode === "preview"}
    <article class="artifact-viewer__document artifact-viewer__markdown">{@html renderedHTML}</article>
  {:else if kind === "code"}
    <pre class="artifact-viewer__source"><code class="hljs">{@html highlightedSource}</code></pre>
  {:else}
    <pre class="artifact-viewer__source"><code>{source}</code></pre>
  {/if}
</div>

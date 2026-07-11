<script lang="ts">
  import { onDestroy } from "svelte";
  import type {
    PDFDocumentLoadingTask,
    PDFDocumentProxy,
    PDFPageProxy,
    RenderTask,
  } from "pdfjs-dist";
  import { artifactKindLabel, classifyArtifact } from "../lib/artifacts";
  import { assertSafePDFCanvas } from "../lib/pdf";
  import type { Upload } from "../lib/types";

  type Props = {
    upload: Upload;
    url: string;
    onOpenImage?: (url: string, title: string) => void;
    onOpenArtifact?: (upload: Upload) => void;
  };

  let { upload, url, onOpenImage = () => {}, onOpenArtifact = () => {} }: Props = $props();

  const MAX_MEDIA_HEIGHT = 360;
  const MIN_MEDIA_HEIGHT = 120;

  let videoEl: HTMLVideoElement | null = $state(null);
  let pdfCanvasEl: HTMLCanvasElement | null = $state(null);
  let started = $state(false);
  let pdfThumbnailReady = $state(false);
  let pdfThumbnailFailed = $state(false);
  let loadedDurationLabel = $state("");
  let durationLabel = $derived(loadedDurationLabel || formatDuration(upload.duration_ms ?? 0));
  let cleanupPDFThumbnail: (() => void) | null = null;

  let contentType = $derived((upload.content_type || "").split(";")[0].trim().toLowerCase());
  let isImage = $derived(contentType.startsWith("image/"));
  let isVideo = $derived(contentType.startsWith("video/"));
  let isAudio = $derived(contentType.startsWith("audio/"));
  let isPDF = $derived(contentType === "application/pdf");
  let artifactKind = $derived(classifyArtifact(upload));
  let canPreviewDocument = $derived(artifactKind !== "unsupported");
  let documentLabel = $derived(artifactKindLabel(artifactKind));

  let mediaStyle = $derived.by(() => {
    const w = upload.width ?? 0;
    const h = upload.height ?? 0;
    if (w <= 0 || h <= 0) return "";
    const cap = isImage ? 320 : MAX_MEDIA_HEIGHT;
    const ratioH = Math.min(cap, Math.max(MIN_MEDIA_HEIGHT, h));
    return `aspect-ratio: ${w} / ${h}; max-height: ${ratioH}px;`;
  });

  function formatDuration(ms: number): string {
    if (!ms || ms <= 0) return "";
    const total = Math.floor(ms / 1000);
    const m = Math.floor(total / 60);
    const s = total % 60;
    return `${m}:${s.toString().padStart(2, "0")}`;
  }

  function handlePlay() {
    started = true;
  }

  function handleLoadedMetadata() {
    if (!videoEl || !isFinite(videoEl.duration)) return;
    loadedDurationLabel = formatDuration(videoEl.duration * 1000);
  }

  function startPlayback() {
    if (!videoEl) return;
    started = true;
    void videoEl.play();
  }

  function formatBytes(size: number) {
    if (size < 1024) return `${size} B`;
    if (size < 1024 * 1024) return `${Math.round(size / 1024)} KB`;
    return `${(size / (1024 * 1024)).toFixed(1)} MB`;
  }

  $effect(() => {
    if (!isPDF || !pdfCanvasEl) {
      pdfThumbnailReady = false;
      pdfThumbnailFailed = false;
      return;
    }

    cleanupPDFThumbnail?.();

    let cancelled = false;
    let renderTask: RenderTask | null = null;
    let loadingTask: PDFDocumentLoadingTask | null = null;
    let pdfDoc: PDFDocumentProxy | null = null;

    pdfThumbnailReady = false;
    pdfThumbnailFailed = false;

    const render = async () => {
      try {
        const [pdfjs, worker] = await Promise.all([
          import("pdfjs-dist"),
          import("pdfjs-dist/build/pdf.worker.mjs?url"),
        ]);
        pdfjs.GlobalWorkerOptions.workerSrc = worker.default;
        loadingTask = pdfjs.getDocument({ url, withCredentials: true }) as typeof loadingTask;
        pdfDoc = await loadingTask.promise;
        const page: PDFPageProxy = await pdfDoc.getPage(1);
        if (cancelled) return;

        const baseViewport = page.getViewport({ scale: 1 });
        const scale = 128 / baseViewport.width;
        const viewport = page.getViewport({ scale });
        const canvas = pdfCanvasEl;
        const context = canvas?.getContext("2d");
        if (!canvas || !context) throw new Error("pdf thumbnail canvas unavailable");

        const dpr = Math.min(window.devicePixelRatio || 1, 2);
        const backingWidth = Math.max(1, Math.floor(viewport.width * dpr));
        const backingHeight = Math.max(1, Math.floor(viewport.height * dpr));
        assertSafePDFCanvas(backingWidth, backingHeight);
        canvas.width = backingWidth;
        canvas.height = backingHeight;
        context.setTransform(dpr, 0, 0, dpr, 0, 0);
        renderTask = page.render({ canvasContext: context, viewport });
        await renderTask.promise;
        if (!cancelled) pdfThumbnailReady = true;
      } catch (error) {
        if (!cancelled && !(error instanceof Error && error.name === "RenderingCancelledException")) {
          pdfThumbnailFailed = true;
        }
      }
    };

    void render();
    cleanupPDFThumbnail = () => {
      cancelled = true;
      renderTask?.cancel();
      void pdfDoc?.destroy();
      void loadingTask?.destroy();
      cleanupPDFThumbnail = null;
    };

    return () => cleanupPDFThumbnail?.();
  });

  onDestroy(() => cleanupPDFThumbnail?.());
</script>

{#if isImage}
  <div class="media-tile media-tile--image">
    <button
      type="button"
      class="media-tile__open"
      aria-label={`Open image ${upload.filename}`}
      onclick={() => onOpenImage(url, upload.filename)}
    >
      <img
        src={url}
        alt={upload.filename}
        loading="lazy"
        decoding="async"
        width={upload.width || undefined}
        height={upload.height || undefined}
        style={mediaStyle}
      />
    </button>
    <div class="media-tile__caption">
      <span class="media-tile__name">{upload.filename}</span>
      <a
        class="media-tile__chip"
        href={url}
        download={upload.filename}
        aria-label={`Download ${upload.filename}`}
        onclick={(event) => event.stopPropagation()}
      >
        <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
          <path
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M12 4v12m0 0 4-4m-4 4-4-4M5 20h14"
          />
        </svg>
      </a>
    </div>
  </div>
{:else if isVideo}
  <div class="media-tile media-tile--video" class:is-started={started}>
    <video
      bind:this={videoEl}
      preload="metadata"
      playsinline
      controls={started}
      controlslist="nodownload"
      aria-label={upload.filename}
      width={upload.width || undefined}
      height={upload.height || undefined}
      style={mediaStyle}
      onplay={handlePlay}
      onloadedmetadata={handleLoadedMetadata}
    >
      <source src={url} type={contentType} />
    </video>
    {#if !started}
      <button
        type="button"
        class="media-tile__play"
        aria-label={`Play ${upload.filename}`}
        onclick={startPlayback}
      >
        <span class="media-tile__play-icon" aria-hidden="true">
          <svg viewBox="0 0 24 24" width="26" height="26">
            <path fill="currentColor" d="M8 5.5v13l11-6.5z" />
          </svg>
        </span>
      </button>
      {#if durationLabel}
        <span class="media-tile__duration" aria-hidden="true">{durationLabel}</span>
      {/if}
    {/if}
    <div class="media-tile__caption">
      <span class="media-tile__name">{upload.filename}</span>
      <a
        class="media-tile__chip"
        href={url}
        download={upload.filename}
        aria-label={`Download ${upload.filename}`}
        onclick={(event) => event.stopPropagation()}
      >
        <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
          <path
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M12 4v12m0 0 4-4m-4 4-4-4M5 20h14"
          />
        </svg>
      </a>
    </div>
  </div>
{:else if isAudio}
  <div class="audio-attachment">
    <div class="audio-attachment__meta">
      <span class="file-icon" aria-hidden="true">♪</span>
      <span>
        <strong>{upload.filename}</strong>
        <small>{formatBytes(upload.byte_size)}</small>
      </span>
    </div>
    <audio controls preload="metadata" src={url}>
      <a href={url} target="_blank" rel="noreferrer">{upload.filename}</a>
    </audio>
  </div>
{:else if canPreviewDocument}
  <div class="document-attachment">
    <button
      type="button"
      class="document-attachment__thumbnail"
      class:has-preview={pdfThumbnailReady}
      class:thumbnail-failed={pdfThumbnailFailed}
      aria-label={`Open ${upload.filename}`}
      onclick={() => onOpenArtifact(upload)}
    >
      {#if isPDF}
        <canvas
          bind:this={pdfCanvasEl}
          class="document-attachment__preview-canvas"
          aria-hidden="true"
        ></canvas>
      {/if}
      <span>{documentLabel}</span>
    </button>
    <div class="document-attachment__meta">
      <button type="button" class="document-attachment__title" onclick={() => onOpenArtifact(upload)}>
        {upload.filename}
      </button>
      <small>{formatBytes(upload.byte_size)}</small>
    </div>
    <a
      class="document-attachment__download"
      href={url}
      download={upload.filename}
      aria-label={`Download ${upload.filename}`}
    >
      <svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true">
        <path
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          d="M12 4v12m0 0 4-4m-4 4-4-4M5 20h14"
        />
      </svg>
    </a>
  </div>
{:else}
  <a class="file-attachment" href={url} target="_blank" rel="noreferrer">
    <span class="file-icon" aria-hidden="true">↧</span>
    <span>
      <strong>{upload.filename}</strong>
      <small>{formatBytes(upload.byte_size)}</small>
    </span>
  </a>
{/if}

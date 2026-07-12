<script lang="ts">
  import { artifactKindLabel, classifyArtifact } from "../lib/artifacts";
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
  let started = $state(false);
  let loadedDurationLabel = $state("");
  let durationLabel = $derived(loadedDurationLabel || formatDuration(upload.duration_ms ?? 0));

  let contentType = $derived((upload.content_type || "").split(";")[0].trim().toLowerCase());
  let artifactKind = $derived(classifyArtifact(upload));
  let isImage = $derived(artifactKind === "unsupported" && contentType.startsWith("image/"));
  let isVideo = $derived(artifactKind === "unsupported" && contentType.startsWith("video/"));
  let isAudio = $derived(artifactKind === "unsupported" && contentType.startsWith("audio/"));
  let canPreviewDocument = $derived(
    artifactKind === "code" ||
      artifactKind === "text" ||
      artifactKind === "markdown" ||
      artifactKind === "pdf" ||
      artifactKind === "spreadsheet" ||
      artifactKind === "presentation" ||
      artifactKind === "html",
  );
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
      data-artifact-upload-id={upload.id}
      aria-label={`Open ${upload.filename}`}
      onclick={() => onOpenArtifact(upload)}
    >
      <span>{documentLabel}</span>
    </button>
    <div class="document-attachment__meta">
      <button
        type="button"
        class="document-attachment__title"
        data-artifact-upload-id={upload.id}
        onclick={() => onOpenArtifact(upload)}
      >
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
  <a class="file-attachment" href={url} download={upload.filename} aria-label={`Download ${upload.filename}`}>
    <span class="file-icon" aria-hidden="true">↧</span>
    <span>
      <strong>{upload.filename}</strong>
      <small>{formatBytes(upload.byte_size)}</small>
    </span>
  </a>
{/if}

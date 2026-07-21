<script lang="ts">
  import { onDestroy, onMount } from "svelte";

  type Props = {
    url: string;
    title: string;
    onClose: () => void;
  };

  let { url, title, onClose }: Props = $props();
  let scrimElement: HTMLElement | null = $state(null);
  let dialogElement: HTMLElement | null = $state(null);
  let closeButton: HTMLButtonElement | null = $state(null);
  const opener =
    typeof document !== "undefined" && document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null;
  const threadScope = opener?.closest(".thread");
  const timelineScope = opener?.closest(".timeline");
  const focusFallback =
    threadScope?.querySelector<HTMLElement>('[aria-label="Reply body"]:not(:disabled)') ??
    threadScope?.querySelector<HTMLElement>('[aria-label="Close thread"]') ??
    timelineScope?.querySelector<HTMLElement>('[aria-label="Message body"]:not(:disabled)') ??
    timelineScope?.querySelector<HTMLElement>('[aria-label="Search messages"]') ??
    (typeof document !== "undefined"
      ? document.querySelector<HTMLElement>('[aria-label="Reply body"]:not(:disabled)') ??
        document.querySelector<HTMLElement>('[aria-label="Message body"]:not(:disabled)') ??
        document.querySelector<HTMLElement>('[aria-label="Close thread"]') ??
        document.querySelector<HTMLElement>('[aria-label="Search messages"]')
      : null);
  const inertSiblings = new Set<HTMLElement>();

  onMount(() => {
    const parent = scrimElement?.parentElement;
    if (parent) {
      for (const sibling of parent.children) {
        if (!(sibling instanceof HTMLElement) || sibling === scrimElement || sibling.inert) continue;
        sibling.inert = true;
        inertSiblings.add(sibling);
      }
    }
    closeButton?.focus({ preventScroll: true });
  });

  onDestroy(() => {
    for (const sibling of inertSiblings) sibling.inert = false;
    inertSiblings.clear();
    if (opener?.isConnected && opener !== document.body) {
      opener.focus({ preventScroll: true });
      if (document.activeElement === opener) return;
    }
    if (focusFallback?.isConnected) focusFallback.focus({ preventScroll: true });
  });

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      onClose();
      return;
    }
    if (event.key !== "Tab" || !dialogElement) return;
    const focusable = Array.from(
      dialogElement.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), [tabindex]:not([tabindex="-1"])',
      ),
    ).filter((element) => !element.inert && element.getClientRects().length > 0);
    if (focusable.length === 0) {
      event.preventDefault();
      dialogElement.focus();
      return;
    }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && (document.activeElement === first || !dialogElement.contains(document.activeElement))) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && (document.activeElement === last || !dialogElement.contains(document.activeElement))) {
      event.preventDefault();
      first.focus();
    }
  }
</script>

<div bind:this={scrimElement} class="modal-scrim image-viewer-scrim" role="presentation">
  <button
    class="modal-backdrop"
    type="button"
    tabindex="-1"
    aria-label="Close image viewer"
    onclick={onClose}
  ></button>
  <div
    bind:this={dialogElement}
    class="image-viewer"
    role="dialog"
    aria-modal="true"
    aria-label={`Image viewer: ${title}`}
    tabindex="-1"
    onkeydown={handleKeydown}
  >
    <header>
      <strong>{title}</strong>
      <div>
        <a href={url} target="_blank" rel="noreferrer">Open original</a>
        <button
          bind:this={closeButton}
          class="image-viewer__close"
          type="button"
          aria-label="Close image viewer"
          onclick={onClose}
        >
          ×
        </button>
      </div>
    </header>
    <div class="image-viewer-stage">
      <img src={url} alt={title} />
    </div>
  </div>
</div>

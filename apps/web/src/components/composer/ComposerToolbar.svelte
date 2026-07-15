<script lang="ts">
  type Props = {
    showGifPicker: boolean;
    disabled?: boolean;
    onWrap: (before: string, after?: string) => void;
    onAppend: (snippet: string) => void;
    onToggleGif: () => void;
  };

  let { showGifPicker, disabled = false, onWrap, onAppend, onToggleGif }: Props = $props();
</script>

<div class="composer-toolbar" aria-label="Message tools">
  <button type="button" title="Bold" aria-label="Bold" {disabled} onclick={() => onWrap("**")}>
    <strong>B</strong>
  </button>
  <button type="button" title="Italic" aria-label="Italic" {disabled} onclick={() => onWrap("_")}>
    <em>I</em>
  </button>
  <button type="button" title="Code" aria-label="Code" {disabled} onclick={() => onWrap("`")}>
    <span>{`<>`}</span>
  </button>
  <button type="button" title="Code block" aria-label="Code block" {disabled} onclick={() => onWrap("```", "\n```")}>
    <span>{`{}`}</span>
  </button>
  <button type="button" title="Link" aria-label="Link" {disabled} onclick={() => onAppend("[label](https://)")}>
    <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
      <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M10 13a5 5 0 0 0 7.07 0l2.12-2.12a5 5 0 0 0-7.07-7.07L11 4.93M14 11a5 5 0 0 0-7.07 0L4.81 13.12a5 5 0 0 0 7.07 7.07L13 19.07"/>
    </svg>
  </button>
  <button
    type="button"
    title="GIF picker"
    aria-label="GIF picker"
    class:active={showGifPicker}
    {disabled}
    onclick={onToggleGif}
  >
    GIF
  </button>
</div>

<script lang="ts">
  type Props = { size?: number; class?: string };

  let { size = 44, class: className = "" }: Props = $props();
</script>

<!-- The Keystroke mark: a raised porcelain key and a pressed cyan key on the
     navy board. The colors mirror apps/web/static/favicon.svg and must change
     together. The board is CSS rather than an SVG rect so a tile can hand the
     mark its own border radius. -->
<span class="keystroke-mark {className}" style="--keystroke-size: {size}px" aria-hidden="true">
  <svg viewBox="0 0 64 64" focusable="false">
    <rect x="10" y="21" width="20" height="20" rx="5" fill="#070b12" />
    <rect x="10" y="16" width="20" height="20" rx="5" fill="#edf2f9" />
    <rect x="34" y="28" width="20" height="20" rx="5" fill="#17638f" />
    <rect x="34" y="26.5" width="20" height="20" rx="5" fill="#41c6ff" />
  </svg>
</span>

<style>
  .keystroke-mark {
    display: block;
    flex: none;
    width: var(--keystroke-size);
    height: var(--keystroke-size);
    border-radius: 22%;
    background: #141926;
    /* The faint plate ring from the desktop icon keeps the navy board legible
       on dark panels. */
    box-shadow: inset 0 0 0 1px rgba(190, 210, 255, 0.13);
    overflow: hidden;
  }

  .keystroke-mark svg {
    display: block;
    width: 100%;
    height: 100%;
  }
</style>

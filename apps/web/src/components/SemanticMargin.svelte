<script lang="ts">
  import { inspectMode } from '../lib/ui';

  // Track B will pass per-message intent data and line indices.
  // For now, this is the structural shell — static grid marks
  // and a scrollable content area for Track B to populate.

  let {
    scrollTop = 0,
    messageCount = 0,
  }: {
    scrollTop?: number;
    messageCount?: number;
  } = $props();

  const gridMarkCount = 80; // static alignment marks

  // Derived line numbers for the visible range
  let lineNumbers = $derived(
    Array.from({ length: Math.max(messageCount, 20) }, (_, i) => i + 1),
  );

  let isInspectMode = false;
  inspectMode.subscribe((v) => (isInspectMode = v));
</script>

<aside
  class="semantic-margin"
  class:inspect={isInspectMode}
  aria-label="Semantic margin"
  role="complementary"
>
  <!-- Static alignment grid marks -->
  <div class="semantic-margin__grid" aria-hidden="true">
    {#each Array(gridMarkCount) as _, i}
      <span class="semantic-margin__grid-mark"></span>
    {/each}
  </div>

  <!-- Scrollable content: line counters and intent ticks -->
  <div class="semantic-margin__scroll" style="transform: translateY(-{scrollTop}px)">
    <!-- Line counters -->
    <div class="semantic-margin__lines" aria-hidden="true">
      {#each lineNumbers as n}
        <span class="semantic-margin__line-num">{n.toString().padStart(3, '0')}</span>
      {/each}
    </div>

    <!-- Intent indicator ticks (populated by Track B) -->
    <div class="semantic-margin__intents" aria-hidden="true">
      {#each Array(messageCount) as _, i}
        <span class="semantic-margin__intent-tick"></span>
      {/each}
    </div>
  </div>
</aside>

<style>
  .semantic-margin {
    position: relative;
    width: 48px;
    min-width: 48px;
    background: var(--rail);
    border-right: 1px solid var(--line);
    overflow: hidden;
    user-select: none;
    display: grid;
    grid-template-rows: 1fr;
    /* All transitions use rigid motion tokens */
    transition: background var(--motion-fast), border-color var(--motion-fast);
  }

  .semantic-margin.inspect {
    background: var(--panel);
    border-right-color: var(--line-strong);
  }

  /* -------- Static alignment grid marks -------- */
  .semantic-margin__grid {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    pointer-events: none;
    padding-top: 0;
  }

  .semantic-margin__grid-mark {
    flex: 0 0 20px;
    border-bottom: 1px solid var(--line);
    opacity: 0.4;
  }

  /* -------- Scrollable overlay -------- */
  .semantic-margin__scroll {
    position: relative;
    z-index: 1;
    display: grid;
    grid-template-columns: 1fr;
    padding-top: 49px; /* aligns with first message row */
  }

  /* -------- Line counters -------- */
  .semantic-margin__lines {
    display: flex;
    flex-direction: column;
  }

  .semantic-margin__line-num {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    padding: 0 6px 0 0;
    height: 20px;
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 500;
    color: var(--muted-2);
    letter-spacing: 0.02em;
    font-feature-settings: "tnum";
    font-variant-numeric: tabular-nums;
  }

  /* -------- Intent indicator ticks -------- */
  .semantic-margin__intents {
    position: absolute;
    inset: 0;
    padding-top: 49px;
    pointer-events: none;
  }

  .semantic-margin__intent-tick {
    display: block;
    height: 20px;
    margin-left: 4px;
    border-left: 2px solid var(--line);
    transition: border-color var(--motion-fast);
  }

  /* Intent edge band colors (§8.4).
     Track B sets these via CSS class on each tick. */
  :global(.semantic-margin__intent-tick.intent-ask)    { border-left-color: var(--intent-ask); }
  :global(.semantic-margin__intent-tick.intent-command) { border-left-color: var(--intent-command); }
  :global(.semantic-margin__intent-tick.intent-reflect) { border-left-color: var(--intent-reflect); }
  :global(.semantic-margin__intent-tick.intent-draft)   { border-left-color: var(--intent-draft); }
  :global(.semantic-margin__intent-tick.intent-clarify) { border-left-color: var(--intent-clarify); }
  :global(.semantic-margin__intent-tick.intent-explore) { border-left-color: var(--intent-explore); }
</style>

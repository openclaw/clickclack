<script lang="ts">
  import { markdown } from "../../lib/format";
  import { toolDetail } from "../../lib/chat/tool-detail";
  import type { PreambleBlock } from "../../lib/types";

  type Props = {
    block: PreambleBlock;
  };

  let { block }: Props = $props();

  // Block lifecycle: while the turn is live (final === false) the block opens
  // expanded so the operator watches narration stream in. Once the turn ends
  // (final === true) it collapses to a single line they can re-expand. The
  // operator's manual toggle wins for the rest of the session via preambleOpen,
  // but a state flip (live -> final) re-applies the default once.
  let preambleOpen = $state(true);
  let lastFinal = $state<boolean | undefined>(undefined);
  $effect(() => {
    if (lastFinal === undefined || block.final !== lastFinal) {
      lastFinal = block.final;
      preambleOpen = !block.final;
    }
  });

  // Per-tool expansion: every tool row renders collapsed to a one-line summary
  // (glyph + action + tool name + truncated detail) and expands on click to
  // the full stored body. Tracked per row id so expanding one never touches
  // the others, and live updates don't reset an operator's expansion.
  let expandedTools = $state<Record<string, boolean>>({});

  function toggleTool(id: string) {
    expandedTools[id] = !expandedTools[id];
  }

  // Split a stored tool body ("**head**\n\ntext" from the bridge) into the
  // step-chain head and the output text. The head renders as a plain styled
  // line, not markdown: chains regularly contain characters (*/, **, |) that
  // break markdown emphasis and would render as literal asterisks.
  function splitFull(full: string): { head?: string; text: string } {
    const match = full.match(/^\*\*([^]*?)\*\*\s*\n*([^]*)$/);
    if (!match) return { text: full };
    const head = match[1].trim();
    const text = match[2].trim();
    return { head: head || undefined, text };
  }

  // Interleaved items in arrival order; tool rows resolve to display detail.
  let resolved = $derived(
    block.items.map((item) =>
      item.type === "tool" ? { item, tool: toolDetail(item.name, item.detail) } : { item, tool: null },
    ),
  );
  // Final turns label the pill with the visibility state (expanded/collapsed)
  // instead of a lifecycle word; "live" remains while the turn streams.
  let stateLabel = $derived(block.final ? (preambleOpen ? "expanded" : "collapsed") : "live");
  let toggleLabel = $derived(preambleOpen ? "Hide preamble" : "Show preamble");
</script>

<section class="preamble-contract" aria-label="Agent preamble" class:is-final={block.final}>
  <button
    type="button"
    class="preamble-toggle"
    aria-expanded={preambleOpen}
    onclick={() => (preambleOpen = !preambleOpen)}
  >
    <span class="preamble-chevron" class:open={preambleOpen} aria-hidden="true">▸</span>
    <span class="preamble-state" class:is-live={!block.final}>{stateLabel}</span>
    <span class="preamble-action">{toggleLabel}</span>
  </button>
  {#if preambleOpen}
    <div class="preamble-flow">
      {#each resolved as entry (entry.item.id)}
        {#if entry.item.type === "commentary"}
          <div class="markdown preamble-body">{@html markdown(entry.item.body)}</div>
        {:else if entry.tool}
          {@const open = expandedTools[entry.item.id] === true}
          <div class="preamble-tool" class:expanded={open}>
            <button
              type="button"
              class="preamble-tool-summary"
              aria-expanded={open}
              onclick={() => toggleTool(entry.item.id)}
            >
              <span class="tool-line-chevron" class:open={open} aria-hidden="true">▸</span>
              <span class="tool-line-glyph" aria-hidden="true">{entry.tool.glyph}</span>
              <span class="tool-line-action">{entry.tool.action}</span>
              {#if entry.tool.name && entry.tool.name !== "tool"}
                <span class="tool-line-name">{entry.tool.name}</span>
              {/if}
              {#if !open && entry.tool.detail}
                <span class="tool-line-detail" title={entry.tool.detail}>{entry.tool.detail}</span>
              {/if}
            </button>
            {#if open}
              {@const full = splitFull(entry.item.full)}
              <div class="preamble-tool-full">
                {#if full.head}
                  <div class="preamble-tool-full-head">{full.head}</div>
                {/if}
                {#if full.text}
                  <div class="markdown preamble-tool-full-body">{@html markdown(full.text)}</div>
                {/if}
              </div>
            {/if}
          </div>
        {/if}
      {/each}
    </div>
  {/if}
</section>

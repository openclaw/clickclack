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
  let preambleOpen = $state(!block.final);
  let lastFinal = $state(block.final);
  $effect(() => {
    if (block.final !== lastFinal) {
      lastFinal = block.final;
      preambleOpen = !block.final;
    }
  });

  // Tool sub-items are collapsed by default (ragesaq's spec), independent of the
  // whole-block toggle, so an operator can read the prose without the tool list
  // unless they ask for it.
  let toolsOpen = $state(false);

  let toolCount = $derived(block.tools.length);
  let resolvedTools = $derived(block.tools.map((t) => ({ key: t.id, ...toolDetail(t.name, t.detail) })));
  let stateLabel = $derived(block.final ? "done" : "live");
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
    <span class="preamble-title">Preamble</span>
    <span class="preamble-state" class:is-live={!block.final}>{stateLabel}</span>
    <span class="preamble-action">{toggleLabel}</span>
  </button>
  {#if preambleOpen}
    {#if block.commentary.trim()}
      <div class="markdown preamble-body">{@html markdown(block.commentary)}</div>
    {/if}
    {#if toolCount > 0}
      <div class="preamble-tools" class:expanded={toolsOpen}>
        <button
          type="button"
          class="preamble-tools-toggle"
          aria-expanded={toolsOpen}
          onclick={() => (toolsOpen = !toolsOpen)}
        >
          <span class="preamble-tools-chevron" class:open={toolsOpen} aria-hidden="true">▸</span>
          <span class="preamble-tools-label"
            >{toolsOpen ? "Hide" : "Show"} {toolCount} tool {toolCount === 1 ? "call" : "calls"}</span
          >
        </button>
        {#if toolsOpen}
          <ol class="tool-line" aria-label="Tool calls">
            {#each resolvedTools as detail (detail.key)}
              <li class="tool-line-row">
                <span class="tool-line-glyph" aria-hidden="true">{detail.glyph}</span>
                <span class="tool-line-action">{detail.action}</span>
                {#if detail.name && detail.name !== "tool"}
                  <span class="tool-line-name">{detail.name}</span>
                {/if}
                {#if detail.detail}
                  <span class="tool-line-detail" title={detail.detail}>{detail.detail}</span>
                {/if}
              </li>
            {/each}
          </ol>
        {/if}
      </div>
    {/if}
  {/if}
</section>

<script lang="ts">
  import { inspectMode } from "$lib/ui";

  let isInspect = $state(false);
  $effect(() => {
    const unsub = inspectMode.subscribe((v) => (isInspect = v));
    return unsub;
  });

  let { messageCount = 0, intents = [] as string[] } = $props<{
    messageCount?: number;
    intents?: string[];
  }>();
</script>

<aside class="semantic-margin" class:inspect={isInspect} aria-label="semantic margin">
  <div class="margin-grid" aria-hidden="true">
    {#each Array.from({ length: 24 }) as _, i}
      <span class="grid-mark"></span>
    {/each}
  </div>
  <div class="margin-lines">
    {#each Array.from({ length: messageCount }) as _, i}
      <span class="line-counter">{String(i + 1).padStart(3, "0")}</span>
    {/each}
  </div>
  <div class="margin-intents">
    {#each intents as intent}
      <span
        class="intent-tick"
        class:intent-ask={intent === "ask"}
        class:intent-command={intent === "command"}
        class:intent-reflect={intent === "reflect"}
        class:intent-draft={intent === "draft"}
        class:intent-clarify={intent === "clarify"}
        class:intent-explore={intent === "explore"}
      ></span>
    {/each}
  </div>
</aside>

<style>
  .semantic-margin {
    grid-column: 1;
    grid-row: 1;
    display: grid;
    grid-template-rows: 1fr 1fr 1fr;
    border-right: 1px solid var(--line);
    background: var(--rail);
    overflow: hidden;
    transition: background var(--motion-fast);
  }
  .semantic-margin.inspect {
    background: rgba(0, 136, 255, 0.04);
  }
  .margin-grid {
    display: grid;
    grid-template-rows: repeat(24, 1fr);
    opacity: 0.35;
  }
  .grid-mark {
    border-bottom: 1px solid var(--line);
  }
  .margin-lines {
    font-family: var(--font-mono);
    font-size: 9px;
    color: var(--muted);
    text-align: center;
    overflow: hidden;
    line-height: 1.6;
  }
  .line-counter {
    display: block;
  }
  .margin-intents {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding-top: 4px;
  }
  .intent-tick {
    width: 2px;
    height: 10px;
    background: var(--intent-default);
  }
  .intent-ask { background: var(--intent-ask); }
  .intent-command { background: var(--intent-command); }
  .intent-reflect { background: var(--intent-reflect); }
  .intent-draft { background: var(--intent-draft); }
  .intent-clarify { background: var(--intent-clarify); }
  .intent-explore { background: var(--intent-explore); }
</style>

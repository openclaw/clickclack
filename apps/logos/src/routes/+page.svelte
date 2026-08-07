<script lang="ts">
  import { currentPersona, semanticPaneOpen, telemetryOpen, type Persona } from "$lib/ui";

  let persona = $state<Persona>("operator");
  $effect(() => {
    const unsub = currentPersona.subscribe((v) => (persona = v));
    return unsub;
  });

  let statusLine = $state("LOGOS console — awaiting workspace connection");
</script>

<div class="console">
  <header class="console-topbar logos-mono">
    <span class="brand">LOGOS</span>
    <span class="spacer"></span>
    <span class="persona-tag accent-intent">PERSONA: {persona.toUpperCase()}</span>
    <button class="ghost" onclick={() => semanticPaneOpen.set(!$semanticPaneOpen)}>THREADS</button>
    <button class="ghost" onclick={() => telemetryOpen.set(!$telemetryOpen)}>TELEMETRY</button>
  </header>

  <div class="console-body">
    <section class="pane chat-pane">
      <div class="pane-head logos-mono">CHAT STREAM <span class="accent-thread">· substrate: clickclack API</span></div>
      <div class="chat-placeholder logos-mono">
        <p>Chat pane — the only piece inherited from clickclack.</p>
        <p>Wired to the clickclack API (messages, realtime) as substrate.</p>
        <p class="accent-verified">[STATUS: {statusLine}]</p>
      </div>
    </section>

    <aside class="pane right-pane" class:open={$semanticPaneOpen}>
      <div class="pane-head logos-mono">SEMANTIC THREADS</div>
      <div class="pane-body logos-mono">
        <p class="muted">Cluster workspace → CL-01, CL-02…</p>
        <p class="muted">Cross-thread retrieval → #NODE-XX (score)</p>
      </div>
    </aside>
  </div>

  <footer class="console-statusbar logos-mono">
    <span>⌘K palette</span><span>·</span><span>Alt inspect</span><span>·</span>
    <span>j/k navigate</span><span>·</span><span class="accent-thread">:{persona} switch</span>
  </footer>
</div>

<style>
  .console {
    display: grid;
    grid-template-rows: 40px minmax(0, 1fr) 28px;
    height: 100%;
  }
  .console-topbar {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 0 12px;
    border-bottom: 1px solid var(--line);
    background: var(--panel);
  }
  .brand {
    font-weight: 700;
    color: var(--text-strong);
    letter-spacing: 0.12em;
  }
  .spacer { flex: 1; }
  .persona-tag {
    border: 1px solid var(--line-strong);
    padding: 2px 6px;
  }
  .ghost {
    background: transparent;
    border: 1px solid var(--line);
    color: var(--muted);
    font-family: var(--font-mono);
    font-size: 11px;
    padding: 2px 8px;
    cursor: pointer;
  }
  .ghost:hover { color: var(--text-strong); border-color: var(--line-strong); }
  .console-body {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 0px;
    min-height: 0;
    transition: grid-template-columns var(--motion-med);
  }
  .console-body:has(.right-pane.open) {
    grid-template-columns: minmax(0, 1fr) 340px;
  }
  .pane {
    border-right: 1px solid var(--line);
    display: grid;
    grid-template-rows: 30px minmax(0, 1fr);
    min-height: 0;
  }
  .pane-head {
    display: flex;
    align-items: center;
    padding: 0 10px;
    border-bottom: 1px solid var(--line);
    background: var(--panel-2);
    color: var(--muted-2);
  }
  .right-pane {
    overflow: hidden;
    transition: all var(--motion-med);
  }
  .right-pane:not(.open) {
    opacity: 0;
    pointer-events: none;
  }
  .pane-body {
    padding: 10px;
    color: var(--muted);
    line-height: 1.7;
  }
  .chat-placeholder {
    padding: 14px;
    color: var(--muted);
    line-height: 1.8;
  }
  .muted { color: var(--muted); }
  .console-statusbar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 12px;
    border-top: 1px solid var(--line);
    background: var(--panel);
    color: var(--muted-2);
  }
</style>

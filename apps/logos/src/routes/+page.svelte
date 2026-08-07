<script lang="ts">
  import { currentPersona, semanticPaneOpen, telemetryOpen, type Persona } from "$lib/ui";
  import ChatStream from "$lib/components/ChatStream.svelte";
  import SemanticThreadPane from "$lib/components/SemanticThreadPane.svelte";
  import { chatState } from "$lib/clickclack/chat";

  let persona = $state<Persona>("operator");
  $effect(() => {
    const unsub = currentPersona.subscribe((v) => (persona = v));
    return unsub;
  });

  // Feed the semantic pane with the current message window
  let threadMessages = $state<Array<{ id: string; content: string }>>([]);
  $effect(() => {
    const unsub = chatState.subscribe((v) => {
      threadMessages = v.messages.map((m) => ({ id: m.id, content: m.body ?? "" }));
    });
    return unsub;
  });

  function focusMessage(messageId: string) {
    // Find the message row and scroll it into view
    const el = document.querySelector(`[data-msg-id="${messageId}"]`);
    el?.scrollIntoView({ block: "center" });
    el?.classList.add("flash-highlight");
    setTimeout(() => el?.classList.remove("flash-highlight"), 1200);
  }
</script>

<div class="console">
  <header class="console-topbar logos-mono">
    <span class="brand">LOGOS</span>
    <span class="spacer"></span>
    <span class="persona-tag accent-intent">PERSONA: {persona.toUpperCase()}</span>
    <button class="ghost" class:active={$semanticPaneOpen} onclick={() => semanticPaneOpen.set(!$semanticPaneOpen)}>THREADS</button>
    <button class="ghost" class:active={$telemetryOpen} onclick={() => telemetryOpen.set(!$telemetryOpen)}>TELEMETRY</button>
  </header>

  <div class="console-body" class:semantic-open={$semanticPaneOpen}>
    <section class="pane chat-pane">
      <div class="pane-head logos-mono">CHAT STREAM <span class="accent-thread">· substrate: clickclack API</span></div>
      <div class="pane-body chat-body">
        <ChatStream />
      </div>
    </section>

    <aside class="pane right-pane" class:open={$semanticPaneOpen}>
      <SemanticThreadPane
        messages={threadMessages}
        onClose={() => semanticPaneOpen.set(false)}
        onFocusMessage={focusMessage}
      />
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
  .ghost:hover,
  .ghost.active {
    color: var(--text-strong);
    border-color: var(--line-strong);
  }
  .console-body {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 0px;
    min-height: 0;
    transition: grid-template-columns var(--motion-med);
  }
  .console-body.semantic-open {
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
  .chat-body {
    min-height: 0;
  }
  .right-pane {
    overflow: hidden;
    transition: all var(--motion-med);
  }
  .right-pane:not(.open) {
    opacity: 0;
    pointer-events: none;
  }
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

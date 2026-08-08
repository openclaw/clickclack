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
    <span>j/k navigate</span><span>·</span><span>Enter inspect</span><span>·</span>
    <span>Esc close</span><span>·</span><span class="accent-thread">:{persona} switch</span>
  </footer>
</div>

<style>
  .console {
    display: grid;
    grid-template-rows: 52px minmax(0, 1fr) 36px;
    height: 100%;
    min-width: 0;
  }
  .console-topbar {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: 0 var(--space-4);
    border-bottom: 1px solid var(--line);
    background: color-mix(in srgb, var(--panel) 82%, transparent);
    backdrop-filter: blur(12px);
  }
  .brand {
    font-weight: 700;
    color: var(--text-strong);
    letter-spacing: 0.14em;
    font-size: 12px;
  }
  .spacer { flex: 1; }
  .persona-tag {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-height: 32px;
    padding: 0 12px;
    border: 1px solid color-mix(in srgb, var(--accent-intent) 28%, var(--line));
    border-radius: var(--radius-pill);
    background: color-mix(in srgb, var(--accent-intent) 10%, transparent);
    color: var(--text-strong);
    box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.02);
  }
  .ghost {
    min-height: 34px;
    padding: 0 12px;
    border-radius: var(--radius-pill);
    border: 1px solid var(--line);
    background: color-mix(in srgb, var(--panel-2) 78%, transparent);
    color: var(--muted);
    font-family: var(--font-ui);
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.02em;
    cursor: pointer;
    box-shadow: var(--accent-glow);
  }
  .ghost:hover,
  .ghost.active {
    color: var(--text-strong);
    border-color: var(--line-strong);
    background: color-mix(in srgb, var(--panel-raised) 92%, transparent);
    box-shadow: var(--shadow-sm);
  }
  .ghost.active {
    border-color: color-mix(in srgb, var(--accent-thread) 45%, var(--line-strong));
  }
  .console-body {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 0px;
    min-height: 0;
    transition: grid-template-columns var(--motion-med);
  }
  .console-body.semantic-open {
    grid-template-columns: minmax(0, 1fr) 340px;
    gap: var(--space-3);
  }
  .pane {
    display: grid;
    grid-template-rows: 42px minmax(0, 1fr);
    min-height: 0;
    border: 1px solid color-mix(in srgb, var(--line-strong) 75%, transparent);
    border-radius: var(--radius-lg);
    background: color-mix(in srgb, var(--panel) 90%, transparent);
    box-shadow: var(--shadow-sm);
    overflow: hidden;
  }
  .pane-head {
    display: flex;
    align-items: center;
    padding: 0 var(--space-4);
    border-bottom: 1px solid var(--line);
    background: color-mix(in srgb, var(--panel-2) 90%, transparent);
    color: var(--muted-2);
    font-size: 11px;
    letter-spacing: 0.04em;
  }
  .chat-body {
    min-height: 0;
  }
  .right-pane {
    overflow: hidden;
    transition:
      opacity var(--motion-med),
      transform var(--motion-med),
      width var(--motion-med);
  }
  .right-pane:not(.open) {
    opacity: 0;
    pointer-events: none;
    transform: translateX(12px);
  }
  .console-statusbar {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: 0 var(--space-4);
    border-top: 1px solid var(--line);
    background: color-mix(in srgb, var(--panel) 82%, transparent);
    color: var(--muted-2);
    font-size: 11px;
    overflow-x: auto;
    white-space: nowrap;
  }

  @media (max-width: 900px) {
    .console-topbar {
      flex-wrap: wrap;
      min-height: 52px;
      padding-block: var(--space-2);
    }
    .console-body.semantic-open {
      grid-template-columns: minmax(0, 1fr);
    }
    .right-pane.open {
      position: absolute;
      inset: 76px var(--space-3) 48px var(--space-3);
      z-index: 10;
      box-shadow: var(--shadow-lg);
    }
  }

  @media (max-width: 480px) {
    .console {
      grid-template-rows: auto minmax(0, 1fr) 40px;
    }
    .brand {
      width: 100%;
    }
    .persona-tag,
    .ghost {
      min-height: 40px;
    }
  }
</style>

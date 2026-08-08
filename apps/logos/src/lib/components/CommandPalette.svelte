<script lang="ts">
  import { commandPaletteOpen, currentPersona, inspectMode, telemetryOpen, semanticPaneOpen, operatorNotice, type Persona } from "$lib/ui";

  let isOpen = $state(false);
  let input = $state("");
  let feedback = $state("");

  const PERSONAS: Persona[] = ["operator", "analyst", "creative", "socratic", "archivist"];

  $effect(() => {
    const onToggle = () => {
      isOpen = !isOpen;
      commandPaletteOpen.set(isOpen);
      if (isOpen) feedback = "";
    };
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        onToggle();
      } else if (e.key === "/" && !isOpen && !(e.target instanceof HTMLInputElement)) {
        e.preventDefault();
        onToggle();
      } else if (e.key === "Escape" && isOpen) {
        onToggle();
      }
    };
    window.addEventListener("keydown", onKey);
    document.addEventListener("logos:palette", onToggle);
    return () => {
      window.removeEventListener("keydown", onKey);
      document.removeEventListener("logos:palette", onToggle);
    };
  });

  function runCommand() {
    const cmd = input.trim();
    feedback = "";
    if (!cmd) return;
    if (cmd.startsWith(":persona")) {
      const name = cmd.split(/\s+/)[1] as Persona | undefined;
      if (name && PERSONAS.includes(name)) {
        currentPersona.set(name);
        feedback = `persona → ${name}`;
      } else {
        feedback = `personas: ${PERSONAS.join(" | ")}`;
      }
    } else if (cmd === ":inspect") {
      inspectMode.set(!$inspectMode);
      feedback = `inspect ${$inspectMode ? "on" : "off"}`;
    } else if (cmd === ":telemetry") {
      telemetryOpen.set(!$telemetryOpen);
      feedback = `telemetry ${$telemetryOpen ? "on" : "off"}`;
    } else if (cmd === ":threads") {
      semanticPaneOpen.set(!$semanticPaneOpen);
      feedback = `semantic threads ${$semanticPaneOpen ? "on" : "off"}`;
    } else if (cmd === ":focus") {
      operatorNotice.set("Focus returned to the active stream.");
      feedback = "focus → stream";
    } else if (cmd === ":memory") {
      semanticPaneOpen.set(true);
      feedback = "semantic pane → memory workflow";
    } else if (cmd === ":help") {
      feedback =
        "commands: :persona <name> | :inspect | :telemetry | :threads | :memory | :focus";
    } else {
      feedback =
        `unknown: ${cmd} — try :persona analyst | :inspect | :telemetry | :threads | :memory | :help`;
    }
    input = "";
  }
</script>

{#if isOpen}
  <div class="palette-overlay" role="dialog" aria-label="command palette" tabindex="-1" on:click|stopPropagation>
    <div class="palette-bar logos-palette">
      <span class="prompt">&gt;</span>
      <input
        bind:value={input}
        placeholder=":persona analyst | :inspect | :telemetry | :threads | :memory"
        on:keydown={(e) => {
          if (e.key === "Enter") runCommand();
        }}
        autofocus
      />
    </div>
    {#if feedback}
      <div class="palette-feedback">{feedback}</div>
    {/if}
  </div>
{/if}

<style>
  .palette-overlay {
    position: fixed;
    inset: 0;
    z-index: 100;
    display: grid;
    align-content: start;
    justify-items: center;
    padding: 80px var(--space-4) 0;
    background: rgba(8, 10, 14, 0.56);
    backdrop-filter: blur(12px);
  }
  .palette-bar {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: min(760px, 100%);
    padding: 14px 16px;
    border: 1px solid color-mix(in srgb, var(--line-strong) 78%, transparent);
    border-radius: var(--radius-xl);
    background: color-mix(in srgb, var(--panel-raised) 92%, transparent);
    box-shadow: var(--shadow-lg);
  }
  .prompt {
    color: var(--accent-thread);
    font-size: 16px;
  }
  input {
    flex: 1;
    background: transparent;
    border: none;
    outline: none;
    color: var(--text-strong);
    font-family: var(--font-body);
    font-size: 15px;
  }
  .palette-feedback {
    width: min(760px, 100%);
    margin-top: var(--space-2);
    padding: 10px 14px;
    border: 1px solid color-mix(in srgb, var(--line) 75%, transparent);
    border-radius: var(--radius-lg);
    background: color-mix(in srgb, var(--panel) 88%, transparent);
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--accent-verified);
    box-shadow: var(--shadow-sm);
  }
</style>

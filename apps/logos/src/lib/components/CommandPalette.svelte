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
  <div class="palette-overlay" role="dialog" aria-label="command palette" on:click|stopPropagation>
    <div class="palette-bar logos-palette">
      <span class="prompt">&gt;</span>
      <input
        bind:value={input}
        placeholder=":persona analyst | :inspect | :telemetry | :threads"
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
    top: 0;
    left: 48px;
    right: 0;
    z-index: 100;
    background: var(--bg);
    border-bottom: 1px solid var(--line-strong);
  }
  .palette-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 14px;
    border-bottom: 1px solid var(--line);
  }
  .prompt {
    color: var(--accent-thread);
  }
  input {
    flex: 1;
    background: transparent;
    border: none;
    outline: none;
    color: var(--text-strong);
    font-family: var(--font-mono);
    font-size: 13px;
  }
  .palette-feedback {
    padding: 8px 14px;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--accent-verified);
  }
</style>

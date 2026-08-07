<script lang="ts">
  import { onMount, tick } from 'svelte';
  import {
    commandPaletteOpen,
    currentPersona,
    inspectMode,
    type Persona,
  } from '../lib/ui';

  let inputElement: HTMLInputElement | null = null;
  let paletteValue = '';
  let feedbackText = '';
  let feedbackTimeout: ReturnType<typeof setTimeout> | undefined;

  // Recognized commands
  const PERSONAS: Persona[] = ['operator', 'analyst', 'creative', 'socratic', 'archivist'];
  const COMMANDS = [
    ':persona',
    ':inspect',
    ':transform',
    ':thread',
  ] as const;

  function open() {
    paletteValue = '';
    feedbackText = '';
    commandPaletteOpen.set(true);
    void tick().then(() => inputElement?.focus());
  }

  function close() {
    paletteValue = '';
    feedbackText = '';
    commandPaletteOpen.set(false);
    if (feedbackTimeout) clearTimeout(feedbackTimeout);
  }

  function showFeedback(text: string, durationMs = 1800) {
    feedbackText = text;
    if (feedbackTimeout) clearTimeout(feedbackTimeout);
    feedbackTimeout = setTimeout(() => {
      feedbackText = '';
    }, durationMs);
  }

  function execute(command: string) {
    const trimmed = command.trim();
    if (!trimmed.startsWith(':')) return;

    const parts = trimmed.split(/\s+/);
    const cmd = parts[0].toLowerCase();
    const arg = parts.slice(1).join(' ').toLowerCase();

    switch (cmd) {
      case ':persona': {
        if (!arg) {
          showFeedback(`PERSONAS: ${PERSONAS.join(' | ')}`);
          return;
        }
        const persona = PERSONAS.find((p) => p === arg);
        if (persona) {
          currentPersona.set(persona);
          showFeedback(`PERSONA → ${persona.toUpperCase()}`);
        } else {
          showFeedback(`UNKNOWN PERSONA: "${arg}" — valid: ${PERSONAS.join(' | ')}`);
        }
        return;
      }
      case ':inspect': {
        inspectMode.update((v) => !v);
        const state = !isInspectModeBeforeToggle;
        showFeedback(`INSPECT MODE: ${state ? 'ON' : 'OFF'}`);
        return;
      }
      case ':transform': {
        if (!arg) {
          showFeedback('TRANSFORM ops: condense | expand | rewrite | mem-node');
          return;
        }
        showFeedback(`TRANSFORM → ${arg.toUpperCase()} (Track B)`, 2400);
        return;
      }
      case ':thread': {
        if (!arg) {
          showFeedback('USAGE: :thread <id>');
          return;
        }
        showFeedback(`THREAD → ${arg} (Track B)`, 2400);
        return;
      }
      default:
        showFeedback(`UNKNOWN: ${cmd} — try :persona, :inspect, :transform, :thread`);
    }
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.preventDefault();
      close();
      return;
    }
    if (event.key === 'Enter') {
      event.preventDefault();
      execute(paletteValue);
      paletteValue = '';
      return;
    }
    if (event.key === 'Backspace' && paletteValue === '' && event.repeat) {
      // Don't close on repeated backspace when empty
      return;
    }
  }

  // Snapshot inspectMode before toggling (for feedback)
  let isInspectModeBeforeToggle = false;
  inspectMode.subscribe((v) => (isInspectModeBeforeToggle = v));

  onMount(() => {
    return () => {
      if (feedbackTimeout) clearTimeout(feedbackTimeout);
    };
  });
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="command-palette"
  class:open={$commandPaletteOpen}
  role="region"
  aria-label="Command palette"
  onkeydown={(e) => {
    if (e.key === 'Escape' && $commandPaletteOpen) {
      e.stopPropagation();
      close();
    }
  }}
>
  <div class="command-palette__bar">
    <span class="command-palette__prompt" aria-hidden="true">&gt;</span>
    <input
      bind:this={inputElement}
      bind:value={paletteValue}
      type="text"
      class="command-palette__input"
      placeholder=":persona operator  |  :inspect  |  :transform condense  |  :thread #id"
      onkeydown={handleKeydown}
      onblur={() => {
        // Small delay so click-on-option works
        setTimeout(() => {
          if ($commandPaletteOpen) close();
        }, 150);
      }}
      aria-label="Command input"
      spellcheck="false"
      autocomplete="off"
    />
    <kbd class="command-palette__esc" aria-hidden="true">ESC</kbd>
  </div>
  {#if feedbackText}
    <div class="command-palette__feedback" role="status" aria-live="polite">
      {feedbackText}
    </div>
  {/if}
</div>

<style>
  .command-palette {
    position: absolute;
    bottom: 100%;
    left: 0;
    right: 0;
    z-index: 45;
    background: var(--panel);
    border: 1px solid var(--line-strong);
    border-bottom: 1px solid var(--text-strong);
    font-family: var(--font-mono);
    transform: translateY(8px);
    opacity: 0;
    pointer-events: none;
    transition: opacity var(--motion-fast), transform var(--motion-fast);
  }

  .command-palette.open {
    opacity: 1;
    pointer-events: auto;
    transform: translateY(0);
  }

  .command-palette__bar {
    display: flex;
    align-items: center;
    height: 40px;
    padding: 0 10px;
    gap: 8px;
    background: var(--panel-2);
  }

  .command-palette__prompt {
    flex: 0 0 auto;
    color: var(--accent-verified);
    font-weight: 700;
    font-size: 16px;
    margin-right: 2px;
  }

  .command-palette__input {
    flex: 1;
    min-width: 0;
    height: 100%;
    background: transparent;
    border: 0;
    outline: 0;
    color: var(--text-strong);
    font-family: var(--font-mono);
    font-size: 13px;
    letter-spacing: 0.02em;
  }

  .command-palette__input::placeholder {
    color: var(--muted-2);
    font-family: var(--font-mono);
    font-size: 11px;
  }

  .command-palette__esc {
    flex: 0 0 auto;
    display: inline-flex;
    align-items: center;
    height: 20px;
    padding: 0 5px;
    background: var(--panel-3);
    border: 1px solid var(--line-strong);
    color: var(--muted-2);
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.06em;
  }

  .command-palette__feedback {
    padding: 8px 14px;
    color: var(--text);
    font-family: var(--font-mono);
    font-size: 12px;
    letter-spacing: 0.02em;
    border-top: 1px solid var(--line);
    background: var(--panel);
  }
</style>

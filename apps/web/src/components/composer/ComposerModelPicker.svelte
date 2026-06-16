<script lang="ts">
  // Composer-anchored model picker. Lives at the lower-right of the composer
  // glyph row and opens the Codex-style cascading picker upward (providers →
  // models, reasoning, speed) with a "Reset to default" footer. This is the
  // single interactive surface for per-session model overrides; the topbar's
  // read-only Provider/Model/Runtime pills were retired in favor of this.
  //
  // Self-contained: it reads the channel runtime store directly so it stays
  // reactive on channel switch, and only takes
  // the override handlers as props. The parent decides when to mount it
  // (live/structured channels only, where an override actually takes effect).
  import {
    prettyModelLabel,
    prettyProviderLabel,
    resolveChannelRuntime,
  } from "../../lib/chat/channel-runtime";
  import { channelRuntime } from "../../lib/chat/channel-runtime-store.svelte";
  import ModelPickerMenu from "./ModelPickerMenu.svelte";

  type Props = {
    onModel: (model: string | undefined) => void;
    onProvider: (provider: string | undefined) => void;
    onThinking: (mode: string | undefined) => void;
    onFast: (fast: boolean) => void;
    onRuntime: (runtime: string | undefined) => void;
    onResetOverrides: () => void;
  };

  let { onModel, onProvider, onThinking, onFast, onRuntime, onResetOverrides }: Props = $props();

  // Read the store inside $derived so the pill re-resolves on channel switch.
  let resolved = $derived(resolveChannelRuntime(channelRuntime.runtime));
  let pickerOpen = $state(false);
  // Trigger pill ref, handed to the menu so it can anchor itself with fixed
  // positioning and break out of the composer card's overflow clip.
  let pillEl: HTMLButtonElement | null = $state(null);

  // Pretty provider + model labels, matching the picker menu (e.g. "Pioneer" /
  // "Claude Opus 4.8") rather than the raw slug, so the composer pill reads the
  // same as the menu it opens.
  let providerLabel = $derived(prettyProviderLabel(resolved.provider.effective));
  let modelLabel = $derived(
    prettyModelLabel(resolved.provider.effective, resolved.model.effective) || "default",
  );
  let anyOverride = $derived(
    resolved.model.overridden || resolved.thinking.overridden || resolved.provider.overridden,
  );
</script>

<span class="cmp-anchor">
  <button
    type="button"
    class="cmp-pill"
    class:is-match={!anyOverride}
    class:is-drift={anyOverride}
    class:is-open={pickerOpen}
    bind:this={pillEl}
    onclick={() => (pickerOpen = !pickerOpen)}
    aria-haspopup="menu"
    aria-expanded={pickerOpen}
    title={`Model: ${resolved.model.effective || "default"} · Thinking: ${resolved.thinking.effective} · Speed: ${resolved.fast ? "fast" : "normal"}${anyOverride ? " (session override)" : ""} — click to change`}
  >
    <span class="cmp-dot" aria-hidden="true"></span>
    {#if providerLabel}
      <span class="cmp-provider">{providerLabel}</span>
      <span class="cmp-slash" aria-hidden="true">/</span>
    {/if}
    <span class="cmp-current">{modelLabel}</span>
    <span class="cmp-think">{resolved.thinking.effective}</span>
    {#if resolved.fast}<span class="cmp-fast">fast</span>{/if}
    {#if anyOverride}<span class="cmp-star" aria-hidden="true">*</span>{/if}
    <span class="cmp-caret" aria-hidden="true">▾</span>
  </button>

  {#if pickerOpen}
    <ModelPickerMenu
      {resolved}
      placement="up"
      anchor={pillEl}
      onModel={(v) => onModel(v)}
      onProvider={(v) => onProvider(v)}
      onThinking={(v) => onThinking(v)}
      onFast={(v) => onFast(v)}
      onRuntime={(v) => onRuntime(v)}
      onResetDefault={() => onResetOverrides()}
      onClose={() => (pickerOpen = false)}
    />
  {/if}
</span>

<style>
  .cmp-anchor {
    position: relative;
    display: inline-flex;
    flex-shrink: 0;
  }

  .cmp-pill {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 4px 10px;
    /* Squared to match the composer card's shape language (flat sides, sharp
       rounded corners) rather than a fully-rounded capsule. */
    border-radius: var(--radius);
    border: 1px solid transparent;
    background: transparent;
    color: var(--muted);
    font-size: 12.5px;
    font-weight: 600;
    line-height: 1;
    white-space: nowrap;
    cursor: pointer;
    font-family: inherit;
    opacity: 0.82;
    transition: border-color 120ms ease, background 120ms ease, opacity 120ms ease;
  }

  .cmp-pill:hover,
  .cmp-pill.is-open {
    background: var(--hover-strong);
    border-color: var(--line);
    opacity: 1;
  }

  .cmp-provider {
    color: var(--muted);
    font-weight: 600;
  }

  .cmp-slash {
    color: var(--muted-2);
    font-weight: 600;
  }

  .cmp-current {
    color: var(--text);
    font-weight: 700;
  }

  .cmp-think {
    color: var(--muted);
    font-weight: 600;
    font-family: "Geist Mono Variable", ui-monospace, SFMono-Regular, monospace;
  }

  .cmp-fast {
    color: var(--danger);
    font-weight: 700;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .cmp-star {
    color: var(--warn);
    font-size: 10.5px;
  }

  .cmp-caret {
    color: var(--muted);
    font-size: 9.5px;
  }

  /* State lives on the dot only, so the pill stays embedded in the glyph row. */
  .cmp-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--muted);
  }

  .cmp-pill.is-match .cmp-dot {
    background: var(--success, #22c55e);
  }

  .cmp-pill.is-drift .cmp-dot {
    background: var(--warn);
  }

  /* Phone: drop the provider prefix and the thinking/speed labels so the pill
     shrinks to the dot + model name + caret and stays on the runtime row next
     to the context meter. Full provider/thinking detail is in the picker menu.
     The model name itself truncates rather than pushing the row wider. */
  @media (max-width: 520px) {
    .cmp-provider,
    .cmp-slash,
    .cmp-think,
    .cmp-fast {
      display: none;
    }

    .cmp-current {
      max-width: 16ch;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
  }
</style>

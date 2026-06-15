<script lang="ts">
  // Codex-style cascading model picker. Anchored above the composer's model
  // pill (lower-right of the glyph row). Structure (per operator spec):
  //   1. Provider section: one row per provider, models in a side popover.
  //   2. Reasoning row: thinking modes in a side popover.
  //   3. Speed row: normal/fast in a side popover.
  //   4. Footer: "Reset to default".
  // Checkmarks (✓) mark the effective selection at each level. The agent
  // default model is shaded light-green in the model submenu so the operator
  // can see at a glance which model is the channel default vs a session pick.
  // Chevrons mark rows that open a side popover. One submenu opens at a time.
  import {
    PROVIDER_CHOICES,
    PROVIDER_MODELS,
    RUNTIME_CHOICES,
    THINKING_MODES,
    providerForModel,
    runtimeChoiceLabel,
    shortModel,
    type ResolvedChannelRuntime,
  } from "../../lib/chat/channel-runtime";

  type Props = {
    resolved: ResolvedChannelRuntime;
    // Placement relative to the trigger. "up" anchors the menu above the
    // trigger (composer use); "down" anchors below (legacy topbar use).
    placement?: "up" | "down";
    onModel: (model: string | undefined) => void;
    onProvider: (provider: string | undefined) => void;
    onThinking: (mode: string | undefined) => void;
    onFast: (fast: boolean) => void;
    // Pin the runtime engine (auto|pi|codex).
    onRuntime: (runtime: string | undefined) => void;
    // Reset all session overrides back to the channel/agent default.
    onResetDefault: () => void;
    onClose: () => void;
  };

  let {
    resolved,
    placement = "up",
    onModel,
    onProvider,
    onThinking,
    onFast,
    onRuntime,
    onResetDefault,
    onClose,
  }: Props = $props();

  // Which submenu is open: a provider key, "reasoning", "speed", or null.
  let openSub = $state<string | null>(null);
  // The picker menu element and the live submenu flyout, used to center the
  // flyout on its trigger row and clamp it inside the viewport.
  let menuEl = $state<HTMLElement | null>(null);
  let subEl = $state<HTMLElement | null>(null);
  // Vertical center (px, relative to the menu) of the row that opened the
  // submenu. The flyout is centered on this point so it reads as attached to
  // the row the pointer is on, instead of pinned to the menu's bottom edge.
  let subCenter = $state(0);
  // When centering would push the flyout past a viewport edge, this holds an
  // explicit clamped top (px, menu-relative); null means "use centered".
  let subClampTop = $state<number | null>(null);

  const effectiveProvider = $derived(
    (resolved.provider.effective || "").toLowerCase() || providerForModel(resolved.model.effective),
  );
  // The channel default provider, used to shade the default provider+model green.
  const defaultProvider = $derived((resolved.provider.agent || "").toLowerCase());

  // Whether the effective model already is the channel default (disables Reset).
  const onDefaultAlready = $derived(!resolved.model.overridden && !resolved.provider.overridden);

  // Current runtime pin (auto|pi|codex); "auto" when nothing is pinned. The row
  // hint shows the pinned engine when set, else the derived effective engine.
  const currentRuntime = $derived(resolved.runtimeOverride || "auto");
  const runtimeRowHint = $derived(
    resolved.runtimeOverride ? runtimeChoiceLabel(resolved.runtimeOverride) : resolved.runtime.effective,
  );

  // Selection + default are matched on the full provider+model slug, not a loose
  // model-name match. A provider that re-hosts another vendor's model (e.g.
  // Pioneer carrying Claude/GPT slugs) must not light up as selected/default for
  // a pick that belongs to a different provider.
  function isCurrentModel(provider: string, value: string): boolean {
    const m = resolved.model.effective;
    if (!m || provider !== effectiveProvider) return false;
    return shortModel(value) === m || value.endsWith(`/${m}`) || value === m;
  }

  function isDefaultModel(provider: string, value: string): boolean {
    const m = resolved.model.agent;
    if (!m || provider !== defaultProvider) return false;
    return shortModel(value) === m || value.endsWith(`/${m}`) || value === m;
  }

  function pickModel(provider: string, value: string) {
    onProvider(provider);
    onModel(value);
    onClose();
  }

  function pickThinking(mode: string | undefined) {
    onThinking(mode);
    onClose();
  }

  function pickSpeed(fast: boolean) {
    onFast(fast);
    onClose();
  }

  function pickRuntime(value: string) {
    onRuntime(value);
    onClose();
  }

  function openSubmenu(key: string, ev?: Event) {
    const btn = ev?.currentTarget as HTMLElement | null;
    // offsetTop is relative to the positioned menu (offsetParent), so this is
    // the trigger row's vertical center in the same coordinate space the
    // flyout's `top` uses.
    if (btn) subCenter = btn.offsetTop + btn.offsetHeight / 2;
    // Re-measure clamping for the new anchor from a clean centered baseline.
    subClampTop = null;
    openSub = key;
  }

  function handleReset() {
    onResetDefault();
    onClose();
  }

  // After the flyout renders centered on its row, measure it and switch to an
  // explicit clamped top only if the centered position overflows the viewport.
  // No overflow -> stays purely CSS-centered (no reflow, no flash). subClampTop
  // is not read here, so setting it does not re-trigger this effect.
  $effect(() => {
    const sub = openSub;
    void subCenter;
    if (!sub || !subEl || !menuEl) return;
    const margin = 8;
    const rect = subEl.getBoundingClientRect();
    const menuTop = menuEl.getBoundingClientRect().top;
    if (rect.top < margin) {
      subClampTop = margin - menuTop;
    } else if (rect.bottom > window.innerHeight - margin) {
      subClampTop = window.innerHeight - margin - rect.height - menuTop;
    }
  });
</script>

<svelte:window onkeydown={(e) => e.key === "Escape" && onClose()} />

<button class="mp-scrim" aria-label="Close picker" onclick={onClose}></button>

<div class="mp-menu" class:place-up={placement === "up"} role="menu" aria-label="Model picker" bind:this={menuEl}>
  <div class="mp-section-label">Provider</div>

  {#each PROVIDER_CHOICES as choice (choice.value)}
    <button
      class="mp-row"
      class:is-open={openSub === choice.value}
      class:is-default={choice.value === defaultProvider}
      role="menuitem"
      aria-haspopup="menu"
      aria-expanded={openSub === choice.value}
      onmouseenter={(e) => openSubmenu(choice.value, e)}
      onclick={(e) => openSubmenu(choice.value, e)}
    >
      <span class="mp-row-label">{choice.label}</span>
      <span class="mp-row-end">
        {#if choice.value === defaultProvider}<span class="mp-default-tag">default</span>{/if}
        {#if effectiveProvider === choice.value}<span class="mp-check" aria-hidden="true">✓</span>{/if}
        <span class="mp-chevron" aria-hidden="true">‹</span>
      </span>
    </button>
  {/each}

  <div class="mp-sep" role="separator"></div>

  <button
    class="mp-row"
    class:is-open={openSub === "reasoning"}
    role="menuitem"
    aria-haspopup="menu"
    aria-expanded={openSub === "reasoning"}
    onmouseenter={(e) => openSubmenu("reasoning", e)}
    onclick={(e) => openSubmenu("reasoning", e)}
  >
    <span class="mp-row-label">Reasoning</span>
    <span class="mp-row-end">
      <span class="mp-current">{resolved.thinking.effective}</span>
      <span class="mp-chevron" aria-hidden="true">‹</span>
    </span>
  </button>

  <button
    class="mp-row"
    class:is-open={openSub === "speed"}
    role="menuitem"
    aria-haspopup="menu"
    aria-expanded={openSub === "speed"}
    onmouseenter={(e) => openSubmenu("speed", e)}
    onclick={(e) => openSubmenu("speed", e)}
  >
    <span class="mp-row-label">Speed</span>
    <span class="mp-row-end">
      <span class="mp-current">{resolved.fast ? "fast" : "normal"}</span>
      <span class="mp-chevron" aria-hidden="true">‹</span>
    </span>
  </button>

  <button
    class="mp-row"
    class:is-open={openSub === "runtime"}
    role="menuitem"
    aria-haspopup="menu"
    aria-expanded={openSub === "runtime"}
    onmouseenter={(e) => openSubmenu("runtime", e)}
    onclick={(e) => openSubmenu("runtime", e)}
  >
    <span class="mp-row-label">Runtime</span>
    <span class="mp-row-end">
      <span class="mp-current">{runtimeRowHint}</span>
      <span class="mp-chevron" aria-hidden="true">‹</span>
    </span>
  </button>

  <div class="mp-sep" role="separator"></div>

  <div class="mp-footer">
    <button
      class="mp-action"
      role="menuitem"
      disabled={onDefaultAlready}
      title={onDefaultAlready ? "Already on the channel default" : "Clear session overrides and return to the channel default"}
      onclick={handleReset}
    >
      <svg viewBox="0 0 24 24" width="13" height="13" aria-hidden="true">
        <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M3 12a9 9 0 1 0 3-6.7L3 8m0-5v5h5"/>
      </svg>
      <span>Reset to default</span>
    </button>
  </div>

  {#if openSub && PROVIDER_MODELS[openSub]}
    <div
      class="mp-sub"
      bind:this={subEl}
      style={`--sub-pos:${subClampTop != null ? `${subClampTop}px` : `${subCenter}px`}; --sub-tf:${subClampTop != null ? "none" : "translateY(-50%)"}`}
      role="menu"
      aria-label="Models"
    >
      <div class="mp-section-label">Model</div>
      {#each PROVIDER_MODELS[openSub].models as model (model.value)}
        <button
          class="mp-row"
          class:is-default={isDefaultModel(openSub, model.value)}
          role="menuitemradio"
          aria-checked={isCurrentModel(openSub, model.value)}
          onclick={() => pickModel(openSub!, model.value)}
        >
          <span class="mp-row-label">{model.label}</span>
          <span class="mp-row-end">
            {#if isDefaultModel(openSub, model.value)}<span class="mp-default-tag">default</span>{/if}
            {#if isCurrentModel(openSub, model.value)}<span class="mp-check" aria-hidden="true">✓</span>{/if}
          </span>
        </button>
      {/each}
    </div>
  {:else if openSub === "reasoning"}
    <div
      class="mp-sub"
      bind:this={subEl}
      style={`--sub-pos:${subClampTop != null ? `${subClampTop}px` : `${subCenter}px`}; --sub-tf:${subClampTop != null ? "none" : "translateY(-50%)"}`}
      role="menu"
      aria-label="Reasoning modes"
    >
      <div class="mp-section-label">Reasoning</div>
      <button class="mp-row" role="menuitemradio" aria-checked={!resolved.thinking.overridden} onclick={() => pickThinking(undefined)}>
        <span class="mp-row-label">Agent default ({resolved.thinking.agent})</span>
        <span class="mp-row-end">
          {#if !resolved.thinking.overridden}<span class="mp-check" aria-hidden="true">✓</span>{/if}
        </span>
      </button>
      {#each THINKING_MODES as mode (mode)}
        <button class="mp-row" role="menuitemradio" aria-checked={resolved.thinking.overridden && resolved.thinking.effective === mode} onclick={() => pickThinking(mode)}>
          <span class="mp-row-label">{mode}</span>
          <span class="mp-row-end">
            {#if resolved.thinking.overridden && resolved.thinking.effective === mode}<span class="mp-check" aria-hidden="true">✓</span>{/if}
          </span>
        </button>
      {/each}
    </div>
  {:else if openSub === "speed"}
    <div
      class="mp-sub"
      bind:this={subEl}
      style={`--sub-pos:${subClampTop != null ? `${subClampTop}px` : `${subCenter}px`}; --sub-tf:${subClampTop != null ? "none" : "translateY(-50%)"}`}
      role="menu"
      aria-label="Speed"
    >
      <div class="mp-section-label">Speed</div>
      <button class="mp-row" role="menuitemradio" aria-checked={!resolved.fast} onclick={() => pickSpeed(false)}>
        <span class="mp-row-label">normal</span>
        <span class="mp-row-end">
          {#if !resolved.fast}<span class="mp-check" aria-hidden="true">✓</span>{/if}
        </span>
      </button>
      <button class="mp-row" role="menuitemradio" aria-checked={resolved.fast} onclick={() => pickSpeed(true)}>
        <span class="mp-row-label">fast <span class="mp-hint">priority processing</span></span>
        <span class="mp-row-end">
          {#if resolved.fast}<span class="mp-check" aria-hidden="true">✓</span>{/if}
        </span>
      </button>
    </div>
  {:else if openSub === "runtime"}
    <div
      class="mp-sub"
      bind:this={subEl}
      style={`--sub-pos:${subClampTop != null ? `${subClampTop}px` : `${subCenter}px`}; --sub-tf:${subClampTop != null ? "none" : "translateY(-50%)"}`}
      role="menu"
      aria-label="Runtime"
    >
      <div class="mp-section-label">Runtime</div>
      {#each RUNTIME_CHOICES as rt (rt.value)}
        <button
          class="mp-row"
          role="menuitemradio"
          aria-checked={currentRuntime === rt.value}
          onclick={() => pickRuntime(rt.value)}
        >
          <span class="mp-row-label">{rt.label}{#if rt.value === "auto"} <span class="mp-hint">picks by model</span>{/if}</span>
          <span class="mp-row-end">
            {#if currentRuntime === rt.value}<span class="mp-check" aria-hidden="true">✓</span>{/if}
          </span>
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .mp-scrim {
    position: fixed;
    inset: 0;
    z-index: 40;
    background: transparent;
    border: 0;
    cursor: default;
  }

  .mp-menu {
    position: absolute;
    top: calc(100% + 6px);
    right: 0;
    z-index: 41;
    width: 220px;
    padding: 6px;
    border-radius: 10px;
    border: 1px solid var(--line-strong);
    background: var(--ic-panel-solid, #1d1e22);
    box-shadow: 0 18px 50px -18px rgba(0, 0, 0, 0.6);
  }

  /* Composer placement: open upward from the trigger pill. */
  .mp-menu.place-up {
    top: auto;
    bottom: calc(100% + 6px);
    box-shadow: 0 -18px 50px -18px rgba(0, 0, 0, 0.6);
  }

  .mp-section-label {
    padding: 4px 8px 3px;
    font-size: 10px;
    font-weight: 600;
    color: var(--muted-2);
    text-transform: none;
  }

  .mp-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    width: 100%;
    height: 28px;
    padding: 0 8px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--text);
    font-size: 12px;
    font-family: inherit;
    cursor: pointer;
    text-align: left;
    white-space: nowrap;
  }

  .mp-row:hover,
  .mp-row.is-open {
    background: var(--panel-2);
  }

  /* Light-green shading on the agent default model row. */
  .mp-row.is-default {
    background: color-mix(in srgb, var(--success, #22c55e) 14%, transparent);
  }

  .mp-row.is-default:hover {
    background: color-mix(in srgb, var(--success, #22c55e) 22%, transparent);
  }

  .mp-row.is-default .mp-row-label {
    color: var(--success, #22c55e);
  }

  .mp-default-tag {
    font-size: 9px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--success, #22c55e);
    background: color-mix(in srgb, var(--success, #22c55e) 18%, transparent);
    padding: 1px 5px;
    border-radius: 999px;
  }

  .mp-row-label {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .mp-row-end {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
  }

  .mp-current {
    color: var(--muted);
    font-size: 11px;
    font-family: "Geist Mono Variable", ui-monospace, SFMono-Regular, monospace;
  }

  .mp-check {
    color: var(--text);
    font-size: 11px;
  }

  .mp-chevron {
    color: var(--muted);
    font-size: 13px;
    line-height: 1;
  }

  .mp-hint {
    color: var(--muted-2);
    font-size: 10px;
  }

  .mp-sep {
    height: 1px;
    margin: 5px 4px;
    background: var(--line);
  }

  .mp-footer {
    display: grid;
    gap: 3px;
    padding: 2px 0 0;
  }

  .mp-action {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    width: 100%;
    height: 30px;
    padding: 0 8px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--text);
    font-size: 12px;
    font-family: inherit;
    font-weight: 600;
    cursor: pointer;
    text-align: left;
  }

  .mp-action svg {
    flex-shrink: 0;
    color: var(--muted);
  }

  .mp-action:hover:not(:disabled) {
    background: var(--panel-2);
  }

  .mp-action:disabled {
    opacity: 0.4;
    cursor: default;
  }

  .mp-sub {
    position: absolute;
    left: auto;
    right: calc(100% + 4px);
    /* Centered on the trigger row (--sub-pos = row center, --sub-tf pulls the
       flyout up by half its height). When the centered position would clip a
       viewport edge, the component sets --sub-pos to a clamped top and --sub-tf
       to none. Driven via custom props so the mobile rule below still wins. */
    top: var(--sub-pos, 0px);
    transform: var(--sub-tf, none);
    z-index: 42;
    min-width: 200px;
    max-height: 320px;
    overflow-y: auto;
    padding: 6px;
    border-radius: 10px;
    border: 1px solid var(--line-strong);
    background: var(--ic-panel-solid, #1d1e22);
    box-shadow: 0 18px 50px -18px rgba(0, 0, 0, 0.6);
  }

  /* Mobile: side popovers do not fit next to the menu; stack over the parent. */
  @media (max-width: 820px) {
    .mp-menu {
      width: 240px;
    }

    .mp-sub {
      left: 12px;
      right: 12px;
      top: auto;
      bottom: 48px;
      transform: none;
      min-width: 0;
    }
  }
</style>

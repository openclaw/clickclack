<script lang="ts">
  import type { BotToken } from "../../../lib/bots";
  import {
    buildOpenClawConfigSnippet,
    buildOpenClawShellSnippet,
    type OpenClawAccountMode,
  } from "../../../lib/bots";

  type Props = {
    token: BotToken;
    botHandle: string;
    botUserID: string;
    workspaceRouteID: string;
    onDismiss: () => void;
  };

  let { token, botHandle, botUserID, workspaceRouteID, onDismiss }: Props = $props();

  let acknowledged = $state(false);
  let mode = $state<OpenClawAccountMode>("single");
  let copied = $state<"token" | "config" | "shell" | null>(null);

  const configSnippet = $derived(
    buildOpenClawConfigSnippet({
      workspaceRouteID,
      botHandle,
      botUserID,
      mode,
    }),
  );
  const shellSnippet = $derived(
    buildOpenClawShellSnippet({
      botHandle,
      token: token.token ?? "",
      mode,
    }),
  );

  async function copyTo(value: string, kind: "token" | "config" | "shell") {
    try {
      await navigator.clipboard.writeText(value);
      copied = kind;
      setTimeout(() => {
        if (copied === kind) copied = null;
      }, 1800);
    } catch {
      // Clipboard may be blocked; the value is still visible in the input.
    }
  }
</script>

<section class="ws-bots__reveal" aria-live="polite">
  <header class="ws-bots__reveal-header">
    <div>
      <h3 class="ws-bots__reveal-title">Your new token is ready</h3>
      <p class="ws-bots__reveal-hint">
        Copy it now. ClickClack stores only a hash, so this is the last time the raw token is visible.
        If you lose it, mint a new one and revoke this one.
      </p>
    </div>
  </header>

  <div class="ws-bots__reveal-field">
    <label class="ws-bots__reveal-label" for="ws-bots-reveal-token">Token</label>
    <div class="ws-bots__reveal-row">
      <input
        id="ws-bots-reveal-token"
        class="ws-bots__reveal-input"
        type="text"
        readonly
        value={token.token ?? ""}
      />
      <button
        type="button"
        class="ws-btn ws-btn--primary"
        onclick={() => copyTo(token.token ?? "", "token")}
      >
        {copied === "token" ? "Copied" : "Copy"}
      </button>
    </div>
  </div>

  <div class="ws-bots__reveal-field">
    <span class="ws-bots__reveal-label">OpenClaw account shape</span>
    <div class="ws-bots__setup-mode" role="group" aria-label="OpenClaw account shape">
      <button
        type="button"
        class:is-active={mode === "single"}
        onclick={() => (mode = "single")}
      >
        Single bot
      </button>
      <button
        type="button"
        class:is-active={mode === "named"}
        onclick={() => (mode = "named")}
      >
        Named account
      </button>
    </div>
  </div>

  <div class="ws-bots__reveal-field">
    <div class="ws-bots__reveal-snippet-header">
      <span class="ws-bots__reveal-label">OpenClaw config</span>
      <button
        type="button"
        class="ws-btn"
        onclick={() => copyTo(configSnippet, "config")}
      >
        {copied === "config" ? "Copied" : "Copy config"}
      </button>
    </div>
    <pre class="ws-bots__reveal-snippet"><code>{configSnippet}</code></pre>
  </div>

  <div class="ws-bots__reveal-field">
    <div class="ws-bots__reveal-snippet-header">
      <span class="ws-bots__reveal-label">Export and start</span>
      <button
        type="button"
        class="ws-btn"
        onclick={() => copyTo(shellSnippet, "shell")}
      >
        {copied === "shell" ? "Copied" : "Copy commands"}
      </button>
    </div>
    <pre class="ws-bots__reveal-snippet"><code>{shellSnippet}</code></pre>
  </div>

  {#if token.scopes?.length}
    <div class="ws-bots__reveal-field">
      <span class="ws-bots__reveal-label">Scopes</span>
      <div class="ws-bots__scope-row">
        {#each token.scopes as scope (scope)}
          <span class="ws-bots__scope-chip">{scope}</span>
        {/each}
      </div>
    </div>
  {/if}

  <label class="ws-bots__reveal-ack">
    <input type="checkbox" bind:checked={acknowledged} />
    <span>I've copied this token somewhere safe.</span>
  </label>

  <div class="ws-bots__reveal-actions">
    <button type="button" class="ws-btn ws-btn--primary" disabled={!acknowledged} onclick={onDismiss}>
      Done
    </button>
  </div>
</section>

<script lang="ts">
  import {
    BOT_SCOPE_BUNDLES,
    botLoadErrorMessage,
    createWorkspaceBotToken,
    type BotScopeBundle,
    type BotToken,
  } from "../../../lib/bots";

  type Props = {
    workspaceID: string;
    botUserID: string;
    // "token" mints a raw token immediately; "code" defers to a one-time
    // setup code that mints the token when claimed.
    mode?: "token" | "code";
    onCreated: (token: BotToken) => void;
    onCode?: (name: string, scopes: string[]) => void;
    onCancel: () => void;
  };

  let { workspaceID, botUserID, mode = "token", onCreated, onCode, onCancel }: Props = $props();

  let tokenName = $state("");
  let selectedScope = $state<BotScopeBundle>("bot:write");
  let submitting = $state(false);
  let error = $state("");

  const canSubmit = $derived(!submitting && tokenName.trim().length > 0);

  async function submit(event: Event) {
    event.preventDefault();
    if (!canSubmit) return;
    if (mode === "code") {
      // The reveal panel mints the code itself; just hand over the shape.
      onCode?.(tokenName.trim(), [selectedScope]);
      return;
    }
    submitting = true;
    error = "";
    try {
      const token = await createWorkspaceBotToken(workspaceID, botUserID, {
        name: tokenName.trim(),
        scopes: [selectedScope],
      });
      onCreated(token);
    } catch (err) {
      error = botLoadErrorMessage(err);
    } finally {
      submitting = false;
    }
  }
</script>

<form class="ws-bots__form ws-bots__token-form" onsubmit={submit}>
  <header class="ws-bots__form-header">
    <h4 class="ws-bots__form-title">{mode === "code" ? "New setup code" : "Mint new token"}</h4>
    <p class="ws-bots__form-hint">
      {mode === "code"
        ? "The code mints a fresh token for this bot when OpenClaw claims it. Name the runtime and grant only the access it needs."
        : "Name the runtime using this credential and grant only the access it needs."}
    </p>
  </header>

  <label class="ws-bots__form-field">
    <span class="ws-bots__form-label">Token name</span>
    <input
      class="ws-bots__form-input"
      type="text"
      bind:value={tokenName}
      placeholder="openclaw-production"
      maxlength="80"
      required
    />
  </label>

  <fieldset class="ws-bots__form-field">
    <legend class="ws-bots__form-label">Scope</legend>
    <div class="ws-bots__choices">
      {#each BOT_SCOPE_BUNDLES as bundle (bundle.id)}
        <label class="ws-bots__choice" class:is-active={selectedScope === bundle.id}>
          <input type="radio" name="token-scope" value={bundle.id} bind:group={selectedScope} />
          <span class="ws-bots__choice-title">{bundle.label}</span>
          <span class="ws-bots__choice-hint">{bundle.hint}</span>
        </label>
      {/each}
    </div>
  </fieldset>

  {#if error}
    <p class="ws-bots__form-error" role="alert">{error}</p>
  {/if}

  <div class="ws-bots__form-actions">
    <button type="button" class="ws-btn" onclick={onCancel} disabled={submitting}>Cancel</button>
    <button type="submit" class="ws-btn ws-btn--primary" disabled={!canSubmit}>
      {mode === "code" ? "Generate code" : submitting ? "Minting…" : "Mint token"}
    </button>
  </div>
</form>

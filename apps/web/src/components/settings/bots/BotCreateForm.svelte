<script lang="ts">
  import {
    createWorkspaceBot,
    suggestHandleFrom,
    botLoadErrorMessage,
    BOT_SCOPE_BUNDLES,
    type BotScopeBundle,
    type CreateBotResponse,
  } from "../../../lib/bots";

  type Ownership = "service" | "user";

  type Props = {
    workspaceID: string;
    currentUserID: string;
    canCreateService: boolean;
    onCreated: (response: CreateBotResponse, scopes: string[]) => void;
    onCancel: () => void;
  };

  let {
    workspaceID,
    currentUserID,
    canCreateService,
    onCreated,
    onCancel,
  }: Props = $props();

  let displayName = $state("");
  let handle = $state("");
  let handleEdited = $state(false);
  let ownership = $state<Ownership>(canCreateService ? "service" : "user");
  let tokenName = $state("default");
  let selectedScope = $state<BotScopeBundle>("bot:write");
  let submitting = $state(false);
  let error = $state("");

  $effect(() => {
    if (!handleEdited) {
      handle = suggestHandleFrom(displayName);
    }
  });

  function onHandleInput(event: Event) {
    handleEdited = true;
    handle = (event.target as HTMLInputElement).value;
  }

  const canSubmit = $derived(
    !submitting && displayName.trim().length > 0 && handle.trim().length > 0,
  );

  async function submit(event: Event) {
    event.preventDefault();
    if (!canSubmit) return;
    submitting = true;
    error = "";
    try {
      const response = await createWorkspaceBot(workspaceID, {
        display_name: displayName.trim(),
        handle: handle.trim(),
        owner_user_id: ownership === "user" ? currentUserID : undefined,
        token_name: tokenName.trim() || "default",
        scopes: [selectedScope],
      });
      onCreated(response, [selectedScope]);
    } catch (err) {
      error = botLoadErrorMessage(err);
    } finally {
      submitting = false;
    }
  }
</script>

<form class="ws-bots__form" onsubmit={submit}>
  <header class="ws-bots__form-header">
    <h3 class="ws-bots__form-title">New bot</h3>
    <p class="ws-bots__form-hint">
      Bots post to channels and DMs through tokens you mint here. Plug a token into OpenClaw to give
      it a presence in this workspace.
    </p>
  </header>

  <div class="ws-bots__form-grid">
    <label class="ws-bots__form-field">
      <span class="ws-bots__form-label">Display name</span>
      <input
        class="ws-bots__form-input"
        type="text"
        bind:value={displayName}
        placeholder="OpenClaw"
        maxlength="80"
        required
      />
    </label>

    <label class="ws-bots__form-field">
      <span class="ws-bots__form-label">Handle</span>
      <div class="ws-bots__form-handle">
        <span aria-hidden="true">@</span>
        <input
          class="ws-bots__form-input"
          type="text"
          value={handle}
          oninput={onHandleInput}
          placeholder="openclaw"
          required
        />
      </div>
    </label>
  </div>

  <fieldset class="ws-bots__form-field">
    <legend class="ws-bots__form-label">Ownership</legend>
    <div class="ws-bots__choices">
      {#if canCreateService}
        <label class="ws-bots__choice" class:is-active={ownership === "service"}>
          <input type="radio" name="ownership" value="service" bind:group={ownership} />
          <span class="ws-bots__choice-title">Service bot</span>
          <span class="ws-bots__choice-hint">
            Belongs to the workspace. Any owner or moderator can rotate its tokens.
          </span>
        </label>
      {/if}
      <label class="ws-bots__choice" class:is-active={ownership === "user"}>
        <input type="radio" name="ownership" value="user" bind:group={ownership} />
        <span class="ws-bots__choice-title">User-owned bot</span>
        <span class="ws-bots__choice-hint">
          Belongs to you. Only you can rotate or revoke its tokens. Managers can remove it from this
          workspace.
        </span>
      </label>
    </div>
  </fieldset>

  <fieldset class="ws-bots__form-field">
    <legend class="ws-bots__form-label">Scope</legend>
    <div class="ws-bots__choices">
      {#each BOT_SCOPE_BUNDLES as bundle (bundle.id)}
        <label class="ws-bots__choice" class:is-active={selectedScope === bundle.id}>
          <input type="radio" name="scope" value={bundle.id} bind:group={selectedScope} />
          <span class="ws-bots__choice-title">{bundle.label}</span>
          <span class="ws-bots__choice-hint">{bundle.hint}</span>
        </label>
      {/each}
    </div>
  </fieldset>

  <label class="ws-bots__form-field">
    <span class="ws-bots__form-label">Token name</span>
    <input
      class="ws-bots__form-input"
      type="text"
      bind:value={tokenName}
      placeholder="default"
      maxlength="80"
    />
  </label>

  {#if error}
    <p class="ws-bots__form-error" role="alert">{error}</p>
  {/if}

  <div class="ws-bots__form-actions">
    <button type="button" class="ws-btn" onclick={onCancel} disabled={submitting}>Cancel</button>
    <button type="submit" class="ws-btn ws-btn--primary" disabled={!canSubmit}>
      {submitting ? "Creating…" : "Create bot"}
    </button>
  </div>
</form>

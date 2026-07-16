<script lang="ts" module>
  export type ConnectMethod = "code" | "token";

  export type BotCreateDetail = {
    connect: ConnectMethod;
    tokenName: string;
    scopes: string[];
  };
</script>

<script lang="ts">
  import { untrack } from "svelte";
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
    onCreated: (response: CreateBotResponse, detail: BotCreateDetail) => void;
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
  let ownership = $state<Ownership>(untrack(() => (canCreateService ? "service" : "user")));
  let connect = $state<ConnectMethod>("code");
  let tokenName = $state("default");
  let selectedScope = $state<BotScopeBundle>("bot:write");
  let setupNonce = $state(crypto.randomUUID());
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
      const name = tokenName.trim() || "default";
      const response = await createWorkspaceBot(workspaceID, {
        display_name: displayName.trim(),
        handle: handle.trim(),
        owner_user_id: ownership === "user" ? currentUserID : undefined,
        setup_nonce: setupNonce,
        // Code mode creates the bot without a credential; the setup code
        // mints the token when OpenClaw claims it.
        ...(connect === "code"
          ? { initial_token: false }
          : { token_name: name, scopes: [selectedScope] }),
      });
      onCreated(response, { connect, tokenName: name, scopes: [selectedScope] });
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
    <legend class="ws-bots__form-label">How will you connect it?</legend>
    <div class="ws-bots__choices">
      <label class="ws-bots__choice" class:is-active={connect === "code"}>
        <input type="radio" name="connect" value="code" bind:group={connect} />
        <span class="ws-bots__choice-title">Setup code (recommended)</span>
        <span class="ws-bots__choice-hint">
          One command on the OpenClaw machine. The one-time code mints the token there — nothing to
          copy by hand.
        </span>
      </label>
      <label class="ws-bots__choice" class:is-active={connect === "token"}>
        <input type="radio" name="connect" value="token" bind:group={connect} />
        <span class="ws-bots__choice-title">Manual token</span>
        <span class="ws-bots__choice-hint">
          Mint a raw token now and wire it up yourself — for hand-written config or clients other
          than OpenClaw.
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
    {#if connect === "code"}
      <span class="ws-bots__form-hint">Names the token the setup code mints when it's claimed.</span>
    {/if}
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

<script lang="ts">
  import {
    activeTokens,
    botLoadErrorMessage,
    createWorkspaceBotToken,
    isServiceBot,
    listWorkspaceBots,
    listWorkspaceBotTokens,
    removeBotFromWorkspace,
    revokeBotToken,
    type BotToken,
    type BotWithTokens,
    type CreateBotResponse,
  } from "../../../../../lib/bots";
  import { isWorkspaceManager } from "../../../../../lib/permissions";
  import BotCreateForm from "../../../../../components/settings/bots/BotCreateForm.svelte";
  import TokenRevealPanel from "../../../../../components/settings/bots/TokenRevealPanel.svelte";
  import { untrack } from "svelte";

  let { data } = $props();

  let bots = $state<BotWithTokens[]>(untrack(() => data.bots));
  let loadError = $state(untrack(() => data.loadError));
  let refreshing = $state(false);
  let showCreate = $state(false);
  let revealed = $state<{
    bot: BotWithTokens["bot"];
    token: BotToken;
  } | null>(null);
  let expandedBotID = $state<string | null>(null);
  let pendingAction = $state<{ botID: string; kind: "rotate" | "revoke" | "remove" } | null>(null);
  let actionError = $state("");

  const me = $derived(data.me);
  const workspaceID = $derived(data.workspaceID);
  const workspaceRouteID = $derived(data.workspaceRouteID || data.workspace?.route_id || data.workspaceID);
  const canManage = $derived(isWorkspaceManager(data.workspace?.role));
  const canCreateService = $derived(canManage);

  function ownerBadge(bot: BotWithTokens["bot"]): string {
    if (isServiceBot(bot)) return "Service";
    if (me && bot.owner_user_id === me.id) return "Owned by you";
    return `Owned`;
  }

  function canRotate(bot: BotWithTokens["bot"]): boolean {
    if (isServiceBot(bot)) return true; // backend will gate
    return !!me && bot.owner_user_id === me.id;
  }

  function canRemove(): boolean {
    return true; // managers only — backend gates; we show inline error on 403
  }

  function toggleExpand(botID: string) {
    expandedBotID = expandedBotID === botID ? null : botID;
  }

  async function refresh() {
    refreshing = true;
    try {
      bots = await listWorkspaceBots(workspaceID);
      loadError = "";
    } catch (err) {
      loadError = botLoadErrorMessage(err);
    } finally {
      refreshing = false;
    }
  }

  async function refreshOneBotTokens(botID: string) {
    try {
      const tokens = await listWorkspaceBotTokens(workspaceID, botID);
      bots = bots.map((b) => (b.bot.id === botID ? { ...b, tokens } : b));
    } catch (err) {
      actionError = botLoadErrorMessage(err);
    }
  }

  function handleCreated(response: CreateBotResponse) {
    showCreate = false;
    revealed = { bot: response.bot, token: response.bot_token };
    bots = [{ bot: response.bot, tokens: [response.bot_token] }, ...bots];
    expandedBotID = response.bot.id;
  }

  async function rotateToken(bot: BotWithTokens["bot"]) {
    pendingAction = { botID: bot.id, kind: "rotate" };
    actionError = "";
    try {
      const token = await createWorkspaceBotToken(workspaceID, bot.id, {
        name: "rotated",
        scopes: ["bot:write"],
      });
      revealed = { bot, token };
      await refreshOneBotTokens(bot.id);
    } catch (err) {
      actionError = botLoadErrorMessage(err);
    } finally {
      pendingAction = null;
    }
  }

  async function revokeOne(bot: BotWithTokens["bot"], token: BotToken) {
    if (!confirm(`Revoke "${token.name || "token"}"? Anything using it will fail immediately.`)) {
      return;
    }
    pendingAction = { botID: bot.id, kind: "revoke" };
    actionError = "";
    try {
      await revokeBotToken(token.id);
      await refreshOneBotTokens(bot.id);
    } catch (err) {
      actionError = botLoadErrorMessage(err);
    } finally {
      pendingAction = null;
    }
  }

  async function removeBot(bot: BotWithTokens["bot"]) {
    const message = isServiceBot(bot)
      ? `Delete service bot @${bot.handle}? All of its tokens will be revoked.`
      : `Remove @${bot.handle} from this workspace? Its tokens here will be revoked.`;
    if (!confirm(message)) return;
    pendingAction = { botID: bot.id, kind: "remove" };
    actionError = "";
    try {
      await removeBotFromWorkspace(workspaceID, bot.id);
      bots = bots.filter((b) => b.bot.id !== bot.id);
      if (expandedBotID === bot.id) expandedBotID = null;
    } catch (err) {
      actionError = botLoadErrorMessage(err);
    } finally {
      pendingAction = null;
    }
  }

  function formatDate(value: string | undefined): string {
    if (!value) return "—";
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return "—";
    return d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
  }

  function formatHandle(handle: string): string {
    return handle.startsWith("@") ? handle : `@${handle}`;
  }

  function initials(name: string): string {
    const parts = name.trim().split(/\s+/).filter(Boolean);
    if (parts.length >= 2) return (parts[0]![0]! + parts[1]![0]!).toUpperCase();
    return name.slice(0, 2).toUpperCase();
  }

  const countLabel = $derived(`${bots.length} ${bots.length === 1 ? "bot" : "bots"}`);
</script>

<div class="ws-bots-page">
  <header class="ws-page__header">
    <h1 class="ws-page__h1">Bots &amp; agents</h1>
    <p class="ws-page__lead">
      Bots are workspace members backed by tokens. Plug a token into OpenClaw to give an agent a
      presence in this workspace.
    </p>
  </header>

  <div class="ws-bots__toolbar">
    <div class="ws-bots__count">{countLabel}</div>
    <div class="ws-bots__toolbar-actions">
      <button
        type="button"
        class="ws-btn"
        onclick={refresh}
        disabled={refreshing}
        title="Refresh"
      >
        {refreshing ? "Refreshing…" : "Refresh"}
      </button>
      {#if canCreateService}
        <button
          type="button"
          class="ws-btn ws-btn--primary"
          onclick={() => {
            showCreate = true;
            revealed = null;
          }}
          disabled={showCreate}
        >
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M12 5v14M5 12h14" />
          </svg>
          Add bot
        </button>
      {/if}
    </div>
  </div>

  {#if loadError}
    <div class="ws-settings__error">{loadError}</div>
  {/if}

  {#if revealed && me}
    <TokenRevealPanel
      token={revealed.token}
      botHandle={formatHandle(revealed.bot.handle)}
      workspaceRouteID={workspaceRouteID}
      onDismiss={() => (revealed = null)}
    />
  {/if}

  {#if showCreate && me}
    <div class="ws-bots__panel">
      <BotCreateForm
        workspaceID={workspaceID}
        currentUserID={me.id}
        {canCreateService}
        onCreated={handleCreated}
        onCancel={() => (showCreate = false)}
      />
    </div>
  {/if}

  {#if actionError}
    <div class="ws-settings__error">{actionError}</div>
  {/if}

  {#if bots.length === 0 && !loadError}
    <div class="ws-bots__empty">
      No bots yet. Add one to plug an agent into this workspace.
    </div>
  {:else}
    <div class="ws-bots__list">
      {#each bots as entry (entry.bot.id)}
        {@const bot = entry.bot}
        {@const tokens = activeTokens(entry.tokens)}
        {@const expanded = expandedBotID === bot.id}
        {@const acting = pendingAction?.botID === bot.id}
        <div class="ws-bots__row" class:is-expanded={expanded}>
          <button
            type="button"
            class="ws-bots__row-main"
            aria-expanded={expanded}
            onclick={() => toggleExpand(bot.id)}
          >
            <span
              class="ws-members__avatar ws-members__avatar--{isServiceBot(bot) ? 'human' : 'bot'}"
              aria-hidden="true"
            >
              {#if bot.avatar_url}
                <img src={bot.avatar_url} alt="" />
              {:else}
                {initials(bot.display_name || bot.handle || "?")}
              {/if}
            </span>
            <div class="ws-bots__row-text">
              <div class="ws-bots__row-name">{bot.display_name || bot.handle}</div>
              <div class="ws-bots__row-meta">
                <code class="ws-members__handle">{formatHandle(bot.handle)}</code>
                <span class="ws-members__dot" aria-hidden="true">·</span>
                <span>Created {formatDate(bot.created_at)}</span>
                <span class="ws-members__dot" aria-hidden="true">·</span>
                <span>{tokens.length} active {tokens.length === 1 ? "token" : "tokens"}</span>
              </div>
            </div>
            <span class="ws-bots__owner-pill ws-bots__owner-pill--{isServiceBot(bot) ? 'service' : 'user'}">
              {ownerBadge(bot)}
            </span>
            <span class="ws-bots__chevron" aria-hidden="true">
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <polyline points={expanded ? "18 15 12 9 6 15" : "6 9 12 15 18 9"} />
              </svg>
            </span>
          </button>

          {#if expanded}
            <div class="ws-bots__detail">
              <div class="ws-bots__detail-header">
                <div>
                  <h4 class="ws-bots__detail-title">Tokens</h4>
                  <p class="ws-bots__detail-hint">
                    Each token authenticates one OpenClaw process. Rotate to mint a new one; revoke
                    to cut off whatever is using it.
                  </p>
                </div>
                <div class="ws-bots__detail-actions">
                  {#if canRotate(bot)}
                    <button
                      type="button"
                      class="ws-btn"
                      onclick={() => rotateToken(bot)}
                      disabled={acting}
                    >
                      {acting && pendingAction?.kind === "rotate" ? "Rotating…" : "Mint new token"}
                    </button>
                  {/if}
                  {#if canRemove()}
                    <button
                      type="button"
                      class="ws-btn ws-btn--danger"
                      onclick={() => removeBot(bot)}
                      disabled={acting}
                    >
                      {acting && pendingAction?.kind === "remove"
                        ? "Removing…"
                        : isServiceBot(bot)
                          ? "Delete bot"
                          : "Remove from workspace"}
                    </button>
                  {/if}
                </div>
              </div>

              {#if tokens.length === 0}
                <p class="ws-bots__detail-empty">No active tokens. Mint one to get this bot online.</p>
              {:else}
                <ul class="ws-bots__token-list">
                  {#each tokens as token (token.id)}
                    <li class="ws-bots__token-row">
                      <div class="ws-bots__token-main">
                        <div class="ws-bots__token-name">{token.name || "token"}</div>
                        <div class="ws-bots__token-meta">
                          Created {formatDate(token.created_at)}
                          {#if token.last_used_at}
                            · last used {formatDate(token.last_used_at)}
                          {/if}
                        </div>
                        {#if token.scopes?.length}
                          <div class="ws-bots__scope-row">
                            {#each token.scopes as scope (scope)}
                              <span class="ws-bots__scope-chip">{scope}</span>
                            {/each}
                          </div>
                        {/if}
                      </div>
                      {#if canRotate(bot)}
                        <button
                          type="button"
                          class="ws-btn ws-btn--danger"
                          onclick={() => revokeOne(bot, token)}
                          disabled={acting}
                        >
                          Revoke
                        </button>
                      {/if}
                    </li>
                  {/each}
                </ul>
              {/if}
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

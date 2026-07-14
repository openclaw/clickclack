<script lang="ts">
  import { untrack } from "svelte";
  import {
    activeOnly,
    attachedTo,
    integrationsLoadErrorMessage,
    listAppInstallations,
    listConnectedAccounts,
    listEventSubscriptions,
    listSlashCommands,
    revokeAppInstallation,
    revokeConnectedAccount,
    unattached,
    type AppInstallation,
    type ConnectedAccount,
    type EventSubscription,
    type SlashCommand,
  } from "$lib/integrations";
  import { isServiceBot, listWorkspaceBots, type BotWithTokens } from "$lib/bots";
  import { manifestForInstallation } from "$lib/app-catalog";
  import { isWorkspaceManager } from "$lib/permissions";
  import InstallWizard from "../../../../../../components/settings/integrations/InstallWizard.svelte";
  import SlashCommandsPanel from "../../../../../../components/settings/integrations/SlashCommandsPanel.svelte";
  import EventSubscriptionsPanel from "../../../../../../components/settings/integrations/EventSubscriptionsPanel.svelte";

  let { data } = $props();

  let installations = $state<AppInstallation[]>(untrack(() => data.installations));
  let commands = $state<SlashCommand[]>(untrack(() => data.commands));
  let subscriptions = $state<EventSubscription[]>(untrack(() => data.subscriptions));
  let connectedAccounts = $state<ConnectedAccount[]>(untrack(() => data.connectedAccounts));
  let bots = $state<BotWithTokens[]>(untrack(() => data.bots));
  let loadError = $state(untrack(() => data.loadError));
  let refreshing = $state(false);
  let showWizard = $state(false);
  let expandedID = $state("");
  let actionError = $state("");
  // Cascade-revoke confirm state for one installation at a time.
  let revokePending = $state<{ installation: AppInstallation; revokeTokens: boolean } | null>(
    null,
  );
  let revoking = $state(false);
  let accountBusyID = $state("");

  const me = $derived(data.me);
  const workspaceID = $derived(data.workspaceID);
  const workspaceIdentifier = $derived(data.workspaceIdentifier || data.workspaceID);
  const canManage = $derived(isWorkspaceManager(data.workspace?.role));

  const activeInstallations = $derived(activeOnly(installations));
  const activeCommands = $derived(activeOnly(commands));
  const activeSubscriptions = $derived(activeOnly(subscriptions));
  const unattachedCommands = $derived(unattached(activeCommands));
  const unattachedSubscriptions = $derived(unattached(activeSubscriptions));
  const boundBotIDs = $derived(
    new Set(activeInstallations.map((installation) => installation.bot_user_id)),
  );

  const countLabel = $derived(
    `${activeInstallations.length} ${activeInstallations.length === 1 ? "app" : "apps"} installed`,
  );

  function botFor(installation: AppInstallation) {
    return bots.find((entry) => entry.bot.id === installation.bot_user_id)?.bot;
  }

  async function refresh() {
    refreshing = true;
    try {
      const [installationsResult, commandsResult, subscriptionsResult, accountsResult, botsResult] =
        await Promise.all([
          listAppInstallations(workspaceID),
          listSlashCommands(workspaceID),
          listEventSubscriptions(workspaceID),
          listConnectedAccounts(workspaceID),
          listWorkspaceBots(workspaceID),
        ]);
      installations = installationsResult;
      commands = commandsResult;
      subscriptions = subscriptionsResult;
      connectedAccounts = accountsResult;
      bots = botsResult;
      loadError = "";
    } catch (err) {
      loadError = integrationsLoadErrorMessage(err);
    } finally {
      refreshing = false;
    }
  }

  async function refreshHooks() {
    try {
      const [commandsResult, subscriptionsResult] = await Promise.all([
        listSlashCommands(workspaceID),
        listEventSubscriptions(workspaceID),
      ]);
      commands = commandsResult;
      subscriptions = subscriptionsResult;
    } catch (err) {
      actionError = integrationsLoadErrorMessage(err);
    }
  }

  function handleInstalled(installation: AppInstallation) {
    installations = [installation, ...installations];
    expandedID = installation.id;
    void refresh();
  }

  function startRevoke(installation: AppInstallation) {
    actionError = "";
    revokePending = { installation, revokeTokens: false };
  }

  async function confirmRevoke() {
    if (!revokePending || revoking) return;
    const { installation, revokeTokens } = revokePending;
    revoking = true;
    actionError = "";
    try {
      await revokeAppInstallation(installation.id, {
        revoke_slash_commands: true,
        revoke_event_subscriptions: true,
        revoke_bot_tokens: revokeTokens,
      });
      revokePending = null;
      if (expandedID === installation.id) expandedID = "";
      await refresh();
    } catch (err) {
      actionError = integrationsLoadErrorMessage(err);
    } finally {
      revoking = false;
    }
  }

  async function revokeAccount(account: ConnectedAccount) {
    if (
      !confirm(
        `Disconnect ${account.display_name || account.provider_account_id} (${account.provider})? The app loses this account binding immediately.`,
      )
    ) {
      return;
    }
    accountBusyID = account.id;
    actionError = "";
    try {
      await revokeConnectedAccount(account.id);
      connectedAccounts = connectedAccounts.filter((entry) => entry.id !== account.id);
    } catch (err) {
      actionError = integrationsLoadErrorMessage(err);
    } finally {
      accountBusyID = "";
    }
  }

  function canRevokeAccount(account: ConnectedAccount): boolean {
    return canManage || (!!me && account.user_id === me.id);
  }

  function formatDate(value: string | undefined): string {
    if (!value) return "—";
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return "—";
    return d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
  }

  function revokeImpact(installation: AppInstallation): {
    commandCount: number;
    subscriptionCount: number;
    tokenCount: number;
  } {
    const bot = bots.find((entry) => entry.bot.id === installation.bot_user_id);
    return {
      commandCount: attachedTo(activeCommands, installation.id).length,
      subscriptionCount: attachedTo(activeSubscriptions, installation.id).length,
      tokenCount: bot ? bot.tokens.filter((token) => !token.revoked_at).length : 0,
    };
  }

  function canRevokeInstallationTokens(installation: AppInstallation): boolean {
    const bot = botFor(installation);
    return !!bot && (isServiceBot(bot) || bot.owner_user_id === me?.id);
  }
</script>

<div class="ws-intg-page">
  <header class="ws-page__header">
    <h1 class="ws-page__h1">Integrations</h1>
    <p class="ws-page__lead">
      Apps connected to this workspace. Each install binds an agent platform to a bot identity,
      with its own token, slash commands, and event subscriptions.
    </p>
  </header>

  <div class="ws-bots__toolbar">
    <div class="ws-bots__count">{countLabel}</div>
    <div class="ws-bots__toolbar-actions">
      <button type="button" class="ws-btn" onclick={refresh} disabled={refreshing} title="Refresh">
        {refreshing ? "Refreshing…" : "Refresh"}
      </button>
      {#if canManage && me}
        <button
          type="button"
          class="ws-btn ws-btn--primary"
          onclick={() => (showWizard = true)}
          disabled={showWizard}
        >
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M12 5v14M5 12h14" />
          </svg>
          Add app
        </button>
      {/if}
    </div>
  </div>

  {#if loadError}
    <div class="ws-settings__error">{loadError}</div>
  {/if}
  {#if actionError}
    <div class="ws-settings__error">{actionError}</div>
  {/if}

  {#if showWizard && me}
    <InstallWizard
      {workspaceID}
      {workspaceIdentifier}
      currentUserID={me.id}
      {bots}
      {boundBotIDs}
      channels={data.channels}
      onInstalled={handleInstalled}
      onClose={() => (showWizard = false)}
    />
  {/if}

  {#if activeInstallations.length === 0 && !loadError}
    <div class="ws-bots__empty">
      No apps installed. Add one to connect an agent platform to this workspace.
    </div>
  {:else}
    <div class="ws-bots__list">
      {#each activeInstallations as installation (installation.id)}
        {@const manifest = manifestForInstallation(installation.app_slug)}
        {@const bot = botFor(installation)}
        {@const expanded = expandedID === installation.id}
        {@const pendingHere = revokePending?.installation.id === installation.id}
        <div class="ws-bots__row" class:is-expanded={expanded}>
          <button
            type="button"
            class="ws-bots__row-main"
            aria-expanded={expanded}
            onclick={() => (expandedID = expanded ? "" : installation.id)}
          >
            <span class="ws-intg__app-icon" aria-hidden="true">
              <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                {#each manifest.icon as path (path)}
                  <path d={path} />
                {/each}
              </svg>
            </span>
            <div class="ws-bots__row-text">
              <div class="ws-bots__row-name">{installation.display_name || manifest.name}</div>
              <div class="ws-bots__row-meta">
                <code class="ws-members__handle">{installation.app_slug}</code>
                {#if bot}
                  <span class="ws-members__dot" aria-hidden="true">·</span>
                  <span>@{bot.handle}</span>
                {/if}
                <span class="ws-members__dot" aria-hidden="true">·</span>
                <span>Installed {formatDate(installation.created_at)}</span>
              </div>
            </div>
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
                  <h4 class="ws-bots__detail-title">
                    {manifest.name}
                  </h4>
                  <p class="ws-bots__detail-hint">
                    {#if bot}
                      Connected as <a href={`/app/${workspaceID}/settings/bots`}>@{bot.handle}</a>
                      — tokens are managed on the Bots page.
                    {:else}
                      The bound bot is no longer in this workspace.
                    {/if}
                  </p>
                </div>
                {#if canManage}
                  <div class="ws-bots__detail-actions">
                    <button
                      type="button"
                      class="ws-btn ws-btn--danger"
                      onclick={() => startRevoke(installation)}
                      disabled={pendingHere}
                    >
                      Uninstall
                    </button>
                  </div>
                {/if}
              </div>

              {#if pendingHere && revokePending}
                {@const impact = revokeImpact(installation)}
                <div class="ws-intg__confirm" role="alertdialog" aria-label="Confirm uninstall">
                  <p class="ws-intg__confirm-text">
                    Uninstalling revokes {impact.commandCount}
                    {impact.commandCount === 1 ? "slash command" : "slash commands"} and
                    {impact.subscriptionCount}
                    {impact.subscriptionCount === 1 ? "event subscription" : "event subscriptions"}
                    attached to this app. Delivery history is kept.
                  </p>
                  {#if canRevokeInstallationTokens(installation) && impact.tokenCount > 0}
                    <label class="ws-intg__toggle">
                      <input type="checkbox" bind:checked={revokePending.revokeTokens} />
                      <span>
                        <span class="ws-bots__choice-title">
                          Also revoke the bot's {impact.tokenCount} active
                          {impact.tokenCount === 1 ? "token" : "tokens"}
                        </span>
                        <span class="ws-bots__choice-hint">
                          Anything still using them fails immediately. Leave unchecked if the bot
                          is shared with other things.
                        </span>
                      </span>
                    </label>
                  {/if}
                  <div class="ws-bots__form-actions">
                    <button
                      type="button"
                      class="ws-btn"
                      onclick={() => (revokePending = null)}
                      disabled={revoking}
                    >
                      Cancel
                    </button>
                    <button
                      type="button"
                      class="ws-btn ws-btn--danger"
                      onclick={confirmRevoke}
                      disabled={revoking}
                    >
                      {revoking ? "Uninstalling…" : "Uninstall app"}
                    </button>
                  </div>
                </div>
              {/if}

              <SlashCommandsPanel
                {workspaceID}
                commands={attachedTo(activeCommands, installation.id)}
                installationID={installation.id}
                botUserID={installation.bot_user_id}
                {canManage}
                onChanged={refreshHooks}
              />

              <EventSubscriptionsPanel
                {workspaceID}
                subscriptions={attachedTo(activeSubscriptions, installation.id)}
                eventTypes={data.eventTypes}
                installationID={installation.id}
                {canManage}
                onChanged={refreshHooks}
              />
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}

  {#if unattachedCommands.length > 0 || unattachedSubscriptions.length > 0}
    <section class="ws-intg__section">
      <h2 class="ws-intg__section-title">Not attached to an app</h2>
      <p class="ws-intg__section-hint">
        Created directly through the API without an app installation. They work the same; they just
        aren't bundled into an install.
      </p>
      {#if unattachedCommands.length > 0}
        <SlashCommandsPanel
          {workspaceID}
          commands={unattachedCommands}
          {canManage}
          onChanged={refreshHooks}
        />
      {/if}
      {#if unattachedSubscriptions.length > 0}
        <EventSubscriptionsPanel
          {workspaceID}
          subscriptions={unattachedSubscriptions}
          eventTypes={data.eventTypes}
          {canManage}
          onChanged={refreshHooks}
        />
      {/if}
    </section>
  {/if}

  <section class="ws-intg__section">
    <h2 class="ws-intg__section-title">Connected accounts</h2>
    <p class="ws-intg__section-hint">
      External identities apps have linked to members of this workspace.
    </p>
    {#if connectedAccounts.length === 0}
      <p class="ws-intg__panel-empty">No connected accounts.</p>
    {:else}
      <ul class="ws-intg__item-list">
        {#each connectedAccounts as account (account.id)}
          <li class="ws-intg__item-row">
            <div class="ws-intg__item-main">
              <div class="ws-intg__item-name">
                {account.display_name || account.provider_account_id}
              </div>
              <div class="ws-intg__item-meta">
                <code class="ws-members__handle">{account.provider}</code>
                · connected {formatDate(account.created_at)}
                {#if account.scopes?.length}
                  · {account.scopes.join(", ")}
                {/if}
              </div>
            </div>
            {#if canRevokeAccount(account)}
              <div class="ws-intg__item-actions">
                <button
                  type="button"
                  class="ws-btn ws-btn--danger"
                  onclick={() => revokeAccount(account)}
                  disabled={accountBusyID === account.id}
                >
                  Disconnect
                </button>
              </div>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </section>
</div>

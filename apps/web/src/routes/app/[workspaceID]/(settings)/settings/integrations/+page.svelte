<script lang="ts">
  import { untrack } from "svelte";
  import { api } from "$lib/api";
  import {
    activeOnly,
    attachedTo,
    integrationsLoadErrorMessage,
    listAppInstallations,
    listConnectedAccounts,
    listEventSubscriptions,
    listEventTypes,
    listSlashCommands,
    revokeAppInstallation,
    revokeConnectedAccount,
    unattached,
    type AppInstallation,
    type ConnectedAccount,
    type EventSubscription,
    type SlashCommand,
  } from "$lib/integrations";
  import {
    isServiceBot,
    listWorkspaceBots,
    type BotToken,
    type BotWithTokens,
  } from "$lib/bots";
  import { manifestForInstallation } from "$lib/app-catalog";
  import { isWorkspaceManager } from "$lib/permissions";
  import type { Channel, User } from "$lib/types";
  import InstallWizard from "../../../../../../components/settings/integrations/InstallWizard.svelte";
  import SlashCommandsPanel from "../../../../../../components/settings/integrations/SlashCommandsPanel.svelte";
  import EventSubscriptionsPanel from "../../../../../../components/settings/integrations/EventSubscriptionsPanel.svelte";

  let { data } = $props();

  let installations = $state<AppInstallation[]>(untrack(() => data.installations));
  let commands = $state<SlashCommand[]>(untrack(() => data.commands));
  let subscriptions = $state<EventSubscription[]>(untrack(() => data.subscriptions));
  let connectedAccounts = $state<ConnectedAccount[]>(untrack(() => data.connectedAccounts));
  let bots = $state<BotWithTokens[]>(untrack(() => data.bots));
  let channels = $state<Channel[]>(untrack(() => data.channels));
  let eventTypes = $state<string[]>(untrack(() => data.eventTypes));
  let me = $state<User | null>(untrack(() => data.me));
  let loaded = $state({ ...untrack(() => data.loaded) });
  let loadError = $state(untrack(() => data.loadError));
  let refreshing = $state(false);
  let refreshGeneration = 0;
  let mutationRevision = 0;
  let showWizard = $state(false);
  let expandedID = $state("");
  let actionError = $state("");
  // Cascade-revoke confirm state for one installation at a time.
  let revokePending = $state<{ installation: AppInstallation; revokeTokens: boolean } | null>(
    null,
  );
  let revoking = $state(false);
  let accountBusyIDs = $state<Set<string>>(new Set());

  const workspaceID = $derived(data.workspaceID);
  const workspaceIdentifier = $derived(data.workspaceIdentifier || data.workspaceID);
  const canManage = $derived(isWorkspaceManager(data.workspace?.role));
  const canAssessRevoke = $derived(loaded.commands && loaded.subscriptions && loaded.bots);

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

  function settledErrors(results: PromiseSettledResult<unknown>[]): string {
    const messages = results
      .filter((result): result is PromiseRejectedResult => result.status === "rejected")
      .map((result) => integrationsLoadErrorMessage(result.reason));
    return messages.length > 0
      ? `Some integration data could not be loaded. ${[...new Set(messages)].join(" ")}`
      : "";
  }

  async function refresh() {
    const generation = ++refreshGeneration;
    const revision = mutationRevision;
    refreshing = true;
    try {
      const [
        installationsResult,
        commandsResult,
        subscriptionsResult,
        accountsResult,
        botsResult,
        channelsResult,
        eventTypesResult,
        meResult,
      ] = await Promise.allSettled([
          listAppInstallations(workspaceID),
          listSlashCommands(workspaceID),
          listEventSubscriptions(workspaceID),
          listConnectedAccounts(workspaceID),
          listWorkspaceBots(workspaceID),
          api<{ channels: Channel[] }>(`/api/workspaces/${workspaceID}/channels`),
          listEventTypes(),
          api<{ user: User }>("/api/me"),
        ]);

      if (generation !== refreshGeneration || revision !== mutationRevision) return;
      if (installationsResult.status === "fulfilled") installations = installationsResult.value;
      if (commandsResult.status === "fulfilled") commands = commandsResult.value;
      if (subscriptionsResult.status === "fulfilled") subscriptions = subscriptionsResult.value;
      if (accountsResult.status === "fulfilled") connectedAccounts = accountsResult.value;
      if (botsResult.status === "fulfilled") bots = botsResult.value;
      if (channelsResult.status === "fulfilled") channels = channelsResult.value.channels ?? [];
      if (eventTypesResult.status === "fulfilled") eventTypes = eventTypesResult.value;
      if (meResult.status === "fulfilled") me = meResult.value.user;
      loaded = {
        installations: loaded.installations || installationsResult.status === "fulfilled",
        commands: loaded.commands || commandsResult.status === "fulfilled",
        subscriptions: loaded.subscriptions || subscriptionsResult.status === "fulfilled",
        connectedAccounts:
          loaded.connectedAccounts || accountsResult.status === "fulfilled",
        bots: loaded.bots || botsResult.status === "fulfilled",
        channels: loaded.channels || channelsResult.status === "fulfilled",
        eventTypes: loaded.eventTypes || eventTypesResult.status === "fulfilled",
        me: loaded.me || meResult.status === "fulfilled",
      };
      loadError = settledErrors([
        installationsResult,
        commandsResult,
        subscriptionsResult,
        accountsResult,
        botsResult,
        channelsResult,
        eventTypesResult,
        meResult,
      ]);
    } finally {
      if (generation === refreshGeneration) refreshing = false;
    }
  }

  function markMutation() {
    mutationRevision += 1;
  }

  function setAccountBusy(id: string, busy: boolean) {
    const next = new Set(accountBusyIDs);
    if (busy) {
      next.add(id);
    } else {
      next.delete(id);
    }
    accountBusyIDs = next;
  }

  function handleCommandChanged(command: SlashCommand) {
    markMutation();
    const { signing_secret: _, ...commandMetadata } = command;
    commands = [commandMetadata, ...commands.filter((entry) => entry.id !== command.id)];
  }

  function handleSubscriptionChanged(subscription: EventSubscription) {
    markMutation();
    const { signing_secret: _, ...subscriptionMetadata } = subscription;
    subscriptions = [
      subscriptionMetadata,
      ...subscriptions.filter((entry) => entry.id !== subscription.id),
    ];
  }

  function handleInstalled(installation: AppInstallation, bot: User, token: BotToken) {
    markMutation();
    const { token: _, ...tokenMetadata } = token;
    const existingBot = bots.find((entry) => entry.bot.id === bot.id);
    const updatedBot = {
      bot,
      tokens: [
        tokenMetadata,
        ...(existingBot?.tokens.filter((entry) => entry.id !== token.id) ?? []),
      ],
    };
    installations = [installation, ...installations];
    bots = [updatedBot, ...bots.filter((entry) => entry.bot.id !== bot.id)];
    expandedID = installation.id;
  }

  function startRevoke(installation: AppInstallation) {
    if (!canAssessRevoke) return;
    actionError = "";
    revokePending = { installation, revokeTokens: false };
  }

  async function confirmRevoke() {
    if (!revokePending || revoking) return;
    const { installation, revokeTokens } = revokePending;
    revoking = true;
    actionError = "";
    try {
      const result = await revokeAppInstallation(installation.id, {
        revoke_slash_commands: true,
        revoke_event_subscriptions: true,
        revoke_bot_tokens: revokeTokens,
      });
      markMutation();
      installations = [
        result.installation,
        ...installations.filter((entry) => entry.id !== installation.id),
      ];
      commands = commands.filter((entry) => entry.app_installation_id !== installation.id);
      subscriptions = subscriptions.filter(
        (entry) => entry.app_installation_id !== installation.id,
      );
      if (revokeTokens) {
        const revokedAt = new Date().toISOString();
        bots = bots.map((entry) =>
          entry.bot.id === installation.bot_user_id
            ? {
                ...entry,
                tokens: entry.tokens.map((token) =>
                  token.revoked_at ? token : { ...token, revoked_at: revokedAt },
                ),
              }
            : entry,
        );
      }
      revokePending = null;
      if (expandedID === installation.id) expandedID = "";
    } catch (err) {
      actionError = integrationsLoadErrorMessage(err);
    } finally {
      revoking = false;
    }
  }

  async function revokeAccount(account: ConnectedAccount) {
    if (accountBusyIDs.has(account.id)) return;
    if (
      !confirm(
        `Disconnect ${account.display_name || account.provider_account_id} (${account.provider})? The app loses this account binding immediately.`,
      )
    ) {
      return;
    }
    setAccountBusy(account.id, true);
    actionError = "";
    try {
      await revokeConnectedAccount(account.id);
      markMutation();
      connectedAccounts = connectedAccounts.filter((entry) => entry.id !== account.id);
    } catch (err) {
      actionError = integrationsLoadErrorMessage(err);
    } finally {
      setAccountBusy(account.id, false);
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
          disabled={showWizard || !loaded.installations || !loaded.bots || !loaded.channels}
          title={
            !loaded.installations || !loaded.bots || !loaded.channels
              ? "Refresh before adding an app so installation, bot, and channel data is current."
              : undefined
          }
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
      {channels}
      onInstalled={handleInstalled}
      onClose={() => (showWizard = false)}
    />
  {/if}

  {#if activeInstallations.length === 0 && loaded.installations}
    <div class="ws-bots__empty">
      No apps installed. Add one to connect an agent platform to this workspace.
    </div>
  {:else if activeInstallations.length > 0}
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
                    {#if !loaded.bots}
                      Bot details are unavailable. Refresh to load the current binding.
                    {:else if bot}
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
                      disabled={pendingHere || !canAssessRevoke}
                      title={
                        canAssessRevoke
                          ? "Uninstall app"
                          : "Refresh before uninstalling so the cascade impact is exact."
                      }
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

              {#if loaded.commands}
                <SlashCommandsPanel
                  {workspaceID}
                  commands={attachedTo(activeCommands, installation.id)}
                  installationID={installation.id}
                  botUserID={installation.bot_user_id}
                  {canManage}
                  onChanged={handleCommandChanged}
                />
              {/if}

              {#if loaded.subscriptions}
                <EventSubscriptionsPanel
                  {workspaceID}
                  subscriptions={attachedTo(activeSubscriptions, installation.id)}
                  {eventTypes}
                  installationID={installation.id}
                  {canManage}
                  onChanged={handleSubscriptionChanged}
                />
              {/if}
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}

  {#if (loaded.commands && unattachedCommands.length > 0) ||
  (loaded.subscriptions && unattachedSubscriptions.length > 0)}
    <section class="ws-intg__section">
      <h2 class="ws-intg__section-title">Not attached to an app</h2>
      <p class="ws-intg__section-hint">
        Created directly through the API without an app installation. They work the same; they just
        aren't bundled into an install.
      </p>
      {#if loaded.commands && unattachedCommands.length > 0}
        <SlashCommandsPanel
          {workspaceID}
          commands={unattachedCommands}
          {canManage}
          onChanged={handleCommandChanged}
        />
      {/if}
      {#if loaded.subscriptions && unattachedSubscriptions.length > 0}
        <EventSubscriptionsPanel
          {workspaceID}
          subscriptions={unattachedSubscriptions}
          {eventTypes}
          {canManage}
          onChanged={handleSubscriptionChanged}
        />
      {/if}
    </section>
  {/if}

  <section class="ws-intg__section">
    <h2 class="ws-intg__section-title">Connected accounts</h2>
    <p class="ws-intg__section-hint">
      External identities apps have linked to members of this workspace.
    </p>
    {#if !loaded.connectedAccounts}
      <p class="ws-intg__panel-empty">Connected accounts are unavailable. Use Refresh to retry.</p>
    {:else if connectedAccounts.length === 0}
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
                  disabled={accountBusyIDs.has(account.id)}
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

<script lang="ts">
  import { onDestroy, untrack } from "svelte";
  import {
    BOT_SCOPE_BUNDLES,
    activeTokens,
    botLoadErrorMessage,
    createWorkspaceBot,
    createWorkspaceBotToken,
    isServiceBot,
    suggestHandleFrom,
    type BotScopeBundle,
    type BotToken,
    type BotWithTokens,
  } from "../../../lib/bots";
  import {
    createAppInstallation,
    integrationsLoadErrorMessage,
    type AppInstallation,
  } from "../../../lib/integrations";
  import { AGENT_ACTIVITY_SCOPE, APP_CATALOG, type AppManifest } from "../../../lib/app-catalog";
  import {
    listWorkspaceMembersPage,
    type WorkspaceMember,
  } from "../../../lib/workspace-members";
  import type { Channel, User } from "../../../lib/types";
  import TokenRevealPanel from "../bots/TokenRevealPanel.svelte";

  type Props = {
    workspaceID: string;
    workspaceIdentifier: string;
    currentUserID: string;
    bots: BotWithTokens[];
    boundBotIDs: Set<string>;
    channels: Channel[];
    onInstalled: (installation: AppInstallation, bot: User, token: BotToken) => void;
    onClose: () => void;
  };

  let {
    workspaceID,
    workspaceIdentifier,
    currentUserID,
    bots,
    boundBotIDs,
    channels,
    onInstalled,
    onClose,
  }: Props = $props();

  type Step = "app" | "bot" | "config" | "reveal";

  let step = $state<Step>("app");
  let manifest = $state<AppManifest | null>(null);

  // Bot step
  let botMode = $state<"create" | "existing">("create");
  let displayName = $state("");
  let handle = $state("");
  let handleEdited = $state(false);
  let ownership = $state<"service" | "user">("service");
  let tokenName = $state("default");
  let scopeBundle = $state<BotScopeBundle>("bot:write");
  let existingBotID = $state("");

  // Config step (manifest-driven)
  let defaultChannel = $state("");
  let allowMode = $state<"everyone" | "specific">("everyone");
  let allowMembers = $state<{ id: string; label: string }[]>([]);
  let memberQuery = $state("");
  let memberResults = $state<WorkspaceMember[]>([]);
  let memberSearchTimer: ReturnType<typeof setTimeout> | null = null;
  let memberSearchGeneration = 0;
  let agentActivity = $state(false);

  let submitting = $state(false);
  let error = $state("");

  // Set when bot/token creation succeeded but the installation call failed,
  // so retrying doesn't create a second bot or token.
  let createdBot = $state<User | null>(null);
  let createdToken = $state<BotToken | null>(null);
  let result = $state<{ installation: AppInstallation; bot: User; token: BotToken } | null>(null);
  const hasPartialCredentials = $derived(!!createdBot && !!createdToken);

  const availableBots = $derived(
    bots.filter(
      (entry) =>
        !boundBotIDs.has(entry.bot.id) &&
        (isServiceBot(entry.bot) || entry.bot.owner_user_id === currentUserID),
    ),
  );

  const channelNames = $derived.by(() => {
    const names = channels
      .filter((channel) => !channel.archived_at)
      .map((channel) => channel.name)
      .filter(Boolean);
    return names;
  });

  $effect(() => {
    if (!handleEdited) {
      handle = suggestHandleFrom(displayName);
    }
  });

  $effect(() => {
    if (!hasPartialCredentials && !channelNames.includes(defaultChannel)) {
      defaultChannel = channelNames[0] ?? "";
    }
  });

  function pickManifest(next: AppManifest) {
    if (memberSearchTimer) clearTimeout(memberSearchTimer);
    memberSearchTimer = null;
    memberSearchGeneration += 1;
    manifest = next;
    botMode = "create";
    displayName = next.suggestedBotName;
    handle = next.suggestedBotHandle || suggestHandleFrom(next.suggestedBotName);
    handleEdited = false;
    ownership = "service";
    tokenName = "default";
    scopeBundle = next.suggestedScopeBundle;
    existingBotID = "";
    defaultChannel = untrack(() => channelNames[0] ?? "");
    allowMode = "everyone";
    allowMembers = [];
    memberQuery = "";
    memberResults = [];
    agentActivity = false;
    createdBot = null;
    createdToken = null;
    result = null;
    error = "";
    step = "bot";
  }

  function onHandleInput(event: Event) {
    handleEdited = true;
    handle = (event.target as HTMLInputElement).value;
  }

  const hasConfigStep = $derived((manifest?.configFields.length ?? 0) > 0);

  const botStepValid = $derived(
    botMode === "create"
      ? displayName.trim().length > 0 && handle.trim().length > 0 && tokenName.trim().length > 0
      : existingBotID.length > 0 && tokenName.trim().length > 0,
  );

  function advanceFromBot() {
    if (!botStepValid) return;
    error = "";
    if (hasConfigStep) {
      step = "config";
    } else {
      void submit();
    }
  }

  function onMemberQueryInput(event: Event) {
    memberQuery = (event.target as HTMLInputElement).value;
    if (memberSearchTimer) clearTimeout(memberSearchTimer);
    const generation = ++memberSearchGeneration;
    const query = memberQuery.trim();
    if (!query) {
      memberResults = [];
      return;
    }
    memberSearchTimer = setTimeout(async () => {
      try {
        const page = await listWorkspaceMembersPage({ workspaceID, query, limit: 8 });
        if (generation !== memberSearchGeneration) return;
        const chosen = new Set(allowMembers.map((m) => m.id));
        memberResults = page.members.filter(
          (member) => member.user.kind === "human" && !chosen.has(member.user.id),
        );
      } catch {
        if (generation !== memberSearchGeneration) return;
        memberResults = [];
      }
    }, 250);
  }

  function addAllowMember(member: WorkspaceMember) {
    allowMembers = [
      ...allowMembers,
      {
        id: member.user.id,
        label: member.user.display_name || member.user.handle || member.user.id,
      },
    ];
    memberQuery = "";
    memberResults = [];
  }

  function removeAllowMember(id: string) {
    allowMembers = allowMembers.filter((member) => member.id !== id);
  }

  const requiresDefaultChannel = $derived(
    manifest?.configFields.some((field) => field.id === "default_channel") ?? false,
  );
  const configStepValid = $derived(
    (!requiresDefaultChannel || defaultChannel.length > 0) &&
      (allowMode === "everyone" || allowMembers.length > 0),
  );

  const allowFromValue = $derived.by(() => {
    if (allowMode === "everyone") return ["*"];
    return allowMembers.map((member) => member.id);
  });

  const defaultToValue = $derived(`channel:${defaultChannel}`);

  async function submit() {
    if (!manifest || submitting || (hasConfigStep && !configStepValid)) return;
    submitting = true;
    error = "";
    try {
      let bot = createdBot;
      let token = createdToken;
      if (!bot || !token) {
        const scopes = [scopeBundle as string];
        if (agentActivity) scopes.push(AGENT_ACTIVITY_SCOPE);
        if (botMode === "create") {
          const response = await createWorkspaceBot(workspaceID, {
            display_name: displayName.trim(),
            handle: handle.trim(),
            owner_user_id: ownership === "user" ? currentUserID : undefined,
            token_name: tokenName.trim() || "default",
            scopes,
          });
          bot = response.bot;
          token = response.bot_token;
        } else {
          const entry = availableBots.find((candidate) => candidate.bot.id === existingBotID);
          if (!entry) throw new Error("Pick a bot to bind this app to.");
          bot = entry.bot;
          token = await createWorkspaceBotToken(workspaceID, entry.bot.id, {
            name: tokenName.trim() || "default",
            scopes,
          });
        }
        createdBot = bot;
        createdToken = token;
      }

      const config: Record<string, unknown> = {};
      if (hasConfigStep) {
        config.default_to = defaultToValue;
        config.allow_from = allowFromValue;
        config.agent_activity = agentActivity;
      }
      const installation = await createAppInstallation(workspaceID, {
        app_slug: manifest.slug,
        display_name: bot.display_name || manifest.name,
        bot_user_id: bot.id,
        config,
      });
      result = { installation, bot, token };
      step = "reveal";
      onInstalled(installation, bot, token);
    } catch (err) {
      error = createdBot
        ? `${integrationsLoadErrorMessage(err)} The bot and token were created — retrying will only create the installation.`
        : botLoadErrorMessage(err);
    } finally {
      submitting = false;
    }
  }

  function back() {
    error = "";
    if (step === "config") step = "bot";
    else if (step === "bot") step = "app";
  }

  const stepIndex = $derived(step === "app" ? 1 : step === "bot" ? 2 : step === "config" ? 3 : 4);
  const stepCount = $derived(hasConfigStep || step === "app" ? 4 : 3);

  onDestroy(() => {
    if (memberSearchTimer) clearTimeout(memberSearchTimer);
    memberSearchGeneration += 1;
  });
</script>

<section class="ws-intg__wizard" aria-label="Add app">
  <header class="ws-intg__wizard-header">
    <div>
      <h3 class="ws-bots__form-title">
        {#if manifest && step !== "app"}Install {manifest.name}{:else}Add app{/if}
      </h3>
      <p class="ws-bots__form-hint">
        {#if step === "app"}
          Pick what you're connecting. The app gets a bot identity in this workspace and a token it
          uses to connect in.
        {:else if step === "bot"}
          The bot is the app's identity here — its name, avatar, and the token it authenticates
          with.
        {:else if step === "config"}
          How the agent behaves in this workspace. This shapes the config you'll paste into the
          platform.
        {:else}
          The app is installed. Copy the token and setup below — this is the only time the raw
          token is visible.
        {/if}
      </p>
    </div>
    <span class="ws-intg__wizard-step">Step {stepIndex} of {stepCount}</span>
  </header>

  {#if step === "app"}
    <div class="ws-intg__catalog">
      {#each APP_CATALOG as entry (entry.slug)}
        <button type="button" class="ws-intg__catalog-card" onclick={() => pickManifest(entry)}>
          <span class="ws-intg__app-icon" aria-hidden="true">
            <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
              {#each entry.icon as path (path)}
                <path d={path} />
              {/each}
            </svg>
          </span>
          <span class="ws-intg__catalog-name">{entry.name}</span>
          <span class="ws-intg__catalog-desc">{entry.description}</span>
        </button>
      {/each}
    </div>
    <div class="ws-bots__form-actions">
      <button type="button" class="ws-btn" onclick={onClose}>Cancel</button>
    </div>
  {:else if step === "bot"}
    <form
      class="ws-intg__wizard-body"
      onsubmit={(event) => {
        event.preventDefault();
        advanceFromBot();
      }}
    >
      <fieldset class="ws-bots__form-field" disabled={hasPartialCredentials}>
        <legend class="ws-bots__form-label">Bot identity</legend>
        <div class="ws-bots__choices">
          <label class="ws-bots__choice" class:is-active={botMode === "create"}>
            <input type="radio" name="intg-bot-mode" value="create" bind:group={botMode} />
            <span class="ws-bots__choice-title">Create a new bot</span>
            <span class="ws-bots__choice-hint">Recommended. A fresh identity just for this app.</span>
          </label>
          <label
            class="ws-bots__choice"
            class:is-active={botMode === "existing"}
            class:is-disabled={availableBots.length === 0}
          >
            <input
              type="radio"
              name="intg-bot-mode"
              value="existing"
              bind:group={botMode}
              disabled={availableBots.length === 0}
            />
            <span class="ws-bots__choice-title">Use an existing bot</span>
            <span class="ws-bots__choice-hint">
              {availableBots.length === 0
                ? "No unbound bots in this workspace."
                : "Bind a bot that isn't attached to another app. A new token is minted for it."}
            </span>
          </label>
        </div>
      </fieldset>

      {#if botMode === "create"}
        <div class="ws-bots__form-grid">
          <label class="ws-bots__form-field">
            <span class="ws-bots__form-label">Display name</span>
            <input
              class="ws-bots__form-input"
              type="text"
              bind:value={displayName}
              placeholder={manifest?.suggestedBotName || "My app"}
              maxlength="80"
              required
              disabled={hasPartialCredentials}
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
                placeholder={manifest?.suggestedBotHandle || "my-app"}
                required
                disabled={hasPartialCredentials}
              />
            </div>
          </label>
        </div>

        <fieldset class="ws-bots__form-field" disabled={hasPartialCredentials}>
          <legend class="ws-bots__form-label">Ownership</legend>
          <div class="ws-bots__choices">
            <label class="ws-bots__choice" class:is-active={ownership === "service"}>
              <input type="radio" name="intg-ownership" value="service" bind:group={ownership} />
              <span class="ws-bots__choice-title">Service bot</span>
              <span class="ws-bots__choice-hint">
                Belongs to the workspace. Any owner or moderator can rotate its tokens.
              </span>
            </label>
            <label class="ws-bots__choice" class:is-active={ownership === "user"}>
              <input type="radio" name="intg-ownership" value="user" bind:group={ownership} />
              <span class="ws-bots__choice-title">User-owned bot</span>
              <span class="ws-bots__choice-hint">
                Belongs to you. Only you can rotate or revoke its tokens.
              </span>
            </label>
          </div>
        </fieldset>
      {:else}
        <label class="ws-bots__form-field">
          <span class="ws-bots__form-label">Bot</span>
          <select
            class="ws-bots__form-input"
            bind:value={existingBotID}
            required
            disabled={hasPartialCredentials}
          >
            <option value="" disabled>Pick a bot…</option>
            {#each availableBots as entry (entry.bot.id)}
              <option value={entry.bot.id}>
                {entry.bot.display_name || entry.bot.handle} (@{entry.bot.handle})
                {isServiceBot(entry.bot) ? " · service" : " · user-owned"}
                · {activeTokens(entry.tokens).length} active tokens
              </option>
            {/each}
          </select>
        </label>
      {/if}

      <fieldset class="ws-bots__form-field" disabled={hasPartialCredentials}>
        <legend class="ws-bots__form-label">Token scope</legend>
        <div class="ws-bots__choices">
          {#each BOT_SCOPE_BUNDLES as bundle (bundle.id)}
            <label class="ws-bots__choice" class:is-active={scopeBundle === bundle.id}>
              <input type="radio" name="intg-scope" value={bundle.id} bind:group={scopeBundle} />
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
          placeholder="production"
          maxlength="80"
          required
          disabled={hasPartialCredentials}
        />
      </label>

      {#if error}
        <p class="ws-bots__form-error" role="alert">{error}</p>
      {/if}

      <div class="ws-bots__form-actions">
        <button
          type="button"
          class="ws-btn"
          onclick={back}
          disabled={submitting || hasPartialCredentials}
        >
          Back
        </button>
        <button
          type="submit"
          class="ws-btn ws-btn--primary"
          disabled={!botStepValid || submitting}
        >
          {hasConfigStep ? "Continue" : submitting ? "Installing…" : "Install"}
        </button>
      </div>
    </form>
  {:else if step === "config" && manifest}
    <form
      class="ws-intg__wizard-body"
      onsubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <label class="ws-bots__form-field">
        <span class="ws-bots__form-label">Default channel</span>
        <select
          class="ws-bots__form-input"
          bind:value={defaultChannel}
          disabled={hasPartialCredentials || channelNames.length === 0}
        >
          {#each channelNames as name (name)}
            <option value={name}>#{name}</option>
          {/each}
        </select>
        <span class="ws-bots__form-hint">
          {channelNames.length === 0
            ? "Create or restore a channel before installing this app."
            : "Where the agent sends messages when no target is specified."}
        </span>
      </label>

      <fieldset class="ws-bots__form-field" disabled={hasPartialCredentials}>
        <legend class="ws-bots__form-label">Who can talk to this agent</legend>
        <div class="ws-bots__choices">
          <label class="ws-bots__choice" class:is-active={allowMode === "everyone"}>
            <input type="radio" name="intg-allow" value="everyone" bind:group={allowMode} />
            <span class="ws-bots__choice-title">Everyone in the workspace</span>
            <span class="ws-bots__choice-hint">Any member can message and mention the agent.</span>
          </label>
          <label class="ws-bots__choice" class:is-active={allowMode === "specific"}>
            <input type="radio" name="intg-allow" value="specific" bind:group={allowMode} />
            <span class="ws-bots__choice-title">Only specific members</span>
            <span class="ws-bots__choice-hint">
              The agent ignores everyone else. Enforced by the platform's allowFrom list.
            </span>
          </label>
        </div>
      </fieldset>

      {#if allowMode === "specific"}
        <div class="ws-bots__form-field">
          <span class="ws-bots__form-label">Allowed members</span>
          {#if allowMembers.length > 0}
            <div class="ws-intg__chips">
              {#each allowMembers as member (member.id)}
                <span class="ws-intg__chip">
                  {member.label}
                  <button
                    type="button"
                    class="ws-intg__chip-remove"
                    aria-label={`Remove ${member.label}`}
                    onclick={() => removeAllowMember(member.id)}
                    disabled={hasPartialCredentials}
                  >
                    ×
                  </button>
                </span>
              {/each}
            </div>
          {/if}
          <input
            class="ws-bots__form-input"
            type="search"
            placeholder="Search members by name or handle"
            value={memberQuery}
            oninput={onMemberQueryInput}
            disabled={hasPartialCredentials}
          />
          {#if memberResults.length > 0}
            <ul class="ws-intg__member-results">
              {#each memberResults as member (member.user.id)}
                <li>
                  <button
                    type="button"
                    class="ws-intg__member-result"
                    onclick={() => addAllowMember(member)}
                    disabled={hasPartialCredentials}
                  >
                    {member.user.display_name || member.user.handle}
                    <code class="ws-members__handle">@{member.user.handle}</code>
                  </button>
                </li>
              {/each}
            </ul>
          {/if}
        </div>
      {/if}

      <label class="ws-bots__form-field ws-intg__toggle">
        <input type="checkbox" bind:checked={agentActivity} disabled={hasPartialCredentials} />
        <span>
          <span class="ws-bots__choice-title">Stream agent activity</span>
          <span class="ws-bots__choice-hint">
            Show the agent's thinking and tool progress in the conversation as it works. Grants the
            <code>agent_activity:write</code> token scope.
          </span>
        </span>
      </label>

      {#if error}
        <p class="ws-bots__form-error" role="alert">{error}</p>
      {/if}

      <div class="ws-bots__form-actions">
        <button
          type="button"
          class="ws-btn"
          onclick={back}
          disabled={submitting || hasPartialCredentials}
        >
          Back
        </button>
        <button
          type="submit"
          class="ws-btn ws-btn--primary"
          disabled={!configStepValid || submitting}
        >
          {submitting ? "Installing…" : createdBot ? "Retry install" : "Install"}
        </button>
      </div>
    </form>
  {:else if step === "reveal" && result && manifest}
    <TokenRevealPanel
      token={result.token}
      botHandle={result.bot.handle}
      botUserID={result.bot.id}
      workspace={workspaceIdentifier}
      defaultTo={hasConfigStep ? defaultToValue : undefined}
      allowFrom={hasConfigStep ? allowFromValue : undefined}
      agentActivity={hasConfigStep ? agentActivity : undefined}
      configSnippetBuilder={manifest.buildConfigSnippet}
      shellSnippetBuilder={manifest.buildShellSnippet}
      onDismiss={onClose}
    />
  {/if}
</section>

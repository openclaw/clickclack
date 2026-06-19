<script lang="ts">
  import { goto } from "$app/navigation";
  import { onMount } from "svelte";
  import { listMyBots, botLoadErrorMessage, type OwnedBotEntry } from "../../lib/bots";
  import { workspaceSettingsPath } from "../../lib/settings";

  type Props = {
    onClose: () => void;
  };

  let { onClose }: Props = $props();

  let bots = $state<OwnedBotEntry[]>([]);
  let status = $state<"loading" | "ready" | "error">("loading");
  let error = $state("");

  onMount(() => {
    void refresh();
  });

  async function refresh() {
    status = "loading";
    try {
      bots = await listMyBots();
      status = "ready";
    } catch (err) {
      status = "error";
      error = botLoadErrorMessage(err);
    }
  }

  function openWorkspaceBots(entry: OwnedBotEntry) {
    onClose();
    void goto(workspaceSettingsPath(entry.workspace.route_id || entry.workspace.id, "bots"));
  }

  function formatHandle(handle: string): string {
    return handle.startsWith("@") ? handle : `@${handle}`;
  }

  function initials(name: string): string {
    const parts = name.trim().split(/\s+/).filter(Boolean);
    if (parts.length >= 2) return (parts[0]![0]! + parts[1]![0]!).toUpperCase();
    return name.slice(0, 2).toUpperCase();
  }

  type Group = { workspace: OwnedBotEntry["workspace"]; entries: OwnedBotEntry[] };

  const groups = $derived.by<Group[]>(() => {
    const map = new Map<string, Group>();
    for (const entry of bots) {
      const key = entry.workspace.id;
      const existing = map.get(key);
      if (existing) {
        existing.entries.push(entry);
      } else {
        map.set(key, { workspace: entry.workspace, entries: [entry] });
      }
    }
    return [...map.values()].sort((a, b) => a.workspace.name.localeCompare(b.workspace.name));
  });
</script>

<header class="settings-page__header">
  <p class="settings-page__eyebrow">Account</p>
  <h2 class="settings-page__h1">My bots</h2>
  <p class="settings-page__lead">
    Bots you own across your workspaces. Tokens live with the workspace where the bot was created.
  </p>
</header>

{#if status === "loading"}
  <p class="settings-status">Loading…</p>
{:else if status === "error"}
  <p class="settings-status is-error">{error}</p>
{:else if bots.length === 0}
  <div class="ws-bots__empty">
    You don't own any bots yet. Open a workspace's Bots &amp; agents page to mint one.
  </div>
{:else}
  <div class="ws-bots__my-list">
    {#each groups as group (group.workspace.id)}
      <section class="ws-bots__my-group">
        <header class="ws-bots__my-group-header">
          <h3 class="ws-bots__my-group-title">{group.workspace.name}</h3>
          <button
            type="button"
            class="ws-btn"
            onclick={() => openWorkspaceBots(group.entries[0]!)}
          >
            Manage in workspace
          </button>
        </header>
        <ul class="ws-bots__my-rows">
          {#each group.entries as entry (entry.bot.id)}
            <li class="ws-bots__my-row">
              <span
                class="ws-members__avatar ws-members__avatar--bot"
                aria-hidden="true"
              >
                {#if entry.bot.avatar_url}
                  <img src={entry.bot.avatar_url} alt="" />
                {:else}
                  {initials(entry.bot.display_name || entry.bot.handle || "?")}
                {/if}
              </span>
              <div class="ws-bots__my-row-text">
                <div class="ws-bots__my-row-name">{entry.bot.display_name || entry.bot.handle}</div>
                <div class="ws-bots__my-row-meta">
                  <code class="ws-members__handle">{formatHandle(entry.bot.handle)}</code>
                  <span class="ws-members__dot" aria-hidden="true">·</span>
                  <span>
                    {entry.active_token_count}
                    active {entry.active_token_count === 1 ? "token" : "tokens"}
                  </span>
                </div>
              </div>
              <button
                type="button"
                class="ws-btn"
                onclick={() => openWorkspaceBots(entry)}
              >
                Open
              </button>
            </li>
          {/each}
        </ul>
      </section>
    {/each}
  </div>
{/if}

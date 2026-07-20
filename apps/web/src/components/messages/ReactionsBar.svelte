<script lang="ts">
  import { untrack } from "svelte";
  import { api } from "../../lib/api";
  import type { ReactionSummary } from "../../lib/types";

  let { messageId, reactions = [], currentUserID = "" }: {
    messageId: string;
    reactions: ReactionSummary[];
    currentUserID?: string;
  } = $props();

  let localReactions = $state<ReactionSummary[]>([]);
  let pendingEmojis = $state(new Set<string>());
  let pendingIntents = $state(new Map<string, "add" | "remove">());
  let errorMessage = $state("");
  let authoritativeReactions: ReactionSummary[] = [];
  let authoritativeRevision = 0;
  let authoritativeUserEmojis = new Set<string>();
  let authoritativeEmojiRevisions = new Map<string, number>();

  function applyPendingIntents(
    authoritative: ReactionSummary[],
    intents: Map<string, "add" | "remove">,
  ) {
    let next = authoritative.map((reaction) => ({ ...reaction }));
    for (const [emoji, intent] of intents) {
      const index = next.findIndex((reaction) => reaction.emoji === emoji);
      if (intent === "remove") {
        if (index < 0 || !next[index].reacted_by_me) continue;
        if (next[index].count <= 1) {
          next.splice(index, 1);
        } else {
          next[index] = {
            ...next[index],
            count: next[index].count - 1,
            reacted_by_me: false,
          };
        }
      } else if (index < 0) {
        next.push({ emoji, count: 1, reacted_by_me: true });
      } else if (!next[index].reacted_by_me) {
        next[index] = {
          ...next[index],
          count: next[index].count + 1,
          reacted_by_me: true,
        };
      }
    }
    return next;
  }

  function replaceAuthoritativeReactions(nextReactions: ReactionSummary[]) {
    const nextUserEmojis = new Set(
      nextReactions
        .filter((reaction) => reaction.reacted_by_me)
        .map((reaction) => reaction.emoji),
    );
    const changedEmojis = new Set([...authoritativeUserEmojis, ...nextUserEmojis]);
    const nextEmojiRevisions = new Map(authoritativeEmojiRevisions);
    for (const emoji of changedEmojis) {
      if (authoritativeUserEmojis.has(emoji) !== nextUserEmojis.has(emoji)) {
        nextEmojiRevisions.set(emoji, (nextEmojiRevisions.get(emoji) ?? 0) + 1);
      }
    }
    authoritativeUserEmojis = nextUserEmojis;
    authoritativeEmojiRevisions = nextEmojiRevisions;
    authoritativeReactions = nextReactions;
    authoritativeRevision += 1;
  }

  $effect(() => {
    replaceAuthoritativeReactions(reactions);
    localReactions = applyPendingIntents(authoritativeReactions, untrack(() => pendingIntents));
  });

  let groupedEntries = $derived(
    [...localReactions].sort(
      (a, b) => b.count - a.count || a.emoji.localeCompare(b.emoji),
    ),
  );

  let showPicker = $state(false);
  let pickerRef = $state<HTMLDivElement>();
  let pickerId = $derived(`reaction-picker-${messageId}`);

  const QUICK_EMOJIS = ["👍", "❤️", "😂", "😮", "😢", "🙏", "🎉", "🔥", "💯", "👀", "🚀", "✅"];

  async function toggleReaction(emoji: string) {
    if (!currentUserID || pendingEmojis.size > 0) return;
    const existing = localReactions.find((reaction) => reaction.emoji === emoji)?.reacted_by_me;
    const intent = existing ? "remove" : "add";
    localReactions = applyPendingIntents(localReactions, new Map([[emoji, intent]]));
    pendingEmojis = new Set([...pendingEmojis, emoji]);
    pendingIntents = new Map(pendingIntents).set(emoji, intent);
    errorMessage = "";
    const emojiRevisionAtStart = authoritativeEmojiRevisions.get(emoji) ?? 0;
    try {
      if (existing) {
        await api(`/api/messages/${messageId}/reactions/${encodeURIComponent(emoji)}`, {
          method: "DELETE",
        });
      } else {
        await api(`/api/messages/${messageId}/reactions`, {
          method: "POST",
          body: JSON.stringify({ emoji }),
        });
      }
      if ((authoritativeEmojiRevisions.get(emoji) ?? 0) === emojiRevisionAtStart) {
        authoritativeReactions = applyPendingIntents(
          authoritativeReactions,
          new Map([[emoji, intent]]),
        );
        authoritativeUserEmojis = new Set(authoritativeUserEmojis);
        if (intent === "add") authoritativeUserEmojis.add(emoji);
        else authoritativeUserEmojis.delete(emoji);
        authoritativeEmojiRevisions = new Map(authoritativeEmojiRevisions).set(
          emoji,
          emojiRevisionAtStart + 1,
        );
        authoritativeRevision += 1;
      }
      const refreshRevision = authoritativeRevision;
      try {
        const data = await api<{ message: { reactions?: ReactionSummary[] } }>(
          `/api/messages/${messageId}`,
        );
        if (authoritativeRevision === refreshRevision) {
          replaceAuthoritativeReactions(data.message.reactions ?? []);
        }
      } catch {
        // The mutation committed; retain the known intent or a newer realtime snapshot.
      }
      localReactions = applyPendingIntents(authoritativeReactions, pendingIntents);
    } catch (e) {
      const remainingIntents = new Map(pendingIntents);
      remainingIntents.delete(emoji);
      const recoveryRevision = authoritativeRevision;
      try {
        const data = await api<{ message: { reactions?: ReactionSummary[] } }>(
          `/api/messages/${messageId}`,
        );
        if (authoritativeRevision === recoveryRevision) {
          replaceAuthoritativeReactions(data.message.reactions ?? []);
        }
      } catch {
        // Fall back to the latest realtime snapshot below.
      }
      localReactions = applyPendingIntents(authoritativeReactions, remainingIntents);
      errorMessage = e instanceof Error ? e.message : "Could not update reaction";
    } finally {
      const next = new Set(pendingEmojis);
      next.delete(emoji);
      pendingEmojis = next;
      const nextIntents = new Map(pendingIntents);
      nextIntents.delete(emoji);
      pendingIntents = nextIntents;
      localReactions = applyPendingIntents(authoritativeReactions, nextIntents);
    }
  }

  function handleClickOutside(e: MouseEvent) {
    if (pickerRef && !pickerRef.contains(e.target as Node)) {
      showPicker = false;
    }
  }

  $effect(() => {
    if (showPicker) {
      document.addEventListener("click", handleClickOutside);
      return () => document.removeEventListener("click", handleClickOutside);
    }
  });
</script>

<div class="reactions-bar">
  {#each groupedEntries as { emoji, count, reacted_by_me }}
    <button
      class="reaction-btn"
      class:me={reacted_by_me}
      onclick={() => toggleReaction(emoji)}
      disabled={pendingEmojis.size > 0}
      aria-pressed={reacted_by_me}
      aria-label="{emoji} — {count} reaction{count !== 1 ? 's' : ''}"
      title="{emoji}"
    >
      <span class="reaction-emoji">{emoji}</span>
      {#if count > 1}
        <span class="reaction-count">{count}</span>
      {/if}
    </button>
  {/each}

  <div class="picker-wrapper" bind:this={pickerRef}>
    <button
      class="reaction-btn add-btn"
      onclick={() => (showPicker = !showPicker)}
      aria-label="Add reaction"
      aria-controls={pickerId}
      aria-expanded={showPicker}
      aria-haspopup="menu"
      disabled={pendingEmojis.size > 0}
    >
      <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
        <path d="M8 3v10M3 8h10" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
      </svg>
    </button>

    {#if showPicker}
      <div class="emoji-grid" id={pickerId} role="menu" aria-label="Choose a reaction">
        {#each QUICK_EMOJIS as emoji}
          <button
            class="emoji-option"
            role="menuitem"
            onclick={() => {
              void toggleReaction(emoji);
              showPicker = false;
            }}
            aria-label={`React with ${emoji}`}
            title={emoji}
            disabled={pendingEmojis.size > 0}
          >
            {emoji}
          </button>
        {/each}
      </div>
    {/if}
  </div>
  {#if errorMessage}<span class="reaction-error" role="status">{errorMessage}</span>{/if}
</div>

<style>
  .reactions-bar {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    align-items: center;
    margin-top: 4px;
    min-height: 28px;
  }

  .reaction-btn {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    padding: 2px 6px;
    border: 1px solid var(--border, color-mix(in srgb, var(--line, #e0e0e0) 50%, transparent));
    border-radius: var(--radius, 8px);
    background: transparent;
    cursor: pointer;
    font-size: 13px;
    line-height: 1.4;
    transition: background 0.1s, border-color 0.1s;
    color: var(--text-muted, #666);
  }

  .reaction-btn:hover {
    background: color-mix(in srgb, var(--accent, #5865f2) 10%, transparent);
    border-color: color-mix(in srgb, var(--accent, #5865f2) 30%, transparent);
  }

  .reaction-btn:disabled {
    cursor: wait;
    opacity: 0.55;
  }

  .reaction-btn.me {
    background: color-mix(in srgb, var(--accent, #5865f2) 15%, transparent);
    border-color: color-mix(in srgb, var(--accent, #5865f2) 40%, transparent);
  }

  .reaction-emoji {
    font-size: 14px;
    line-height: 1;
  }

  .reaction-count {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-muted, #666);
  }

  .reaction-btn.me .reaction-count {
    color: var(--accent, #5865f2);
  }

  .add-btn {
    opacity: 0.6;
    padding: 2px 4px;
  }

  .add-btn:hover {
    opacity: 1;
  }

  .picker-wrapper {
    position: relative;
  }

  .emoji-grid {
    position: absolute;
    bottom: 100%;
    left: 0;
    margin-bottom: 4px;
    display: grid;
    grid-template-columns: repeat(6, 1fr);
    gap: 2px;
    padding: 6px;
    background: var(--panel, #fff);
    border: 1px solid var(--border, #e0e0e0);
    border-radius: var(--radius, 8px);
    box-shadow: 0 4px 12px rgba(0,0,0,0.12);
    z-index: 100;
    min-width: 200px;
  }

  .emoji-option {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    padding: 0;
    border: none;
    border-radius: 4px;
    background: transparent;
    cursor: pointer;
    font-size: 18px;
    line-height: 1;
    transition: background 0.1s;
  }

  .emoji-option:hover {
    background: color-mix(in srgb, var(--accent, #5865f2) 12%, transparent);
  }

  .reaction-error {
    color: var(--danger, #b42318);
    font-size: 12px;
  }
</style>

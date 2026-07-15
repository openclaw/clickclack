<script lang="ts">
  import Avatar from "../avatar/Avatar.svelte";
  import { handleLabel, isDeletedBot, userHandle } from "../../lib/chat/people";
  import type { SearchHighlight, SearchResult, SearchSession } from "../../lib/types";

  type Props = {
    session: SearchSession;
    covered?: boolean;
    inert?: boolean;
    contextFor: (result: SearchResult) => string;
    onClose: () => void;
    onOpenResult: (result: SearchResult) => void;
    onLoadMore: () => void;
  };

  let { session, covered = false, inert = false, contextFor, onClose, onOpenResult, onLoadMore }: Props = $props();
  const timeFormatter = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" });
  const dayFormatter = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });
  const yearFormatter = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", year: "numeric" });

  function snippetParts(snippet: string, highlights: SearchHighlight[]) {
    const characters = Array.from(snippet);
    const parts: { text: string; highlighted: boolean }[] = [];
    let position = 0;
    for (const highlight of highlights) {
      const start = Math.max(position, Math.min(characters.length, highlight.start));
      const end = Math.max(start, Math.min(characters.length, highlight.end));
      if (start > position) parts.push({ text: characters.slice(position, start).join(""), highlighted: false });
      if (end > start) parts.push({ text: characters.slice(start, end).join(""), highlighted: true });
      position = end;
    }
    if (position < characters.length) parts.push({ text: characters.slice(position).join(""), highlighted: false });
    return parts;
  }

  function resultTimestamp(value: string): string {
    const date = new Date(value);
    const now = new Date();
    if (date.toDateString() === now.toDateString()) {
      return timeFormatter.format(date);
    }
    if (date.getFullYear() === now.getFullYear()) {
      return dayFormatter.format(date);
    }
    return yearFormatter.format(date);
  }
</script>

<aside
  class="search-results"
  class:covered
  {inert}
  aria-hidden={covered ? "true" : undefined}
  aria-label="Search results"
>
  <header class="search-results-head">
    <div>
      <p>Search{session.scope.label ? ` in ${session.scope.label}` : ""}</p>
      <strong>Results for “{session.query}”</strong>
    </div>
    <button type="button" class="close" aria-label="Close search panel" onclick={onClose}>&times;</button>
  </header>

  <div class="search-results-summary" aria-live="polite">
    {#if session.state === "loading"}
      Searching messages…
    {:else if session.state === "ready"}
      {session.results.length}{session.nextCursor ? "+" : ""}{" "}
      {session.results.length === 1 && !session.nextCursor ? "result" : "results"}{#if session.scope.label}
        in {session.scope.label}
      {/if}
    {:else if session.state === "error"}
      Search unavailable
    {/if}
  </div>

  <div class="search-results-scroll">
    {#if session.state === "loading"}
      <div class="search-state search-state-loading" role="status" aria-label="Searching">
        <span></span><span></span><span></span>
      </div>
    {:else if session.state === "error"}
      <div class="search-state">
        <span class="search-state-icon" aria-hidden="true">!</span>
        <strong>We couldn’t search messages</strong>
        <p>{session.error}</p>
      </div>
    {:else if session.results.length === 0}
      <div class="search-state">
        <span class="search-state-icon" aria-hidden="true">⌕</span>
        <strong>No messages found</strong>
        <p>Try another word or phrase.</p>
      </div>
    {:else}
      <ul class="search-result-list">
        {#each session.results as result (result.id)}
          {@const context = contextFor(result)}
          <li>
            <button
              type="button"
              class="search-result"
              class:is-active={session.activeResultID === result.id}
              data-result-id={result.id}
              onclick={() => onOpenResult(result)}
            >
              <Avatar
                class="dm-avatar"
                id={result.author.id}
                name={result.author.display_name}
                src={isDeletedBot(result.author) ? undefined : result.author.avatar_url}
                size={30}
              />
              <div class="search-result-body">
                <div class="search-result-meta">
                  <strong>{result.author.display_name || "Local User"}</strong>
                  {#if isDeletedBot(result.author)}
                    <span class="bot-chip bot-chip--deleted">deleted bot</span>
                  {:else if userHandle(result.author)}
                    <span class="search-result-handle">{handleLabel(userHandle(result.author))}</span>
                  {/if}
                  <time datetime={result.created_at}>{resultTimestamp(result.created_at)}</time>
                </div>
                <span class="search-result-snippet">
                  {#each snippetParts(result.snippet, result.highlights) as part}
                    {#if part.highlighted}<mark>{part.text}</mark>{:else}{part.text}{/if}
                  {/each}
                </span>
                <div class="search-result-context">
                  {#if context}
                    <span class="search-result-where">{context}</span>
                  {/if}
                  {#if result.parent_message_id}
                    <span class="search-result-thread">
                      <svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true">
                        <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M21 12a8 8 0 0 1-8 8H7l-4 3V12a8 8 0 0 1 8-8h2a8 8 0 0 1 8 8z"/>
                      </svg>
                      Reply in thread
                    </span>
                  {:else if result.reply_count > 0}
                    <span class="search-result-thread">
                      <svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true">
                        <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M21 12a8 8 0 0 1-8 8H7l-4 3V12a8 8 0 0 1 8-8h2a8 8 0 0 1 8 8z"/>
                      </svg>
                      {result.reply_count} {result.reply_count === 1 ? "reply" : "replies"}
                    </span>
                  {/if}
                </div>
              </div>
            </button>
          </li>
        {/each}
      </ul>
      {#if session.moreError}
        <div class="search-foot-error" role="alert">
          <p>{session.moreError}</p>
          <button type="button" onclick={onLoadMore}>Retry</button>
        </div>
      {:else if session.nextCursor}
        <button
          type="button"
          class="search-load-more"
          disabled={session.loadingMore}
          onclick={onLoadMore}
        >
          {session.loadingMore ? "Loading more…" : "Load more results"}
        </button>
      {:else}
        <div class="search-end" aria-hidden="true">End of results</div>
      {/if}
    {/if}
  </div>
</aside>

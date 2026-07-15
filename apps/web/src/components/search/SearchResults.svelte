<script lang="ts">
  import Avatar from "../avatar/Avatar.svelte";
  import { handleLabel, isDeletedBot, userHandle } from "../../lib/chat/people";
  import { time } from "../../lib/format";
  import type { SearchHighlight, SearchResult } from "../../lib/types";

  type Props = {
    results: SearchResult[];
    onClose: () => void;
    onOpenResult: (result: SearchResult) => void;
  };

  let { results, onClose, onOpenResult }: Props = $props();

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
</script>

{#if results.length > 0}
  <div class="search-results" aria-label="Search results">
    <div class="search-results-head">
      <strong>{results.length} {results.length === 1 ? "result" : "results"}</strong>
      <button type="button" onclick={onClose}>Close</button>
    </div>
    {#each results as result (result.id)}
      <button class="search-result" onclick={() => onOpenResult(result)}>
        <Avatar
          class="dm-avatar"
          id={result.author.id}
          name={result.author.display_name}
          src={isDeletedBot(result.author) ? undefined : result.author.avatar_url}
          size={30}
        />
        <div class="search-result-body">
          <div>
            <strong>{result.author.display_name || "Local User"}</strong>
            {#if isDeletedBot(result.author)}
              <span class="bot-chip bot-chip--deleted">deleted bot</span>
            {/if}
            {#if userHandle(result.author)}
              <span>{handleLabel(userHandle(result.author))}</span>
            {/if}
            <time>{time(result.created_at)}</time>
          </div>
          <span class="search-result-snippet">
            {#each snippetParts(result.snippet, result.highlights) as part}
              {#if part.highlighted}<mark>{part.text}</mark>{:else}{part.text}{/if}
            {/each}
          </span>
        </div>
      </button>
    {/each}
  </div>
{/if}

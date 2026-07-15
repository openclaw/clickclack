<script lang="ts">
  import Avatar from "../avatar/Avatar.svelte";
  import { handleLabel, isDeletedBot, userHandle } from "../../lib/chat/people";
  import { time } from "../../lib/format";
  import type { SearchResult } from "../../lib/types";

  type Props = {
    results: SearchResult[];
    onClose: () => void;
    onOpenResult: (result: SearchResult) => void;
  };

  let { results, onClose, onOpenResult }: Props = $props();
</script>

{#if results.length > 0}
  <div class="search-results" aria-label="Search results">
    <div class="search-results-head">
      <strong>{results.length} {results.length === 1 ? "result" : "results"}</strong>
      <button type="button" onclick={onClose}>Close</button>
    </div>
    {#each results as result (result.message.id)}
      <button class="search-result" onclick={() => onOpenResult(result)}>
        <Avatar
          class="dm-avatar"
          id={result.message.author?.id || result.message.author_id}
          name={result.message.author?.display_name}
          src={isDeletedBot(result.message.author) ? undefined : result.message.author?.avatar_url}
          size={30}
        />
        <div class="search-result-body">
          <div>
            <strong>{result.message.author?.display_name || "Local User"}</strong>
            {#if isDeletedBot(result.message.author)}
              <span class="bot-chip bot-chip--deleted">deleted bot</span>
            {/if}
            {#if userHandle(result.message.author)}
              <span>{handleLabel(userHandle(result.message.author))}</span>
            {/if}
            <time>{time(result.message.created_at)}</time>
          </div>
          <span>{result.message.body}</span>
        </div>
      </button>
    {/each}
  </div>
{/if}

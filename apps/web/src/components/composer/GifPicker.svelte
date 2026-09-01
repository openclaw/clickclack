<script lang="ts">
  import { onMount } from "svelte";
  import type { GifItem } from "../../lib/gifs";

  type Props = {
    gifs: GifItem[];
    query: string;
    onQuery: (value: string) => void;
    onPick: (url: string, title: string) => void;
    onClose: () => void;
  };

  let { gifs, query, onQuery, onPick, onClose }: Props = $props();
  let search = $state<HTMLInputElement>();
  onMount(() => search?.focus());
</script>

<section class="gif-picker" role="dialog" tabindex="-1" aria-label="GIF picker panel"
  onkeydown={(event) => {
    if (event.isComposing || event.keyCode === 229) return;
    if (event.key === "Escape") {
      event.preventDefault();
      onClose();
    }
  }}
>
  <div class="gif-picker-head">
    <strong>GIFs</strong>
    <input
      bind:this={search}
      value={query}
      placeholder="Search reactions"
      aria-label="Search GIFs"
      oninput={(event) => onQuery(event.currentTarget.value)}
    />
  </div>
  <div class="gif-grid">
    {#each gifs as gif (gif.url)}
      <button type="button" onclick={() => onPick(gif.url, gif.title)}>
        <img src={gif.url} alt={gif.title} loading="lazy" />
        <span>{gif.title}</span>
      </button>
    {/each}
  </div>
  {#if gifs.length === 0}<p role="status">No GIFs found</p>{/if}
</section>

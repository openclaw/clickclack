<script lang="ts">
  import { onMount } from "svelte";
  import { initAppearance } from "$lib/appearance";
  import { installEmbedHostTheme } from "$lib/embed-theme";
  import "../styles/index.css";

  let { children } = $props();

  onMount(() => {
    initAppearance();
    return installEmbedHostTheme();
  });
</script>

{@render children()}

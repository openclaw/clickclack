<script lang="ts">
  import { afterNavigate } from "$app/navigation";
  import { onMount } from "svelte";
  import { applyColorMode, initAppearance, loadColorMode } from "$lib/appearance";
  import {
    clearEmbedHostTheme,
    installEmbedHostTheme,
    resolveEmbedHostOrigin,
  } from "$lib/embed-theme";
  import "../styles/index.css";

  let { children } = $props();

  onMount(() => {
    initAppearance();
    return installEmbedHostTheme();
  });

  afterNavigate(() => {
    if (resolveEmbedHostOrigin(window.location) || !clearEmbedHostTheme()) return;

    // The root layout survives embed-to-app navigation; restore the account's
    // own appearance immediately instead of retaining the parent palette.
    applyColorMode(loadColorMode());
  });
</script>

{@render children()}

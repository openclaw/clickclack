<script lang="ts">
  import { afterNavigate } from "$app/navigation";
  import { onMount } from "svelte";
  import { applyColorMode, initAppearance, loadColorMode } from "$lib/appearance";
  import { clearEmbedHostTheme, installEmbedHostTheme } from "$lib/embed-theme";
  import "../styles/index.css";

  let { children } = $props();
  let uninstallEmbedHostTheme = () => {};

  onMount(() => {
    initAppearance();
    return () => {
      uninstallEmbedHostTheme();
      clearEmbedHostTheme();
    };
  });

  afterNavigate(() => {
    // The root layout survives navigation in both directions. Rebind the exact
    // current host, clear its old palette, and restore account or embed mode.
    uninstallEmbedHostTheme();
    clearEmbedHostTheme();
    uninstallEmbedHostTheme = installEmbedHostTheme();
    applyColorMode(loadColorMode());
  });
</script>

{@render children()}

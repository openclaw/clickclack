<script lang="ts">
  import { afterNavigate } from "$app/navigation";
  import { onMount } from "svelte";
  import { applyColorMode, initAppearance, loadColorMode } from "$lib/appearance";
  import { installDisplayModeTracking } from "$lib/displayMode";
  import { clearEmbedHostTheme, installEmbedHostTheme } from "$lib/embed-theme";
  import { registerClickClackServiceWorker } from "$lib/pwa";
  import "../styles/index.css";

  let { children } = $props();
  let uninstallEmbedHostTheme = () => {};

  onMount(() => {
    initAppearance();
    const uninstallDisplayModeTracking = installDisplayModeTracking();
    void registerClickClackServiceWorker();
    return () => {
      uninstallDisplayModeTracking();
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

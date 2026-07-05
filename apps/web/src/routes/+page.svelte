<script lang="ts">
  // Host-conditional dynamic import so each surface is code-split: marketing
  // visitors never download the chat app bundle, and app users never load
  // product.css (whose selectors would otherwise share the app's global CSS
  // namespace).
  const isAppHost = window.location.hostname.startsWith("app.");
  const view = isAppHost ? import("../ChatApp.svelte") : import("../ProductSite.svelte");
</script>

{#await view then module}
  {@const View = module.default}
  <View />
{/await}

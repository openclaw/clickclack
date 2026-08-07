<script lang="ts">
  import "../styles/tokens.css";
  import "../styles/chassis.css";
  import SemanticMargin from "$lib/components/SemanticMargin.svelte";
  import CommandPalette from "$lib/components/CommandPalette.svelte";
  import { inspectMode, telemetryOpen } from "$lib/ui";

  let mounted = false;
  $effect(() => {
    mounted = true;
    const setInspect = (down: boolean) => inspectMode.set(down);
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.altKey) setInspect(true);
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        document.dispatchEvent(new CustomEvent("logos:palette"));
      }
    };
    const onKeyUp = (e: KeyboardEvent) => {
      if (!e.altKey) setInspect(false);
    };
    window.addEventListener("keydown", onKeyDown);
    window.addEventListener("keyup", onKeyUp);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("keyup", onKeyUp);
    };
  });
</script>

<div class="logos-shell" class:inspect={$inspectMode} class:telemetry={$telemetryOpen}>
  <SemanticMargin />
  <main class="logos-main">
    <slot />
  </main>
  <CommandPalette />
</div>

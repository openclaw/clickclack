<script lang="ts">
  import "../styles/tokens.css";
  import "../styles/chassis.css";
  import SemanticMargin from "$lib/components/SemanticMargin.svelte";
  import CommandPalette from "$lib/components/CommandPalette.svelte";
  import TelemetryRail from "$lib/components/TelemetryRail.svelte";
  import { inspectMode, telemetryOpen } from "$lib/ui";
  import { telemetrySnapshot, marginSnapshot } from "$lib/telemetry";

  let mounted = false;
  let tdata = $state($telemetrySnapshot);
  let mdata = $state($marginSnapshot);

  $effect(() => {
    const unsubTelemetry = telemetrySnapshot.subscribe((v) => (tdata = v));
    const unsubMargin = marginSnapshot.subscribe((v) => (mdata = v));
    return () => {
      unsubTelemetry();
      unsubMargin();
    };
  });

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
  <SemanticMargin messageCount={mdata.messageCount} intents={mdata.intents} />
  <main class="logos-main">
    <slot />
  </main>
  {#if $telemetryOpen}
    <TelemetryRail
      intents={tdata.intents}
      personas={tdata.personas}
      pipeline={tdata.pipeline}
      tokens={tdata.tokens}
    />
  {/if}
  <CommandPalette />
</div>

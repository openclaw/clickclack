<script lang="ts">
  /**
   * PROJECT LOGOS — TelemetryRail (Inspection + Telemetry Track)
   *
   * Right-hand rail (col 3 in the chassis grid). Shown when telemetryOpen
   * store is true. Four mono indicators with 2px --accent-thread left border.
   *
   * INS: Intent parser signatures   |  PER: Active persona count
   * PPL: Pipeline status             |  TKN: Total tokens so far
   */

  interface Props {
    intents?: number | null;
    personas?: number | null;
    pipeline?: string | null;
    tokens?: number | null;
  }

  let { intents = null, personas = null, pipeline = null, tokens = null }: Props = $props();

  const fmtIntents = $derived(intents != null ? String(intents) : "--");
  const fmtPersonas = $derived(personas != null ? String(personas) : "--");
  const fmtPipeline = $derived(pipeline ?? "--");
  const fmtTokens = $derived(tokens != null ? tokens.toLocaleString() : "--");
</script>

<div class="telemetry-rail" aria-label="Telemetry rail">
  <div class="rail-indicator">
    <span class="rail-label">INS</span>
    <span class="rail-value">{fmtIntents}</span>
  </div>
  <div class="rail-indicator">
    <span class="rail-label">PER</span>
    <span class="rail-value">{fmtPersonas}</span>
  </div>
  <div class="rail-indicator">
    <span class="rail-label">PPL</span>
    <span class="rail-value">{fmtPipeline}</span>
  </div>
  <div class="rail-indicator">
    <span class="rail-label">TKN</span>
    <span class="rail-value">{fmtTokens}</span>
  </div>
</div>

<style>
  .telemetry-rail {
    display: flex;
    flex-direction: column;
    gap: 0;
    padding: 0;
    background: var(--rail);
    font-family: var(--font-mono);
    min-height: 0;
  }

  .rail-indicator {
    display: grid;
    grid-template-columns: 1fr;
    grid-template-rows: auto auto;
    gap: 2px;
    padding: 10px 12px 8px;
    border-bottom: 1px solid var(--line);
    border-left: 2px solid var(--accent-thread);
  }

  .rail-label {
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.08em;
    color: var(--muted-2);
  }

  .rail-value {
    font-size: 14px;
    font-weight: 600;
    color: var(--text-strong);
    letter-spacing: 0.04em;
    word-break: break-all;
  }
</style>

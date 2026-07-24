<script lang="ts">
  type Props = {
    description?: string;
  };

  let { description }: Props = $props();
</script>

{#if description}
  <details class="channel-purpose">
    <summary title={description} aria-label={`Purpose: ${description}`}>
      <span class="channel-purpose-label">Purpose</span>
      <span aria-hidden="true">·</span>
      <span class="channel-purpose-preview">{description}</span>
    </summary>
    <p>{description}</p>
  </details>
{/if}

<style>
  .channel-purpose {
    min-width: 0;
    color: var(--muted);
    font-size: 11px;
    line-height: 1.25;
  }

  .channel-purpose summary {
    display: flex;
    min-width: 0;
    align-items: baseline;
    gap: 5px;
    cursor: pointer;
    list-style: none;
  }

  .channel-purpose summary::-webkit-details-marker {
    display: none;
  }

  .channel-purpose summary:focus-visible {
    border-radius: 3px;
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .channel-purpose-label {
    flex: none;
    color: var(--text);
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }

  .channel-purpose-preview {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .channel-purpose p {
    margin: 4px 0 0;
    color: var(--text);
    line-height: 1.35;
    overflow-wrap: anywhere;
  }
</style>

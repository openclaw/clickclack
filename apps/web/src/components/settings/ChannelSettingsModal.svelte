<script lang="ts">
  import { channelDisplayTitle } from "../../lib/chat/channels";
  import type { Channel } from "../../lib/types";

  type Props = {
    channel: Channel;
    saving?: boolean;
    error?: string;
    onClose: () => void;
    onArchivedChange: (archived: boolean) => void;
  };

  let {
    channel,
    saving = false,
    error = "",
    onClose,
    onArchivedChange,
  }: Props = $props();

  let confirmingArchive = $state(false);
  const archived = $derived(Boolean(channel.archived_at));
  const channelTitle = $derived(`#${channelDisplayTitle(channel)}`);

  function close() {
    if (!saving) onClose();
  }
</script>

<div class="modal-scrim channel-settings-scrim" role="presentation">
  <button
    class="modal-backdrop"
    type="button"
    aria-label="Close channel settings"
    disabled={saving}
    onclick={close}
  ></button>
  <div
    class="profile-modal channel-settings-modal"
    role="dialog"
    aria-modal="true"
    aria-labelledby="channel-settings-title"
    aria-describedby="channel-settings-description"
  >
    <header>
      <div>
        <p>Channel</p>
        <h2 id="channel-settings-title">Channel settings</h2>
      </div>
      <button type="button" aria-label="Close channel settings" disabled={saving} onclick={close}>
        &times;
      </button>
    </header>

    <div class="channel-settings-content">
      <div>
        <strong class="channel-settings-name">{channelTitle}</strong>
        <p id="channel-settings-description">
          Archived channels move to the Archived section and keep their full message history.
        </p>
      </div>

      {#if error}
        <p class="profile-status error" role="alert">{error}</p>
      {/if}

      {#if archived}
        <div class="profile-actions channel-settings-actions">
          <button type="button" class="ghost-action" disabled={saving} onclick={close}>
            Cancel
          </button>
          <button
            type="button"
            class="primary-action"
            disabled={saving}
            onclick={() => onArchivedChange(false)}
          >
            {saving ? "Restoring..." : "Restore channel"}
          </button>
        </div>
      {:else if confirmingArchive}
        <p class="channel-settings-warning">
          Archive {channelTitle}? The channel will remain available to workspace members under Archived.
        </p>
        <div class="profile-actions channel-settings-actions">
          <button
            type="button"
            class="ghost-action"
            disabled={saving}
            onclick={() => (confirmingArchive = false)}
          >
            Cancel
          </button>
          <button
            type="button"
            class="danger-action"
            disabled={saving}
            onclick={() => onArchivedChange(true)}
          >
            {saving ? "Archiving..." : "Archive channel"}
          </button>
        </div>
      {:else}
        <div class="profile-actions channel-settings-actions">
          <button type="button" class="ghost-action" disabled={saving} onclick={close}>
            Cancel
          </button>
          <button
            type="button"
            class="danger-action"
            disabled={saving}
            onclick={() => (confirmingArchive = true)}
          >
            Archive channel
          </button>
        </div>
      {/if}
    </div>
  </div>
</div>

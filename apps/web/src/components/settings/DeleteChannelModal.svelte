<script lang="ts">
  import { channelDeletionBlockerMessage, channelDeletionConfirmed } from "../../lib/channel-deletion";
  import { channelDisplayTitle } from "../../lib/chat/channels";
  import type { Channel, ChannelDeletionPreview } from "../../lib/types";
  import { formatBytes } from "../../lib/uploads";

  type Props = {
    channel: Channel;
    preview: ChannelDeletionPreview | null;
    loading?: boolean;
    deleting?: boolean;
    error?: string;
    onClose: () => void;
    onArchive: () => void;
    onConfirm: () => void;
  };

  let {
    channel,
    preview,
    loading = false,
    deleting = false,
    error = "",
    onClose,
    onArchive,
    onConfirm,
  }: Props = $props();

  let typed = $state("");
  const channelTitle = $derived(`#${channelDisplayTitle(channel)}`);
  const blocker = $derived(preview ? channelDeletionBlockerMessage(preview) : "");
  const canConfirm = $derived(
    Boolean(preview) && !blocker && !loading && !deleting && channelDeletionConfirmed(typed, channel),
  );
  const numbers = new Intl.NumberFormat();

  function close() {
    if (!deleting) onClose();
  }

  function submit(event: SubmitEvent) {
    event.preventDefault();
    if (canConfirm) onConfirm();
  }
</script>

<div class="modal-scrim delete-channel-scrim" role="presentation">
  <button
    class="modal-backdrop"
    type="button"
    aria-label="Cancel channel deletion"
    disabled={deleting}
    onclick={close}
  ></button>
  <div
    class="profile-modal delete-channel-modal"
    role="dialog"
    aria-modal="true"
    aria-labelledby="delete-channel-title"
    aria-describedby="delete-channel-description"
  >
    <header>
      <div>
        <p class="delete-channel-eyebrow">Delete channel</p>
        <h2 id="delete-channel-title">Delete {channelTitle}?</h2>
      </div>
      <button type="button" aria-label="Cancel channel deletion" disabled={deleting} onclick={close}>
        &times;
      </button>
    </header>

    <form class="delete-channel-content" onsubmit={submit}>
      <p id="delete-channel-description" class="delete-channel-warning">
        This permanently erases the channel and everything in it for everyone. It can't be undone.
      </p>

      {#if loading}
        <p class="profile-status" role="status">Checking what will be deleted...</p>
      {:else if preview}
        <dl class="delete-channel-counts" aria-label="What will be deleted">
          <div><dt>Messages</dt><dd>{numbers.format(preview.counts.messages)}</dd></div>
          <div><dt>Thread replies</dt><dd>{numbers.format(preview.counts.thread_replies)}</dd></div>
          <div><dt>Files</dt><dd>{numbers.format(preview.counts.files)}</dd></div>
          <div><dt>Storage</dt><dd>{formatBytes(preview.counts.file_bytes)}</dd></div>
        </dl>
        <ul class="delete-channel-consequences">
          <li>Pins, reactions, channel topics, and everyone's read state are removed too.</li>
          <li>People viewing the channel are moved out, and links to it stop working.</li>
          {#if channel.external_managed}
            <li class="is-warning">An external application manages this channel and may create it again.</li>
          {/if}
        </ul>
        {#if blocker}
          <p class="profile-status error" role="alert">{blocker}</p>
        {:else}
          <label class="delete-channel-confirm">
            <span>Type <code>{channel.name}</code> to confirm</span>
            <input
              type="text"
              autocomplete="off"
              autocapitalize="off"
              spellcheck="false"
              bind:value={typed}
              disabled={deleting}
            />
          </label>
        {/if}
      {/if}

      {#if error}
        <p class="profile-status error" role="alert">{error}</p>
      {/if}

      <div class="profile-actions delete-channel-actions">
        {#if !channel.archived_at}
          <button type="button" class="ghost-action" disabled={deleting} onclick={onArchive}>
            Archive instead
          </button>
        {/if}
        <button type="button" class="ghost-action" disabled={deleting} onclick={close}>
          Cancel
        </button>
        <button type="submit" class="danger-action" disabled={!canConfirm}>
          {deleting ? "Deleting..." : "Delete channel"}
        </button>
      </div>
    </form>
  </div>
</div>

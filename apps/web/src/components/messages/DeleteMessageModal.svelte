<script lang="ts">
  import { time } from "../../lib/format";
  import type { Message } from "../../lib/types";
  import Avatar from "../avatar/Avatar.svelte";

  type Props = {
    message: Message;
    deleting?: boolean;
    error?: string;
    onClose: () => void;
    onConfirm: () => void;
  };

  let {
    message,
    deleting = false,
    error = "",
    onClose,
    onConfirm,
  }: Props = $props();

  const authorName = $derived(message.author?.display_name || "Local User");

  function close() {
    if (!deleting) onClose();
  }
</script>

<div class="modal-scrim delete-message-scrim" role="presentation">
  <button
    class="modal-backdrop"
    type="button"
    aria-label="Cancel message deletion"
    disabled={deleting}
    onclick={close}
  ></button>
  <div
    class="profile-modal delete-message-modal"
    role="dialog"
    aria-modal="true"
    aria-labelledby="delete-message-title"
    aria-describedby="delete-message-description"
  >
    <header>
      <div>
        <h2 id="delete-message-title">Delete message</h2>
      </div>
      <button type="button" aria-label="Cancel message deletion" disabled={deleting} onclick={close}>
        &times;
      </button>
    </header>

    <div class="delete-message-content">
      <p id="delete-message-description">
        Are you sure you want to delete this message? This cannot be undone.
      </p>

      <div class="delete-message-preview">
        <Avatar
          class="avatar"
          id={message.author?.id || message.author_id}
          name={authorName}
          src={message.author?.avatar_url}
          size={36}
          loading="eager"
          fetchPriority="auto"
        />
        <div class="delete-message-preview__content">
          <div class="delete-message-preview__meta">
            <strong>{authorName}</strong>
            <time datetime={message.created_at}>{time(message.created_at)}</time>
          </div>
          <div class="delete-message-preview__body">{message.body}</div>
        </div>
      </div>

      {#if error}
        <p class="profile-status error" role="alert">{error}</p>
      {/if}

      <div class="profile-actions delete-message-actions">
        <button type="button" class="ghost-action" disabled={deleting} onclick={close}>
          Cancel
        </button>
        <button
          type="button"
          class="danger-action"
          disabled={deleting}
          onclick={onConfirm}
        >
          {deleting ? "Deleting..." : "Delete"}
        </button>
      </div>
    </div>
  </div>
</div>

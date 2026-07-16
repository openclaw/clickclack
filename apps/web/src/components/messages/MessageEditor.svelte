<script lang="ts">
  import { onMount, tick } from "svelte";
  import type { Message } from "../../lib/types";

  let {
    message,
    body,
    errorMessage,
    saving,
    onBody,
    onError,
    onCancel,
    onSave,
  }: {
    message: Message;
    body: string;
    errorMessage: string;
    saving: boolean;
    onBody: (body: string) => void;
    onError: (message: string) => void;
    onCancel: () => void;
    onSave: (message: Message, body: string) => Promise<void>;
  } = $props();

  let textarea = $state<HTMLTextAreaElement>();

  function normalizeForServer(value: string): string {
    return value
      .replace(/^\p{White_Space}+/u, "")
      .replace(/\p{White_Space}+$/u, "");
  }

  onMount(async () => {
    await tick();
    textarea?.focus();
    textarea?.setSelectionRange(textarea.value.length, textarea.value.length);
  });

  async function save() {
    const normalizedBody = normalizeForServer(body);
    if (!normalizedBody) {
      onError("Message body is required");
      return;
    }
    if (normalizedBody === message.body) {
      onCancel();
      return;
    }
    onError("");
    try {
      await onSave(message, body);
    } catch (error) {
      onError(error instanceof Error ? error.message : "Could not save edit");
    }
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      onCancel();
      return;
    }
    if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      void save();
    }
  }
</script>

<div class="message-edit">
  <textarea
    bind:this={textarea}
    class="message-edit__textarea"
    value={body}
    oninput={(event) => onBody(event.currentTarget.value)}
    onkeydown={handleKeydown}
    disabled={saving}
    aria-label="Edit message"
  ></textarea>
  <div class="message-edit__footer">
    <button
      type="button"
      class="message-edit__save"
      onclick={save}
      disabled={saving || !normalizeForServer(body)}
    >
      {#if saving}Saving…{:else}Save{/if}
    </button>
    <button type="button" class="message-edit__cancel" onclick={onCancel} disabled={saving}
      >Cancel</button
    >
    <kbd class="message-edit__shortcut">Ctrl/⌘+Enter to save</kbd>
  </div>
  {#if errorMessage}
    <p class="message-edit__error" role="alert">{errorMessage}</p>
  {/if}
</div>

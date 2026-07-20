<script lang="ts">
  import { onMount, tick } from "svelte";
  import { normalizeMessageBodyForServer } from "../../lib/messageEditing.svelte";

  let {
    body,
    errorMessage,
    saving,
    onBody,
    onCancel,
    onSave,
  }: {
    body: string;
    errorMessage: string;
    saving: boolean;
    onBody: (body: string) => void;
    onCancel: () => void;
    onSave: () => void | Promise<void>;
  } = $props();

  let textarea = $state<HTMLTextAreaElement>();

  onMount(async () => {
    await tick();
    textarea?.focus();
    textarea?.setSelectionRange(textarea.value.length, textarea.value.length);
  });

  async function save() {
    await onSave();
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

<div class="message-edit" aria-busy={saving}>
  <textarea
    bind:this={textarea}
    class="message-edit__textarea"
    value={body}
    rows="3"
    oninput={(event) => onBody(event.currentTarget.value)}
    onkeydown={handleKeydown}
    disabled={saving}
    aria-label="Edit message"
    aria-keyshortcuts="Control+Enter Meta+Enter Escape"
    aria-invalid={errorMessage ? "true" : undefined}
    aria-describedby={errorMessage ? "message-edit-error" : undefined}
  ></textarea>
  <div class="message-edit__footer">
    <button
      type="button"
      class="message-edit__save"
      onclick={save}
      disabled={saving || !normalizeMessageBodyForServer(body)}
    >
      {#if saving}Saving…{:else}Save{/if}
    </button>
    <button type="button" class="message-edit__cancel" onclick={onCancel} disabled={saving}
      >Cancel</button
    >
  </div>
  {#if errorMessage}
    <p id="message-edit-error" class="message-edit__error" role="alert">{errorMessage}</p>
  {/if}
</div>

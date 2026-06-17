<script lang="ts">
  import { goto } from "$app/navigation";

  let { data } = $props();

  const workspaceID = $derived(data.workspaceID);

  function backToChat() {
    void goto(`/app/${workspaceID}`);
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key !== "Escape") return;
    const target = event.target as HTMLElement | null;
    if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable)) {
      return;
    }
    event.preventDefault();
    backToChat();
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="workspace-settings-page">
  <header class="workspace-settings-page__header">
    <div>
      <p class="workspace-settings-page__eyebrow">Workspace</p>
      <h1 class="workspace-settings-page__h1">Workspace settings</h1>
      <p class="workspace-settings-page__lead">
        Manage this workspace: members, bots, integrations, and audit logs all live here.
      </p>
    </div>
    <button type="button" class="workspace-settings-page__close" onclick={backToChat} aria-label="Back to chat">
      <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M18 6L6 18M6 6l12 12" />
      </svg>
    </button>
  </header>

  <section class="workspace-settings-page__empty">
    <p>Workspace administration is being built. Bots, members, integrations, and audit logs will appear here.</p>
  </section>
</div>

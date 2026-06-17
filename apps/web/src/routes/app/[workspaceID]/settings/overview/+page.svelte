<script lang="ts">
  import { isWorkspaceManager } from "../../../../../lib/permissions";

  let { data } = $props();
  const workspace = $derived(data.workspace);
  const isOwner = $derived((workspace?.role ?? "") === "owner");
  const isManager = $derived(isWorkspaceManager(workspace?.role));
  const initial = $derived((workspace?.name ?? "?").slice(0, 1).toUpperCase());

  function capitalize(value: string | undefined): string {
    if (!value) return "—";
    return value.charAt(0).toUpperCase() + value.slice(1);
  }
</script>

<header class="ws-page__header">
  <h1 class="ws-page__h1">Overview</h1>
  <p class="ws-page__lead">
    How {workspace?.name ?? "this workspace"} appears to members and the bots that connect to it.
  </p>
</header>

<div class="ws-strip" aria-label="Current workspace">
  <span class="ws-strip__avatar">{initial}</span>
  <span class="ws-strip__name">{workspace?.name ?? "—"}</span>
  <span class="ws-strip__pill ws-strip__pill--{workspace?.role ?? 'member'}">{capitalize(workspace?.role)}</span>
</div>

<div class="ws-section">
  <div class="ws-row">
    <div class="ws-row__main">
      <div class="ws-row__label">Workspace name</div>
      <div class="ws-row__hint">The name members and bots see.</div>
    </div>
    <input
      class="ws-input"
      type="text"
      value={workspace?.name ?? ""}
      readonly
      aria-readonly="true"
    />
  </div>

  <div class="ws-row">
    <div class="ws-row__main">
      <div class="ws-row__label">URL slug</div>
      <div class="ws-row__hint">
        clickclack.app/<b>{workspace?.slug ?? "—"}</b>
      </div>
    </div>
    <input
      class="ws-input"
      type="text"
      value={workspace?.slug ?? ""}
      readonly
      aria-readonly="true"
    />
  </div>

  <div class="ws-row">
    <div class="ws-row__main">
      <div class="ws-row__label">Workspace icon</div>
      <div class="ws-row__hint">Shown in the workspace switcher and in mentions.</div>
    </div>
    <button class="ws-btn" type="button" disabled>Upload icon</button>
  </div>
</div>

{#if isManager}
  <div class="ws-section">
    <h3 class="ws-section__h">Danger zone</h3>

    {#if isOwner}
      <div class="ws-row">
        <div class="ws-row__main">
          <div class="ws-row__label">Transfer ownership</div>
          <div class="ws-row__hint">
            Transfer ownership of {workspace?.name ?? "this workspace"} to another member.
          </div>
        </div>
        <button class="ws-btn" type="button" disabled>Transfer</button>
      </div>

      <div class="ws-row">
        <div class="ws-row__main">
          <div class="ws-row__label">Delete workspace</div>
          <div class="ws-row__hint">
            Permanently delete {workspace?.name ?? "this workspace"}, all of its messages, channels,
            and bot tokens. This cannot be undone.
          </div>
        </div>
        <button class="ws-btn ws-btn--danger" type="button" disabled>Delete workspace</button>
      </div>
    {:else}
      <div class="ws-row">
        <div class="ws-row__main">
          <div class="ws-row__label">Owner-only actions</div>
          <div class="ws-row__hint">
            Only the workspace owner can transfer ownership or delete the workspace.
          </div>
        </div>
      </div>
    {/if}
  </div>
{/if}

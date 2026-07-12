<script lang="ts">
  import { goto, invalidateAll } from "$app/navigation";
  import { api, APIError } from "$lib/api";
  import { isWorkspaceManager } from "$lib/permissions";
  import type { Upload, Workspace } from "$lib/types";
  import {
    listWorkspaceMembersPage,
    type WorkspaceMember,
  } from "$lib/workspace-members";

  let { data } = $props();
  const workspace = $derived(data.workspace);
  const isOwner = $derived((workspace?.role ?? "") === "owner");
  const isManager = $derived(isWorkspaceManager(workspace?.role));
  const initial = $derived((workspace?.name ?? "?").slice(0, 1).toUpperCase());

  let name = $state("");
  let slug = $state("");
  let status = $state("");
  let error = $state("");
  let saving = $state(false);
  let iconInput: HTMLInputElement | undefined = $state();
  let transferMembers = $state<WorkspaceMember[]>([]);
  let transferLoadedFor = $state("");
  let transferUserID = $state("");
  let transferring = $state(false);
  let deleteConfirm = $state("");
  let deleting = $state(false);
  const hasProfileChanges = $derived(
    !!workspace && (name.trim() !== workspace.name || slug.trim() !== workspace.slug),
  );
  const canSaveProfile = $derived(isManager && hasProfileChanges && !saving);
  const canTransfer = $derived(isOwner && transferUserID !== "" && !transferring);
  const canDelete = $derived(isOwner && workspace && deleteConfirm === workspace.name && !deleting);

  $effect(() => {
    if (!workspace) return;
    name = workspace.name;
    slug = workspace.slug;
    deleteConfirm = "";
    status = "";
    error = "";
  });

  $effect(() => {
    if (!isOwner || !workspace || transferLoadedFor === workspace.id) return;
    void loadTransferMembers(workspace.id);
  });

  function capitalize(value: string | undefined): string {
    if (!value) return "-";
    return value.charAt(0).toUpperCase() + value.slice(1);
  }

  function errorMessage(err: unknown): string {
    if (err instanceof APIError) {
      try {
        const parsed = JSON.parse(err.message) as { error?: string };
        if (parsed.error) return parsed.error;
      } catch {
        return err.message;
      }
    }
    return err instanceof Error ? err.message : "Something went wrong";
  }

  async function updateWorkspace(body: Partial<Pick<Workspace, "name" | "slug" | "icon_url">>) {
    if (!workspace) return;
    const result = await api<{ workspace: Workspace }>(`/api/workspaces/${workspace.id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    });
    await invalidateAll();
    return result.workspace;
  }

  async function saveProfile() {
    if (!workspace || !canSaveProfile) return;
    saving = true;
    status = "";
    error = "";
    try {
      await updateWorkspace({ name: name.trim(), slug: slug.trim() });
      status = "Workspace updated.";
    } catch (err) {
      error = errorMessage(err);
    } finally {
      saving = false;
    }
  }

  function chooseIcon() {
    iconInput?.click();
  }

  async function handleIconChange(event: Event) {
    if (!workspace) return;
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    input.value = "";
    if (!file) return;
    if (!file.type.startsWith("image/")) {
      error = "Workspace icon must be an image.";
      return;
    }
    saving = true;
    status = "";
    error = "";
    try {
      const form = new FormData();
      form.set("workspace_id", workspace.id);
      form.set("file", file);
      const uploaded = await api<{ upload: Upload }>("/api/uploads", { method: "POST", body: form });
      const update: Partial<Pick<Workspace, "name" | "slug" | "icon_url">> = {
        icon_url: `/api/uploads/${uploaded.upload.id}`,
      };
      if (name.trim() !== workspace.name) update.name = name.trim();
      if (slug.trim() !== workspace.slug) update.slug = slug.trim();
      await updateWorkspace(update);
      status = "Workspace icon updated.";
    } catch (err) {
      error = errorMessage(err);
    } finally {
      saving = false;
    }
  }

  async function loadTransferMembers(workspaceID: string) {
    try {
      const members: WorkspaceMember[] = [];
      let cursor: string | undefined;
      const seenCursors = new Set<string>();
      do {
        const page = await listWorkspaceMembersPage({ workspaceID, limit: 200, cursor });
        members.push(...page.members);
        cursor = page.has_more ? page.next_cursor : undefined;
        if (page.has_more && !cursor) {
          throw new Error("Member directory returned an incomplete page");
        }
        if (cursor && seenCursors.has(cursor)) {
          throw new Error("Member directory repeated a pagination cursor");
        }
        if (cursor) seenCursors.add(cursor);
      } while (cursor);
      if (workspace?.id !== workspaceID) return;
      transferMembers = members.filter(
        (member) =>
          member.user.kind === "human" &&
          member.role !== "owner" &&
          member.role !== "guest" &&
          member.role !== "bot",
      );
      transferUserID = transferMembers[0]?.user.id ?? "";
      transferLoadedFor = workspaceID;
    } catch (err) {
      error = errorMessage(err);
    }
  }

  async function transferOwnership() {
    if (!workspace || !canTransfer) return;
    transferring = true;
    status = "";
    error = "";
    try {
      await api<{ workspace: Workspace }>(`/api/workspaces/${workspace.id}/transfer-ownership`, {
        method: "POST",
        body: JSON.stringify({ user_id: transferUserID }),
      });
      await invalidateAll();
      status = "Ownership transferred.";
    } catch (err) {
      error = errorMessage(err);
    } finally {
      transferring = false;
    }
  }

  async function deleteWorkspace() {
    if (!workspace || !canDelete) return;
    deleting = true;
    status = "";
    error = "";
    try {
      await api(`/api/workspaces/${workspace.id}`, { method: "DELETE" });
      await goto("/app", { invalidateAll: true, replaceState: true });
    } catch (err) {
      error = errorMessage(err);
      deleting = false;
    }
  }
</script>

<header class="ws-page__header">
  <h1 class="ws-page__h1">Overview</h1>
  <p class="ws-page__lead">
    How {workspace?.name ?? "this workspace"} appears to members and the bots that connect to it.
  </p>
</header>

<div class="ws-strip" aria-label="Current workspace">
  <span class="ws-strip__avatar">
    {#if workspace?.icon_url}
      <img src={workspace.icon_url} alt="" />
    {:else}
      {initial}
    {/if}
  </span>
  <span class="ws-strip__name">{workspace?.name ?? "-"}</span>
  <span class="ws-strip__pill ws-strip__pill--{workspace?.role ?? 'member'}">{capitalize(workspace?.role)}</span>
</div>

<div class="ws-section">
  <div class="ws-row">
    <div class="ws-row__main">
      <div class="ws-row__label">Workspace name</div>
      <div class="ws-row__hint">The name members and bots see.</div>
    </div>
    <input aria-label="Workspace name" class="ws-input" type="text" bind:value={name} disabled={!isManager || saving} />
  </div>

  <div class="ws-row">
    <div class="ws-row__main">
      <div class="ws-row__label">URL slug</div>
      <div class="ws-row__hint">
        clickclack.app/<b>{workspace?.slug ?? "-"}</b>
      </div>
    </div>
    <input aria-label="Workspace slug" class="ws-input" type="text" bind:value={slug} disabled={!isManager || saving} />
  </div>

  <div class="ws-row">
    <div class="ws-row__main">
      <div class="ws-row__label">Workspace icon</div>
      <div class="ws-row__hint">Shown in the workspace switcher and in mentions.</div>
    </div>
    <div class="ws-row__control">
      <input
        bind:this={iconInput}
        class="ws-file"
        type="file"
        aria-label="Workspace icon file"
        accept="image/png,image/jpeg,image/gif,image/webp"
        onchange={handleIconChange}
      />
      <button class="ws-btn" type="button" disabled={!isManager || saving} onclick={chooseIcon}>
        Upload icon
      </button>
    </div>
  </div>

  {#if isManager}
    <div class="ws-actions">
      <p class="ws-status" class:is-error={!!error}>{error || status}</p>
      <button class="ws-btn ws-btn--primary" type="button" disabled={!canSaveProfile} onclick={saveProfile}>
        {saving ? "Saving..." : "Save changes"}
      </button>
    </div>
  {/if}
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
        <div class="ws-row__control">
          <select
            class="ws-select"
            aria-label="New workspace owner"
            bind:value={transferUserID}
            disabled={transferring || transferMembers.length === 0}
          >
            {#each transferMembers as member (member.user.id)}
              <option value={member.user.id}>{member.user.display_name || member.user.handle || member.user.id}</option>
            {/each}
          </select>
          <button class="ws-btn" type="button" disabled={!canTransfer} onclick={transferOwnership}>
            {transferring ? "Transferring..." : "Transfer"}
          </button>
        </div>
      </div>

      <div class="ws-row">
        <div class="ws-row__main">
          <div class="ws-row__label">Delete workspace</div>
          <div class="ws-row__hint">
            Permanently delete {workspace?.name ?? "this workspace"}, all of its messages, channels,
            and bot tokens. This cannot be undone.
          </div>
        </div>
        <div class="ws-row__control">
          <input
            class="ws-input"
            type="text"
            placeholder={workspace?.name ?? "Workspace name"}
            bind:value={deleteConfirm}
            disabled={deleting}
          />
          <button class="ws-btn ws-btn--danger" type="button" disabled={!canDelete} onclick={deleteWorkspace}>
            {deleting ? "Deleting..." : "Delete workspace"}
          </button>
        </div>
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

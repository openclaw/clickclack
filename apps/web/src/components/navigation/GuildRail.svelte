<script lang="ts">
  import { workspaceInitial } from "../../lib/chat/people";
  import type { Workspace } from "../../lib/types";

  type Props = {
    workspaces: Workspace[];
    homeHref?: string;
    selectedWorkspaceID: string;
    workspaceName: string;
    showWorkspaceCreate: boolean;
    hrefForWorkspace: (workspaceID: string) => string;
    onSelectWorkspace: (workspaceID: string) => void;
    onToggleWorkspaceCreate: () => void;
    onWorkspaceName: (value: string) => void;
    onCreateWorkspace: () => void;
  };

  let {
    workspaces,
    homeHref = "/",
    selectedWorkspaceID,
    workspaceName,
    showWorkspaceCreate,
    hrefForWorkspace,
    onSelectWorkspace,
    onToggleWorkspaceCreate,
    onWorkspaceName,
    onCreateWorkspace,
  }: Props = $props();

  function shouldHandleClientNavigation(event: MouseEvent): boolean {
    return event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey;
  }
</script>

<nav id="workspace-navigation" class="guild-rail" aria-label="Workspaces">
  <a class="guild home" title="ClickClack home" href={homeHref}>
    <span>cc</span>
  </a>
  <div class="guild-divider" aria-hidden="true"></div>
  <div class="guild-list">
    {#each workspaces as workspace (workspace.id)}
      <div class="guild-wrap" class:active={workspace.id === selectedWorkspaceID}>
        <a
          class="guild"
          title={workspace.name}
          aria-label={workspace.name}
          href={hrefForWorkspace(workspace.id)}
          onclick={(event) => {
            if (!shouldHandleClientNavigation(event)) return;
            event.preventDefault();
            onSelectWorkspace(workspace.id);
          }}
        >
          {#if workspace.icon_url}
            <img class="guild__image" src={workspace.icon_url} alt="" />
          {:else}
            <span>{workspaceInitial(workspace.name)}</span>
          {/if}
        </a>
      </div>
    {/each}
    <button
      class="guild add"
      title="Create workspace"
      aria-label="Create workspace"
      onclick={onToggleWorkspaceCreate}
    >+</button>
  </div>
  {#if showWorkspaceCreate}
    <form
      class="guild-create"
      onsubmit={(event) => {
        event.preventDefault();
        onCreateWorkspace();
      }}
    >
      <input
        value={workspaceName}
        placeholder="Workspace name"
        aria-label="Workspace name"
        oninput={(event) => onWorkspaceName(event.currentTarget.value)}
      />
    </form>
  {/if}
</nav>

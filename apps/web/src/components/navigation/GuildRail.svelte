<script lang="ts">
  import { workspaceInitial } from "../../lib/chat/people";
  import type { Workspace } from "../../lib/types";

  type Props = {
    workspaces: Workspace[];
    selectedWorkspaceID: string;
    workspaceName: string;
    showWorkspaceCreate: boolean;
    onSelectWorkspace: (workspaceID: string) => void;
    onToggleWorkspaceCreate: () => void;
    onWorkspaceName: (value: string) => void;
    onCreateWorkspace: () => void;
  };

  let {
    workspaces,
    selectedWorkspaceID,
    workspaceName,
    showWorkspaceCreate,
    onSelectWorkspace,
    onToggleWorkspaceCreate,
    onWorkspaceName,
    onCreateWorkspace,
  }: Props = $props();
</script>

<nav id="workspace-navigation" class="guild-rail" aria-label="Workspaces">
  <a class="guild home" title="ClickClack home" href="/">
    <span>cc</span>
  </a>
  <div class="guild-divider" aria-hidden="true"></div>
  <div class="guild-list">
    {#each workspaces as workspace (workspace.id)}
      <div class="guild-wrap" class:active={workspace.id === selectedWorkspaceID}>
        <button class="guild" title={workspace.name} onclick={() => onSelectWorkspace(workspace.id)}>
          <span>{workspaceInitial(workspace.name)}</span>
        </button>
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

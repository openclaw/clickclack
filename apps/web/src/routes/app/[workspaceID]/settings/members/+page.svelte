<script lang="ts">
  let { data } = $props();
  const workspace = $derived(data.workspace);

  const ROLE_ORDER: Record<string, number> = {
    owner: 0,
    moderator: 1,
    member: 2,
    bot: 3,
    guest: 4,
  };

  const sortedMembers = $derived(
    [...data.members].sort((a, b) => {
      const ra = ROLE_ORDER[a.role] ?? 99;
      const rb = ROLE_ORDER[b.role] ?? 99;
      if (ra !== rb) return ra - rb;
      const na = (a.user.display_name || a.user.handle || "").toLocaleLowerCase();
      const nb = (b.user.display_name || b.user.handle || "").toLocaleLowerCase();
      return na.localeCompare(nb);
    }),
  );

  function formatHandle(handle: string): string {
    if (!handle) return "—";
    return handle.startsWith("@") ? handle : `@${handle}`;
  }

  function formatJoined(value: string): string {
    if (!value) return "—";
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return "—";
    return d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
  }
</script>

<header class="ws-page__header">
  <h1 class="ws-page__h1">Members</h1>
  <p class="ws-page__lead">
    {sortedMembers.length}
    {sortedMembers.length === 1 ? "member" : "members"} in {workspace?.name ?? "this workspace"}.
  </p>
</header>

{#if data.loadError}
  <p class="workspace-settings-frame__error">{data.loadError}</p>
{:else if sortedMembers.length === 0}
  <section class="workspace-settings-empty">
    <p>No members yet.</p>
  </section>
{:else}
  <div class="workspace-members">
    <table class="workspace-members__table">
      <thead>
        <tr>
          <th scope="col">Name</th>
          <th scope="col">Handle</th>
          <th scope="col">Role</th>
          <th scope="col">Joined</th>
        </tr>
      </thead>
      <tbody>
        {#each sortedMembers as member (member.user.id)}
          <tr>
            <td>{member.user.display_name || "—"}</td>
            <td class="workspace-members__handle">{formatHandle(member.user.handle)}</td>
            <td class="workspace-members__role">{member.role}</td>
            <td class="workspace-members__joined">{formatJoined(member.user.created_at)}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

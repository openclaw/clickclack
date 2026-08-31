<script lang="ts">
  import Avatar from "../avatar/Avatar.svelte";
  import { avatarHue, handleLabel } from "../../lib/chat/people";
  import type { MemberModeration, User, Workspace } from "../../lib/types";

  type Props = {
    profile: User;
    currentUser: User | null;
    workspaceName?: string;
    currentUserRole?: Workspace["role"] | "";
    moderation?: MemberModeration;
    onClose: () => void;
    onEdit?: () => void;
    onMessage?: (memberID: string) => void;
    messagePending?: boolean;
    messageError?: string;
    onApprove?: (memberID: string) => void;
    onTimeout?: (memberID: string) => void;
    onBlock?: (memberID: string) => void;
    onUnblock?: (memberID: string) => void;
    onSetStatus?: () => void;
  };

  let {
    profile,
    currentUser,
    workspaceName,
    currentUserRole,
    moderation,
    onClose,
    onEdit,
    onMessage,
    messagePending = false,
    messageError = "",
    onApprove,
    onTimeout,
    onBlock,
    onUnblock,
    onSetStatus,
  }: Props = $props();

  const botLabel = $derived(
    profile.kind === "bot" ? (profile.owner_user_id ? `Bot of ${profile.owner_user_id}` : "Service bot") : "",
  );
  const targetRole = $derived(moderation?.role || "member");
  const canModerateRole = $derived(
    Boolean(moderation) &&
      targetRole !== "owner" &&
      (currentUserRole === "owner" || (currentUserRole === "moderator" && (targetRole === "member" || targetRole === "guest"))),
  );
  const canModerate = $derived(
    currentUser?.id !== profile.id && canModerateRole,
  );
  const isBlocked = $derived(Boolean(moderation?.blocked_at));
  const roleLabel = $derived(targetRole);
</script>

<header>
  <div>
    <p>Profile</p>
    <strong>{profile.display_name}</strong>
  </div>
  <button class="close" aria-label="Close profile" onclick={onClose}>×</button>
</header>
<div class="profile-pane">
  <div class="profile-hero" style="--hue: {avatarHue(profile.id)}deg">
    <Avatar
      class="profile-avatar"
      id={profile.id}
      name={profile.display_name}
      src={profile.avatar_url}
      size={240}
      loading="eager"
      fetchPriority="auto"
    />
  </div>
  <section class="profile-pane-body">
    <div class="profile-pane-title">
      <div>
        <h2>{profile.display_name}</h2>
        {#if botLabel}<span class="bot-badge">{botLabel}</span>{/if}
        {#if profile.handle}<span>{handleLabel(profile.handle)}</span>{/if}
      </div>
      {#if currentUser?.id === profile.id && onEdit}
        <button type="button" class="text-action" onclick={onEdit}>Edit</button>
      {/if}
    </div>
    <div class="profile-presence">
      <span class="presence-dot active" aria-hidden="true"></span>
      <span>Active</span>
    </div>
    {#if (currentUser?.id !== profile.id && onMessage) || onSetStatus}
    <div class="profile-actions-row">
      {#if currentUser?.id !== profile.id && onMessage}
        <button type="button" class="primary-action" disabled={messagePending} onclick={() => onMessage?.(profile.id)}>
          {messagePending ? "Starting…" : "Message"}
        </button>
      {/if}
      {#if onSetStatus}<button type="button" class="ghost-action" onclick={onSetStatus}>
        Set a status
      </button>{/if}
    </div>
    {/if}
    {#if messageError}<p class="profile-status error" role="alert">{messageError}</p>{/if}
    <section class="profile-info">
      <header>
        <strong>Contact information</strong>
        {#if currentUser?.id === profile.id && onEdit}
          <button type="button" class="text-action" onclick={onEdit}>Edit</button>
        {/if}
      </header>
      <div class="profile-info-row">
        <span class="info-icon" aria-hidden="true">
          <svg viewBox="0 0 24 24" width="18" height="18">
            <path
              d="M16 8v5a3 3 0 0 0 6 0v-1a10 10 0 1 0-4.1 8.1"
              fill="none"
              stroke="currentColor"
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
            />
            <circle cx="12" cy="12" r="4" fill="none" stroke="currentColor" stroke-width="2" />
          </svg>
        </span>
        <div>
          <small>Handle</small>
          <span>{profile.handle ? handleLabel(profile.handle) : "No handle set"}</span>
        </div>
      </div>
      <div class="profile-info-row">
        <span class="info-icon" aria-hidden="true">
          <svg viewBox="0 0 24 24" width="18" height="18">
            <rect x="4" y="5" width="16" height="14" rx="3" fill="none" stroke="currentColor" stroke-width="2" />
            <path d="M8 10h8M8 14h5" fill="none" stroke="currentColor" stroke-linecap="round" stroke-width="2" />
          </svg>
        </span>
        <div>
          <small>User ID</small>
          <span>{profile.id}</span>
        </div>
      </div>
      {#if profile.kind === "bot"}
        <div class="profile-info-row">
          <span class="info-icon" aria-hidden="true">
            <svg viewBox="0 0 24 24" width="18" height="18">
              <rect x="5" y="7" width="14" height="11" rx="3" fill="none" stroke="currentColor" stroke-width="2" />
              <path d="M12 3v4M9 12h.01M15 12h.01" fill="none" stroke="currentColor" stroke-linecap="round" stroke-width="2" />
            </svg>
          </span>
          <div>
            <small>User kind</small>
            <span>{botLabel}</span>
          </div>
        </div>
      {/if}
    </section>
    <section class="profile-info">
      <header>
        <strong>About</strong>
      </header>
      <p class="profile-note">Member of {workspaceName || "this workspace"}.</p>
    </section>
    {#if canModerate && moderation}
      <section class="profile-info moderation-box">
        <header>
          <strong>Moderation</strong>
          <span class="role-pill">{roleLabel}</span>
        </header>
        {#if moderation.role === "guest" && moderation.post_limit > 0}
          <p class="profile-note">{moderation.posts_remaining} of {moderation.post_limit} waiting-room posts left today.</p>
        {/if}
        {#if moderation.timeout_until}
          <p class="profile-note">Timed out until {new Date(moderation.timeout_until).toLocaleString()}.</p>
        {/if}
        {#if moderation.blocked_at}
          <p class="profile-note">Blocked.</p>
        {/if}
        <div class="moderation-actions">
          {#if moderation.role === "guest" && onApprove}
            <button type="button" class="primary-action" onclick={() => onApprove?.(profile.id)}>Approve</button>
          {/if}
          {#if onTimeout}<button type="button" class="ghost-action" onclick={() => onTimeout?.(profile.id)}>Timeout 1h</button>{/if}
          {#if isBlocked && onUnblock}
            <button type="button" class="ghost-action" onclick={() => onUnblock?.(profile.id)}>Unblock</button>
          {:else if !isBlocked && onBlock}
            <button type="button" class="danger-action" onclick={() => onBlock?.(profile.id)}>Block</button>
          {/if}
        </div>
      </section>
    {/if}
  </section>
</div>

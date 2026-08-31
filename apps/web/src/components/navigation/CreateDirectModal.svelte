<script lang="ts">
  import Avatar from "../avatar/Avatar.svelte";
  import { handleLabel } from "../../lib/chat/people";
  import type { User } from "../../lib/types";

  type Props = {
    people: User[];
    currentUserID?: string;
    memberID: string;
    pending: boolean;
    error: string;
    onMemberID: (value: string) => void;
    onClose: () => void;
    onStart: (memberID: string) => void;
  };

  let { people, currentUserID, memberID, pending, error, onMemberID, onClose, onStart }: Props = $props();

  let query = $derived(memberID.trim().toLowerCase());
  let choices = $derived(people
    .filter((person) => person.id !== currentUserID)
    .filter((person) => {
      if (!query) return true;
      return (
        person.display_name.toLowerCase().includes(query) ||
        person.handle?.toLowerCase().includes(query) ||
        person.id.toLowerCase().includes(query)
      );
    }));
</script>

<div class="modal-scrim" role="presentation">
  <button class="modal-backdrop" type="button" aria-label="Close direct message dialog" onclick={onClose}></button>
  <section class="profile-modal create-modal" aria-label="Start direct message">
    <header>
      <div>
        <p>Direct messages</p>
        <h2>Start a DM</h2>
      </div>
      <button type="button" aria-label="Close direct message dialog" onclick={onClose}>×</button>
    </header>
    <form
      class="profile-form"
      onsubmit={(event) => {
        event.preventDefault();
        onStart(memberID);
      }}
    >
      <label class="field">
        <span>Find a person</span>
        <input
          value={memberID}
          disabled={pending}
          aria-label="Find a person"
          placeholder="Name, handle, or user id"
          autocomplete="off"
          oninput={(event) => onMemberID(event.currentTarget.value)}
        />
      </label>

      <div class="person-picker" aria-label="People">
        {#each choices as person (person.id)}
          <button type="button" class="person-choice" disabled={pending} onclick={() => onStart(person.id)}>
            <Avatar
              class="dm-avatar"
              id={person.id}
              name={person.display_name}
              src={person.avatar_url}
              size={32}
            />
            <span>
              <strong>{person.display_name}</strong>
              <small>{handleLabel(person.handle) || person.id}</small>
            </span>
          </button>
        {/each}
        {#if choices.length === 0}
          <div class="person-empty">No matching people yet</div>
        {/if}
      </div>

      {#if error}<p class="profile-status error" role="alert">{error}</p>{/if}
      <div class="profile-actions">
        <button type="button" class="ghost-action" onclick={onClose}>Cancel</button>
        <button type="submit" class="primary-action" disabled={pending || !memberID.trim()}>{pending ? "Starting…" : "Start DM"}</button>
      </div>
    </form>
  </section>
</div>

<script lang="ts">
  import { onDestroy, type Snippet } from "svelte";
  import { readableAPIError } from "../../lib/api";
  import { requestCurrentUser } from "../../lib/appearance";
  import type { User } from "../../lib/types";

  type Props = {
    section: "profile" | "notifications";
    payload: () => Partial<Pick<User, "display_name" | "handle" | "avatar_url" | "notification_settings">>;
    children: Snippet;
    onUserUpdated: (user: User) => void;
    onSaved?: () => void;
  };

  let { section, payload, children, onUserUpdated, onSaved }: Props = $props();
  let saving = $state(false);
  let status = $state("");
  let statusError = $state(false);
  const lifetime = new AbortController();

  // An accepted save may outlive the form, but its response must not change a newer section.
  onDestroy(() => lifetime.abort());

  async function save() {
    if (saving) return;
    saving = true;
    status = "";
    statusError = false;
    try {
      const data = await requestCurrentUser({
        method: "PATCH",
        body: JSON.stringify(payload()),
        signal: lifetime.signal,
      });
      lifetime.signal.throwIfAborted();
      onUserUpdated(data.user);
      status = "Saved";
      onSaved?.();
    } catch (error) {
      if (lifetime.signal.aborted) return;
      status = readableAPIError(error, `Could not save ${section}`);
      statusError = true;
    } finally {
      saving = false;
    }
  }
</script>

<form class="settings-form" onsubmit={(event) => { event.preventDefault(); void save(); }}>
  <fieldset disabled={saving}>
    {@render children()}
  </fieldset>
  <footer class="settings-footer">
    {#if status}
      <p class="settings-status" class:is-error={statusError} role="status">{status}</p>
    {:else}
      <span class="settings-footer__spacer" aria-hidden="true"></span>
    {/if}
    <button type="submit" class="settings-button settings-button--primary" disabled={saving}>
      {saving ? "Saving..." : `Save ${section}`}
    </button>
  </footer>
</form>

<style>
  fieldset {
    display: flex;
    flex-direction: column;
    gap: inherit;
    min-width: 0;
    margin: 0;
    padding: 0;
    border: 0;
  }
</style>

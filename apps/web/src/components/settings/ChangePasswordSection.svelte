<script lang="ts">
  import { api, readableAPIError } from "../../lib/api";
  import { validateNewPassword } from "../../lib/password";

  let currentPassword = $state("");
  let newPassword = $state("");
  let confirmPassword = $state("");
  let status = $state("");
  let statusError = $state(false);
  let saving = $state(false);

  async function save() {
    if (saving) return;
    const problem = validateNewPassword(currentPassword, newPassword, confirmPassword);
    if (problem) {
      status = problem;
      statusError = true;
      return;
    }
    saving = true;
    status = "";
    statusError = false;
    try {
      await api("/api/auth/password/change", {
        method: "POST",
        body: JSON.stringify({
          current_password: currentPassword,
          new_password: newPassword,
        }),
      });
      // The fields hold a live secret; clear them the moment they are spent.
      currentPassword = "";
      newPassword = "";
      confirmPassword = "";
      status = "Password updated. Your other devices were signed out.";
    } catch (error) {
      status = readableAPIError(error, "Could not change your password");
      statusError = true;
    } finally {
      saving = false;
    }
  }
</script>

<form
  class="settings-form settings-password"
  onsubmit={(event) => {
    event.preventDefault();
    void save();
  }}
>
  <div class="settings-password__intro">
    <strong>Change password</strong>
    <p>Pick a new password for signing in. Your other devices are signed out.</p>
  </div>

  <div class="settings-rows">
    <div class="settings-row2">
      <div class="settings-row2__desc">
        <label class="settings-row2__label" for="password-current">Current password</label>
        <p class="settings-row2__hint">The password you signed in with.</p>
      </div>
      <div class="settings-row2__control">
        <input
          id="password-current"
          class="settings-input"
          type="password"
          bind:value={currentPassword}
          aria-label="Current password"
          autocomplete="current-password"
        />
      </div>
    </div>

    <div class="settings-row2">
      <div class="settings-row2__desc">
        <label class="settings-row2__label" for="password-new">New password</label>
        <p class="settings-row2__hint">At least 8 characters. Longer is better than clever.</p>
      </div>
      <div class="settings-row2__control">
        <input
          id="password-new"
          class="settings-input"
          type="password"
          bind:value={newPassword}
          aria-label="New password"
          autocomplete="new-password"
        />
      </div>
    </div>

    <div class="settings-row2">
      <div class="settings-row2__desc">
        <label class="settings-row2__label" for="password-confirm">Confirm new password</label>
        <p class="settings-row2__hint">Type it once more so a typo cannot lock you out.</p>
      </div>
      <div class="settings-row2__control">
        <input
          id="password-confirm"
          class="settings-input"
          type="password"
          bind:value={confirmPassword}
          aria-label="Confirm new password"
          autocomplete="new-password"
        />
      </div>
    </div>
  </div>

  <footer class="settings-footer">
    {#if status}
      <p class="settings-status" class:is-error={statusError} role="status">{status}</p>
    {:else}
      <span class="settings-footer__spacer" aria-hidden="true"></span>
    {/if}
    <button type="submit" class="settings-button settings-button--primary" disabled={saving}>
      {saving ? "Updating..." : "Update password"}
    </button>
  </footer>
</form>

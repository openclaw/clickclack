<script lang="ts">
  import {
    createEventSubscription,
    integrationsLoadErrorMessage,
    revokeEventSubscription,
    rotateEventSubscriptionSecret,
    type EventSubscription,
  } from "../../../lib/integrations";
  import DeliveriesPanel from "./DeliveriesPanel.svelte";

  type Props = {
    workspaceID: string;
    subscriptions: EventSubscription[];
    eventTypes: string[];
    // When set, new subscriptions are attached to this installation. When
    // unset the panel is read-only over unattached subscriptions.
    installationID?: string;
    canManage: boolean;
    onChanged: (subscription: EventSubscription) => void;
  };

  let { workspaceID, subscriptions, eventTypes, installationID, canManage, onChanged }: Props =
    $props();

  let showCreate = $state(false);
  let callbackURL = $state("");
  let allEvents = $state(false);
  let selectedTypes = $state<string[]>([]);
  let submitting = $state(false);
  let error = $state("");
  let busyIDs = $state<Set<string>>(new Set());
  let revealedSecrets = $state<Record<string, string>>({});
  let copiedID = $state("");
  let openDeliveriesID = $state("");

  const canCreate = $derived(canManage && !!installationID);
  const createValid = $derived(
    !submitting &&
      (allEvents || selectedTypes.length > 0) &&
      callbackURL.trim().startsWith("https://"),
  );

  function toggleType(type: string) {
    selectedTypes = selectedTypes.includes(type)
      ? selectedTypes.filter((t) => t !== type)
      : [...selectedTypes, type];
  }

  function setBusy(id: string, busy: boolean) {
    const next = new Set(busyIDs);
    if (busy) {
      next.add(id);
    } else {
      next.delete(id);
    }
    busyIDs = next;
  }

  async function submitCreate(event: Event) {
    event.preventDefault();
    if (!createValid || !installationID) return;
    submitting = true;
    error = "";
    try {
      const created = await createEventSubscription(workspaceID, {
        app_installation_id: installationID,
        event_types: allEvents ? ["*"] : selectedTypes,
        callback_url: callbackURL.trim(),
      });
      if (created.signing_secret) {
        revealedSecrets = { ...revealedSecrets, [created.id]: created.signing_secret };
      }
      callbackURL = "";
      allEvents = false;
      selectedTypes = [];
      showCreate = false;
      onChanged(created);
    } catch (err) {
      error = integrationsLoadErrorMessage(err);
    } finally {
      submitting = false;
    }
  }

  async function revoke(subscription: EventSubscription) {
    if (busyIDs.has(subscription.id)) return;
    if (
      !confirm(
        "Revoke this event subscription? Deliveries to its callback stop immediately. The delivery history is kept.",
      )
    ) {
      return;
    }
    setBusy(subscription.id, true);
    error = "";
    try {
      const revoked = await revokeEventSubscription(subscription.id);
      onChanged(revoked);
    } catch (err) {
      error = integrationsLoadErrorMessage(err);
    } finally {
      setBusy(subscription.id, false);
    }
  }

  async function rotate(subscription: EventSubscription) {
    if (busyIDs.has(subscription.id)) return;
    if (
      !confirm(
        "Rotate this subscription's signing secret? The old secret stops verifying immediately — update the receiver right away.",
      )
    ) {
      return;
    }
    setBusy(subscription.id, true);
    error = "";
    try {
      const rotated = await rotateEventSubscriptionSecret(subscription.id);
      if (rotated.signing_secret) {
        revealedSecrets = { ...revealedSecrets, [subscription.id]: rotated.signing_secret };
      }
      onChanged(rotated);
    } catch (err) {
      error = integrationsLoadErrorMessage(err);
    } finally {
      setBusy(subscription.id, false);
    }
  }

  async function copySecret(id: string) {
    try {
      await navigator.clipboard.writeText(revealedSecrets[id] ?? "");
      copiedID = id;
      setTimeout(() => {
        if (copiedID === id) copiedID = "";
      }, 1800);
    } catch {
      // Clipboard may be blocked; the value is still visible.
    }
  }

  function dismissSecret(id: string) {
    const { [id]: _, ...rest } = revealedSecrets;
    revealedSecrets = rest;
  }

  function formatDate(value: string | undefined): string {
    if (!value) return "—";
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return "—";
    return d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
  }
</script>

<div class="ws-intg__panel">
  <div class="ws-intg__panel-header">
    <div>
      <h5 class="ws-intg__panel-title">Event subscriptions</h5>
      <p class="ws-intg__panel-hint">
        Workspace events pushed to an HTTPS endpoint, signed with a per-subscription secret. Every
        attempt is recorded in the delivery log.
      </p>
    </div>
    {#if canCreate}
      <button
        type="button"
        class="ws-btn"
        onclick={() => {
          showCreate = !showCreate;
          error = "";
        }}
      >
        {showCreate ? "Cancel" : "Add subscription"}
      </button>
    {/if}
  </div>

  {#if showCreate && canCreate}
    <form class="ws-intg__inline-form" onsubmit={submitCreate}>
      <label class="ws-bots__form-field">
        <span class="ws-bots__form-label">Callback URL</span>
        <input
          class="ws-bots__form-input"
          type="url"
          bind:value={callbackURL}
          placeholder="https://example.com/hooks/events"
          required
        />
      </label>
      <fieldset class="ws-bots__form-field">
        <legend class="ws-bots__form-label">Event types</legend>
        <label class="ws-intg__toggle">
          <input type="checkbox" bind:checked={allEvents} />
          <span>
            <span class="ws-bots__choice-title">All events (<code>*</code>)</span>
            <span class="ws-bots__choice-hint">
              Includes event types added in future releases.
            </span>
          </span>
        </label>
        {#if !allEvents}
          <div class="ws-intg__event-grid">
            {#each eventTypes as type (type)}
              <label class="ws-intg__event-option" class:is-active={selectedTypes.includes(type)}>
                <input
                  type="checkbox"
                  checked={selectedTypes.includes(type)}
                  onchange={() => toggleType(type)}
                />
                <code>{type}</code>
              </label>
            {/each}
          </div>
        {/if}
      </fieldset>
      <div class="ws-bots__form-actions">
        <button type="submit" class="ws-btn ws-btn--primary" disabled={!createValid}>
          {submitting ? "Creating…" : "Create subscription"}
        </button>
      </div>
    </form>
  {/if}

  {#if error}
    <p class="ws-bots__form-error" role="alert">{error}</p>
  {/if}

  {#if subscriptions.length === 0}
    <p class="ws-intg__panel-empty">No event subscriptions.</p>
  {:else}
    <ul class="ws-intg__item-list">
      {#each subscriptions as subscription (subscription.id)}
        <li class="ws-intg__item-row ws-intg__item-row--stacked">
          <div class="ws-intg__item-top">
            <div class="ws-intg__item-main">
              <div class="ws-intg__item-name">
                <span class="ws-intg__url" title={subscription.callback_url}>
                  {subscription.callback_url}
                </span>
              </div>
              <div class="ws-intg__item-meta">
                created {formatDate(subscription.created_at)}
              </div>
              <div class="ws-intg__type-chips">
                {#each subscription.event_types as type (type)}
                  <span class="ws-bots__scope-chip">{type}</span>
                {/each}
              </div>
              {#if revealedSecrets[subscription.id]}
                <div class="ws-intg__secret" aria-live="polite">
                  <span class="ws-intg__secret-label">Signing secret — visible once</span>
                  <div class="ws-intg__secret-row">
                    <input
                      class="ws-bots__reveal-input"
                      type="text"
                      readonly
                      value={revealedSecrets[subscription.id]}
                    />
                    <button
                      type="button"
                      class="ws-btn ws-btn--primary"
                      onclick={() => copySecret(subscription.id)}
                    >
                      {copiedID === subscription.id ? "Copied" : "Copy"}
                    </button>
                    <button
                      type="button"
                      class="ws-btn"
                      onclick={() => dismissSecret(subscription.id)}
                    >
                      Done
                    </button>
                  </div>
                </div>
              {/if}
            </div>
            <div class="ws-intg__item-actions">
              <button
                type="button"
                class="ws-btn"
                onclick={() =>
                  (openDeliveriesID = openDeliveriesID === subscription.id ? "" : subscription.id)}
              >
                {openDeliveriesID === subscription.id ? "Hide deliveries" : "Deliveries"}
              </button>
              {#if canManage}
                <button
                  type="button"
                  class="ws-btn"
                  onclick={() => rotate(subscription)}
                  disabled={busyIDs.has(subscription.id)}
                >
                  Rotate secret
                </button>
                <button
                  type="button"
                  class="ws-btn ws-btn--danger"
                  onclick={() => revoke(subscription)}
                  disabled={busyIDs.has(subscription.id)}
                >
                  Revoke
                </button>
              {/if}
            </div>
          </div>
          {#if openDeliveriesID === subscription.id}
            <DeliveriesPanel subscriptionID={subscription.id} />
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>

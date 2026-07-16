<script lang="ts">
  import { onMount } from "svelte";
  import type { BotSetupCode, BotToken } from "../../../lib/bots";
  import {
    buildOpenClawCodeSnippet,
    buildOpenClawConfigSnippet,
    buildOpenClawShellSnippet,
    botLoadErrorMessage,
    createWorkspaceBotSetupCode,
    type OpenClawAccountMode,
  } from "../../../lib/bots";
  import type { AppSnippetInput } from "../../../lib/app-catalog";

  type SnippetBuilder = (input: AppSnippetInput) => string;

  type Props = {
    // Which single credential this reveal presents. "code" shows only the
    // one-time setup code (no token exists yet); "token" shows only the
    // freshly minted raw token.
    connect?: "code" | "token";
    token?: BotToken | null;
    // Required in code mode, where no token carries these along.
    workspaceID?: string;
    tokenName?: string;
    scopes?: string[];
    botHandle: string;
    botUserID: string;
    workspace: string;
    defaultTo?: string;
    allowFrom?: string[];
    agentActivity?: boolean;
    configSnippetBuilder?: SnippetBuilder | null;
    shellSnippetBuilder?: SnippetBuilder | null;
    codeSnippetBuilder?: SnippetBuilder | null;
    onDismiss: () => void;
  };

  let {
    connect = "token",
    token = null,
    workspaceID,
    tokenName,
    scopes,
    botHandle,
    botUserID,
    workspace,
    defaultTo,
    allowFrom,
    agentActivity,
    configSnippetBuilder = (input) => buildOpenClawConfigSnippet(input),
    shellSnippetBuilder = (input) => buildOpenClawShellSnippet(input),
    codeSnippetBuilder = (input) =>
      buildOpenClawCodeSnippet({
        code: input.setupCode ?? "",
        botHandle: input.botHandle,
        mode: input.mode,
      }),
    onDismiss,
  }: Props = $props();

  const codeMode = $derived(connect === "code" && !!codeSnippetBuilder);
  const scopeChips = $derived(token?.scopes ?? scopes ?? []);

  let acknowledged = $state(false);
  let mode = $state<OpenClawAccountMode>("single");
  let copied = $state<"token" | "config" | "shell" | "code" | null>(null);

  // Setup-code connect path (claim-time mint): the code below mints its own
  // token when OpenClaw claims it, so the raw token never travels by hand.
  let setupCode = $state<BotSetupCode | null>(null);
  let mintingCode = $state(false);
  let codeError = $state("");
  let mintedAtMs = $state(0);
  let nowMs = $state(Date.now());

  async function mintSetupCode() {
    if (!codeMode || mintingCode) return;
    const mintWorkspaceID = workspaceID ?? token?.workspace_id;
    if (!mintWorkspaceID) {
      codeError = "Missing workspace for this setup code. Mint one from the bot's row instead.";
      return;
    }
    const requestedAtMs = Date.now();
    mintingCode = true;
    codeError = "";
    setupCode = null;
    copied = copied === "code" ? null : copied;
    try {
      const minted = await createWorkspaceBotSetupCode(mintWorkspaceID, botUserID, {
        name: tokenName ?? token?.name ?? "default",
        scopes: scopes ?? token?.scopes,
      });
      if (!minted.code) throw new Error("The server did not return a setup code. Try again.");
      setupCode = minted;
      // Starting at request dispatch is conservative: a slow response may make
      // the UI expire early, but can never leave an already-expired code visible.
      mintedAtMs = requestedAtMs;
      nowMs = Date.now();
    } catch (err) {
      codeError = botLoadErrorMessage(err);
    } finally {
      mintingCode = false;
    }
  }

  onMount(() => {
    if (codeMode) void mintSetupCode();
  });

  $effect(() => {
    const activeCode = setupCode;
    const startedAtMs = mintedAtMs;
    if (!activeCode) return;
    const ttl = Date.parse(activeCode.expires_at) - Date.parse(activeCode.created_at);
    if (!Number.isFinite(ttl) || ttl <= 0) return;
    const timer = setInterval(() => {
      const currentMs = Date.now();
      nowMs = currentMs;
      if (currentMs - startedAtMs >= ttl) clearInterval(timer);
    }, 1000);
    return () => clearInterval(timer);
  });

  // Remaining time anchored to the server-side TTL and the locally observed
  // mint moment, so a skewed client clock cannot fake early expiry.
  const codeRemainingMs = $derived.by(() => {
    if (!setupCode) return 0;
    const ttl = Date.parse(setupCode.expires_at) - Date.parse(setupCode.created_at);
    if (!Number.isFinite(ttl) || ttl <= 0) return 0;
    return Math.max(0, ttl - (nowMs - mintedAtMs));
  });
  const codeExpired = $derived(!!setupCode && codeRemainingMs <= 0);
  const codeCountdown = $derived.by(() => {
    const total = Math.ceil(codeRemainingMs / 1000);
    return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
  });

  const snippetInput = $derived<AppSnippetInput>({
    workspace,
    botHandle,
    botUserID,
    token: token?.token ?? "",
    mode,
    defaultTo,
    allowFrom,
    agentActivity,
  });
  const configSnippet = $derived(configSnippetBuilder?.(snippetInput) ?? "");
  const shellSnippet = $derived(shellSnippetBuilder?.(snippetInput) ?? "");
  const codeSnippet = $derived(
    setupCode?.code && codeSnippetBuilder
      ? codeSnippetBuilder({ ...snippetInput, setupCode: setupCode.code })
      : "",
  );

  async function copyTo(value: string, kind: "token" | "config" | "shell" | "code") {
    try {
      await navigator.clipboard.writeText(value);
      copied = kind;
      setTimeout(() => {
        if (copied === kind) copied = null;
      }, 1800);
    } catch {
      // Clipboard may be blocked; the value is still visible in the input.
    }
  }
</script>

<section class="ws-bots__reveal" aria-live="polite">
  <header class="ws-bots__reveal-header">
    <div>
      {#if codeMode}
        <h3 class="ws-bots__reveal-title">Your bot is ready to connect</h3>
        <p class="ws-bots__reveal-hint">
          Run the command below on the machine where OpenClaw lives. The one-time code mints the
          bot's token there — no credential to copy by hand.
        </p>
      {:else}
        <h3 class="ws-bots__reveal-title">Your new token is ready</h3>
        <p class="ws-bots__reveal-hint">
          Copy it now. ClickClack stores only a hash, so this is the last time the raw token is visible.
          If you lose it, mint a new one and revoke this one.
        </p>
      {/if}
    </div>
  </header>

  {#if codeMode}
    <div class="ws-bots__reveal-field">
      <div class="ws-bots__reveal-snippet-header">
        <span class="ws-bots__reveal-label">Add to OpenClaw</span>
        {#if setupCode && !codeExpired}
          <button type="button" class="ws-btn ws-btn--primary" onclick={() => copyTo(codeSnippet, "code")}>
            {copied === "code" ? "Copied" : "Copy command"}
          </button>
        {/if}
      </div>
      {#if setupCode && !codeExpired}
        <pre class="ws-bots__reveal-snippet"><code>{codeSnippet}</code></pre>
        <p class="ws-bots__reveal-hint">
          One-time code, single use.
          <span aria-hidden="true">Expires in <strong>{codeCountdown}</strong>.</span>
          <span class="sr-only">The code expires ten minutes after it was generated.</span>
          A running <code>openclaw gateway</code> picks the new channel up automatically.
        </p>
      {:else if codeExpired}
        <p class="ws-bots__reveal-hint" role="status">
          That setup code expired before it was used. Codes last 10 minutes — generate a fresh one
          when you're ready.
        </p>
        <div>
          <button type="button" class="ws-btn" onclick={mintSetupCode} disabled={mintingCode}>
            {mintingCode ? "Generating…" : "Generate new code"}
          </button>
        </div>
      {:else if mintingCode}
        <p class="ws-bots__reveal-hint">Generating a setup code…</p>
      {:else if codeError}
        <p class="ws-bots__form-error" role="alert">{codeError}</p>
        <div>
          <button type="button" class="ws-btn" onclick={mintSetupCode}>Try again</button>
        </div>
      {/if}
    </div>
  {/if}

  {#if codeMode || ((configSnippetBuilder || shellSnippetBuilder) && token)}
  <div class="ws-bots__reveal-field">
    <span class="ws-bots__reveal-label">OpenClaw account shape</span>
    <div class="ws-bots__setup-mode" role="group" aria-label="OpenClaw account shape">
      <button
        type="button"
        class:is-active={mode === "single"}
        onclick={() => (mode = "single")}
      >
        Single bot
      </button>
      <button
        type="button"
        class:is-active={mode === "named"}
        onclick={() => (mode = "named")}
      >
        Named account
      </button>
    </div>
  </div>
  {/if}

  {#if !codeMode && token}
    <div class="ws-bots__reveal-field">
      <label class="ws-bots__reveal-label" for="ws-bots-reveal-token">Token</label>
      <div class="ws-bots__reveal-row">
        <input
          id="ws-bots-reveal-token"
          class="ws-bots__reveal-input"
          type="text"
          readonly
          value={token.token ?? ""}
        />
        <button
          type="button"
          class="ws-btn ws-btn--primary"
          onclick={() => copyTo(token?.token ?? "", "token")}
        >
          {copied === "token" ? "Copied" : "Copy"}
        </button>
      </div>
    </div>

    {#if shellSnippetBuilder || configSnippetBuilder}
      <details class="ws-bots__reveal-details">
        <summary class="ws-bots__reveal-summary">OpenClaw setup</summary>
        <div class="ws-bots__reveal-details-body">
          {#if shellSnippetBuilder}
            <div class="ws-bots__reveal-snippet-header">
              <span class="ws-bots__reveal-label">Add to OpenClaw with the token</span>
              <button
                type="button"
                class="ws-btn"
                onclick={() => copyTo(shellSnippet, "shell")}
              >
                {copied === "shell" ? "Copied" : "Copy commands"}
              </button>
            </div>
            <pre class="ws-bots__reveal-snippet"><code>{shellSnippet}</code></pre>
            <p class="ws-bots__reveal-hint">
              A running OpenClaw gateway picks this up automatically. Not running yet? Start it with
              <code>openclaw gateway</code>.
            </p>
          {/if}

          {#if configSnippetBuilder}
            <div class="ws-bots__reveal-snippet-header">
              <span class="ws-bots__reveal-label">OpenClaw config</span>
              <button
                type="button"
                class="ws-btn"
                onclick={() => copyTo(configSnippet, "config")}
              >
                {copied === "config" ? "Copied" : "Copy config"}
              </button>
            </div>
            <pre class="ws-bots__reveal-snippet"><code>{configSnippet}</code></pre>
            <p class="ws-bots__reveal-hint">
              The commands above write this config for you. Use it for manual installs, or to add
              options the commands do not cover (agent activity, default channel, allowlist).
            </p>
          {/if}
        </div>
      </details>
    {/if}
  {/if}

  {#if scopeChips.length}
    <div class="ws-bots__reveal-field">
      <span class="ws-bots__reveal-label">Scopes</span>
      <div class="ws-bots__scope-row">
        {#each scopeChips as scope (scope)}
          <span class="ws-bots__scope-chip">{scope}</span>
        {/each}
      </div>
    </div>
  {/if}

  <label class="ws-bots__reveal-ack">
    <input type="checkbox" bind:checked={acknowledged} />
    {#if codeMode}
      <span>I've run the command, or I'll generate a new code from the bot's row later.</span>
    {:else}
      <span>I've copied this token somewhere safe.</span>
    {/if}
  </label>

  <div class="ws-bots__reveal-actions">
    <button type="button" class="ws-btn ws-btn--primary" disabled={!acknowledged} onclick={onDismiss}>
      Done
    </button>
  </div>
</section>

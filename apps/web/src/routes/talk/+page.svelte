<script lang="ts">
  import { onMount } from "svelte";
  import { productAppURLForHost } from "../../productLinks";

  const docsURL = "https://docs.clickclack.chat";
  const transcriptDocsURL = `${docsURL}/features/local-transcript.html`;
  const repoURL = "https://github.com/openclaw/clickclack";
  const appURL = typeof window !== "undefined" ? productAppURLForHost(window.location.hostname) : "/app";

  type AnyRecord = Record<string, any>;

  type CCSession = {
    lane: string;
    truth_status: string;
    api_status?: string | null;
    pid?: number | null;
    process_state?: string | null;
    cpu_ticks_delta?: number | null;
    duplicates?: number | null;
    session_id: string;
    cwd: string;
    last_output_age_seconds?: number | null;
    last_output_role?: string | null;
    last_output_snippet?: string | null;
    why?: string;
  };

  type TranscriptItem = {
    id: string;
    author: string;
    role: string;
    body: string;
    createdAt: string;
    mine: boolean;
  };

  const contractFields = [
    ["project_key", "Keeps one conversation on one lane."],
    ["lane", "Names the active session or thread."],
    ["source", "Shows which local session was selected."],
    ["intent", "Explains why the page selected this session."],
    ["receipts", "Preserves status proof without exposing local transcript paths."],
    ["needs_attention", "Marks when human attention or routing is required."],
  ];

  const truthCards = [
    ["Opt-in source", "Reads local transcript files only when the bridge status script is configured."],
    ["Real selection", "Chooses a live session first, then falls back to the freshest usable transcript."],
    ["Real restraint", "If there is no session, the page says so instead of staging dialogue."],
  ];

  const proofSteps = [
    ["Probe", "Read the bridge status output from the local CC monitor."],
    ["Select", "Pick the current live session or the best available fallback."],
    ["Parse", "Extract only human/assistant transcript lines from the .jsonl file."],
    ["Render", "Show the exact transcript bodies with a clear speaker split."],
    ["Receipt", "Keep status and last-output proof visible without leaking local paths."],
    ["Stop", "No invented dialogue and no remote exposure."],
  ];

  let loading = true;
  let status = "Sign in to expose the live transcript.";
  let session: CCSession | null = null;
  let transcript: TranscriptItem[] = [];
  let liveSessions: CCSession[] = [];

  async function fetchJson(path: string) {
    const response = await fetch(path, { credentials: "include" });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      const error = new Error(payload?.error ?? `${path} failed (${response.status})`);
      (error as Error & { status?: number }).status = response.status;
      throw error;
    }
    return payload;
  }

  function formatClock(value?: string) {
    if (!value) return "";
    const date = new Date(value);
    if (Number.isNaN(date.valueOf())) return "";
    return new Intl.DateTimeFormat("en-US", { hour: "numeric", minute: "2-digit" }).format(date);
  }

  function pickAuthor(role: string) {
    return role === "assistant" ? "Assistant" : role === "user" ? "User" : role || "Unknown";
  }

  function asText(value: any) {
    if (typeof value === "string") return value;
    if (value == null) return "";
    try {
      return JSON.stringify(value);
    } catch {
      return String(value);
    }
  }

  onMount(() => {
    void (async () => {
      loading = true;
      try {
        const payload = await fetchJson("/api/cc/transcript?limit=12");
        liveSessions = Array.isArray(payload.sessions) ? payload.sessions : [];
        session = payload.session ?? null;
        transcript = Array.isArray(payload.messages)
          ? payload.messages.map((message: AnyRecord) => ({
              id: message.id ?? crypto.randomUUID(),
              author: pickAuthor(message.role ?? message.author ?? ""),
              role: String(message.role ?? message.author ?? ""),
              body: asText(message.content ?? message.body ?? message.text ?? "").trim(),
              createdAt: formatClock(message.timestamp ?? message.created_at),
              mine: String(message.role ?? message.author ?? "") === "assistant",
            }))
          : [];
        status = payload.status ?? (session ? `${session.lane} · ${session.truth_status}` : "No local transcript session is attached yet.");
      } catch (error) {
        const err = error as Error & { status?: number };
        if (err.status === 401) {
          status = "Sign in to expose the live transcript.";
        } else {
          status = err.message || "Live transcript unavailable.";
        }
      } finally {
        loading = false;
      }
    })();
  });
</script>

<svelte:head>
  <title>ClickClack Talk — local transcript</title>
  <meta
    name="description"
    content="ClickClack Talk shows an opt-in local transcript, selected session status, and bridge proof for local developer workflows."
  />
  <meta name="color-scheme" content="light dark" />
</svelte:head>

<main class="talk-page">
  <header class="top-shell">
    <a class="brand" href="/" aria-label="ClickClack home">
      <span class="brand-mark">cc</span>
      <span class="brand-copy">
        <strong>ClickClack</strong>
        <small>Local desk</small>
      </span>
    </a>

    <nav class="nav-links" aria-label="Primary">
      <a href="/">Product</a>
      <a href={appURL}>App</a>
      <a href={docsURL}>Docs</a>
      <a href={repoURL}>GitHub</a>
    </nav>
  </header>

  <section class="hero" aria-label="Bridge overview">
    <div class="hero-copy">
      <p class="eyebrow">Local transcript bridge</p>
      <h1>The real conversation, or nothing.</h1>
      <p class="lede">
        This page reads an explicitly configured local transcript bridge, selects the active session,
        and renders the actual message bodies. If the bridge is disabled or idle, it says that plainly.
      </p>
      <div class="hero-actions">
        <a class="primary-action" href={appURL}>Open app</a>
        <a class="secondary-action" href={transcriptDocsURL}>Setup guide</a>
        <a class="ghost-action" href="#transcript">See transcript</a>
      </div>
    </div>

    <aside class="proof-card" aria-label="Bridge proof">
      <p class="card-kicker">What is true</p>
      <h2>One local source. One selection rule. No remote exposure.</h2>
      <ul>
        {#each truthCards as [title, body]}
          <li>
            <strong>{title}</strong>
            <span>{body}</span>
          </li>
        {/each}
      </ul>
      <p class="status-line">{status}</p>
      {#if session}
        <div class="session-stack">
          <div class="session-row">
            <span>Session</span>
            <strong>{session.session_id}</strong>
          </div>
          <div class="session-row">
            <span>Lane</span>
            <strong>{session.lane}</strong>
          </div>
          <div class="session-row">
            <span>CWD</span>
            <strong>{session.cwd}</strong>
          </div>
          <div class="session-row">
            <span>Visible sessions</span>
            <strong>{liveSessions.length}</strong>
          </div>
          {#if session.last_output_snippet}
            <div class="session-snippet">
              <span>Last output</span>
              <p>{session.last_output_snippet}</p>
            </div>
          {/if}
        </div>
      {/if}
    </aside>
  </section>

  <section class="proof-slab" id="transcript" aria-label="Live transcript">
    <div class="slab-head">
      <div>
        <p class="eyebrow">Live transcript</p>
        <h2>{session ? session.cwd : "No live session selected"}</h2>
      </div>
      <p class="slab-note">
        The page never fabricates dialogue. It either renders the configured local transcript or it
        shows the disabled or empty state.
      </p>
    </div>

    {#if transcript.length}
      <ul class="message-list">
        {#each transcript as message}
          <li class:mine={message.mine}>
            <div class="message-meta">
              <strong>{message.author}</strong>
              <span>{message.createdAt}</span>
            </div>
            <p>{message.body}</p>
          </li>
        {/each}
      </ul>
    {:else}
      <div class="empty-transcript">
        <p>{loading ? "Loading live data…" : status}</p>
        <p>
          Configure the bridge on localhost and this card will show text from the selected session.
          Tool calls, metadata rows, and local transcript paths stay out of the feed.
        </p>
      </div>
    {/if}
  </section>

  <section class="contract-grid" aria-label="Bridge contract">
    <article>
      <p class="eyebrow">Contract</p>
      <h2>Six fields. One lane. No shrug.</h2>
      <div class="field-grid">
        {#each contractFields as [field, meaning]}
          <div class="field-chip">
            <strong>{field}</strong>
            <span>{meaning}</span>
          </div>
        {/each}
      </div>
    </article>

    <article>
      <p class="eyebrow">How it stays honest</p>
      <h2>Probe, select, parse, render, receipt, stop.</h2>
      <ol class="step-list">
        {#each proofSteps as [title, body], index}
          <li>
            <span>{index + 1}</span>
            <div>
              <strong>{title}</strong>
              <p>{body}</p>
            </div>
          </li>
        {/each}
      </ol>
    </article>
  </section>
</main>

<style>
  :global(body) {
    margin: 0;
    background: #f4efe7;
    color: #111111;
  }

  :global(*) {
    box-sizing: border-box;
  }

  .talk-page {
    --paper: #f4efe7;
    --canvas: #fffaf2;
    --ink: #111111;
    --ink-soft: rgba(17, 17, 17, 0.72);
    --line: rgba(17, 17, 17, 0.16);
    --violet: #5d4bff;
    --violet-soft: rgba(93, 75, 255, 0.12);
    --moss: #6b7f63;
    --moss-soft: rgba(107, 127, 99, 0.12);
    --shadow: 0 22px 0 rgba(17, 17, 17, 0.08);
    --radius: 22px;

    min-height: 100vh;
    padding: 18px clamp(16px, 3.8vw, 56px) 64px;
    background:
      linear-gradient(180deg, rgba(255, 255, 255, 0.28), rgba(255, 255, 255, 0)),
      var(--paper);
    font-family: "Geist Variable", "Avenir Next", "Segoe UI", ui-sans-serif, system-ui, sans-serif;
  }

  a {
    color: inherit;
    text-decoration: none;
  }

  a:focus-visible {
    outline: 3px solid var(--violet);
    outline-offset: 3px;
  }

  .top-shell {
    position: sticky;
    top: 14px;
    z-index: 6;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 20px;
    max-width: 1280px;
    margin: 0 auto 30px;
    padding: 14px 16px;
    border: 1px solid var(--ink);
    border-radius: 999px;
    background: rgba(255, 250, 242, 0.86);
    backdrop-filter: blur(12px);
    box-shadow: 0 10px 30px rgba(17, 17, 17, 0.06);
  }

  .brand,
  .brand-copy,
  .nav-links,
  .hero-actions,
  .message-meta,
  .step-list li,
  .session-row {
    display: flex;
  }

  .brand {
    align-items: center;
    gap: 12px;
    min-width: 0;
  }

  .brand-mark {
    display: grid;
    place-items: center;
    width: 42px;
    height: 42px;
    border: 1px solid var(--ink);
    border-radius: 12px;
    background: #ffffff;
    color: var(--ink);
    font-weight: 950;
    letter-spacing: -0.04em;
    text-transform: lowercase;
  }

  .brand-copy {
    flex-direction: column;
    gap: 2px;
  }

  .brand-copy strong {
    font-weight: 900;
  }

  .brand-copy small,
  .eyebrow,
  .slab-note,
  .card-kicker,
  .field-chip span,
  .message-meta span,
  .step-list p,
  .status-line,
  .session-row span,
  .session-snippet span {
    color: var(--ink-soft);
    font-size: 13px;
    line-height: 1.45;
  }

  .nav-links {
    align-items: center;
    gap: clamp(14px, 3vw, 28px);
    font-weight: 750;
  }

  .nav-links a {
    position: relative;
  }

  .nav-links a::after {
    position: absolute;
    inset: auto 0 -6px;
    height: 2px;
    background: currentColor;
    content: "";
    opacity: 0;
    transform: scaleX(0.2);
    transform-origin: center;
    transition: opacity 160ms ease, transform 160ms ease;
  }

  .nav-links a:hover::after {
    opacity: 1;
    transform: scaleX(1);
  }

  .hero {
    display: grid;
    grid-template-columns: minmax(0, 1.02fr) minmax(320px, 0.86fr);
    gap: clamp(24px, 4vw, 40px);
    max-width: 1280px;
    margin: 0 auto;
    padding: clamp(20px, 4vw, 36px);
    border: 1px solid var(--ink);
    border-radius: 34px;
    background: var(--canvas);
    box-shadow: var(--shadow);
  }

  .hero-copy {
    display: grid;
    min-width: 0;
    gap: 22px;
    align-content: start;
    padding: clamp(8px, 1vw, 18px);
  }

  .eyebrow,
  .card-kicker {
    margin: 0;
    color: var(--violet);
    font-weight: 850;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  h1,
  h2,
  p,
  ul,
  ol {
    margin: 0;
  }

  h1 {
    max-width: 12ch;
    font-family: Georgia, "Times New Roman", serif;
    font-size: clamp(58px, 7vw, 108px);
    font-weight: 900;
    letter-spacing: -0.075em;
    line-height: 0.88;
  }

  .lede {
    max-width: 58ch;
    font-size: clamp(19px, 2vw, 27px);
    line-height: 1.28;
  }

  .hero-actions {
    flex-wrap: wrap;
    gap: 12px;
  }

  .primary-action,
  .secondary-action,
  .ghost-action {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-height: 48px;
    padding: 0 18px;
    border-radius: 999px;
    font-weight: 850;
  }

  .primary-action {
    background: var(--ink);
    color: var(--paper);
  }

  .secondary-action {
    border: 1px solid var(--ink);
    background: transparent;
    color: var(--ink);
  }

  .ghost-action {
    border: 1px solid transparent;
    background: var(--violet-soft);
    color: var(--violet);
  }

  .proof-card,
  .proof-slab,
  .contract-grid article {
    min-width: 0;
    border: 1px solid var(--ink);
    border-radius: var(--radius);
    background: #ffffff;
    box-shadow: 0 14px 0 rgba(17, 17, 17, 0.06);
  }

  .proof-card {
    display: grid;
    gap: 18px;
    align-content: start;
    padding: 24px;
  }

  .proof-card h2,
  .proof-slab h2,
  .contract-grid h2 {
    font-size: clamp(28px, 3.8vw, 52px);
    line-height: 0.98;
    letter-spacing: -0.05em;
  }

  .proof-card ul {
    display: grid;
    gap: 12px;
    list-style: none;
    padding: 0;
  }

  .proof-card li {
    display: grid;
    gap: 4px;
    padding: 14px 0 0;
    border-top: 1px solid var(--line);
  }

  .proof-card strong,
  .field-chip strong,
  .message-meta strong,
  .step-list strong,
  .session-row strong {
    font-weight: 850;
  }

  .proof-card span,
  .field-chip span,
  .step-list p,
  .empty-transcript p,
  .session-snippet p {
    color: var(--ink-soft);
    line-height: 1.5;
  }

  .status-line {
    padding-top: 12px;
    border-top: 1px solid var(--line);
    color: var(--moss);
  }

  .session-stack {
    display: grid;
    gap: 12px;
    padding-top: 4px;
    border-top: 1px solid var(--line);
  }

  .session-row {
    justify-content: space-between;
    gap: 14px;
    align-items: start;
  }

  .session-row strong {
    max-width: 30ch;
    text-align: right;
    word-break: break-word;
  }

  .session-snippet {
    display: grid;
    gap: 4px;
    padding-top: 6px;
  }

  .proof-slab {
    display: grid;
    gap: 22px;
    max-width: 1280px;
    margin: 26px auto 0;
    padding: 26px;
  }

  .slab-head {
    display: flex;
    justify-content: space-between;
    gap: 24px;
    align-items: end;
  }

  .slab-head h2 {
    margin-top: 8px;
    font-size: clamp(28px, 3.4vw, 44px);
    line-height: 0.96;
  }

  .slab-note {
    max-width: 48ch;
    padding-top: 2px;
  }

  .message-list {
    display: grid;
    gap: 12px;
    list-style: none;
    padding: 0;
  }

  .message-list li {
    display: grid;
    gap: 8px;
    padding: 16px 16px 18px;
    border: 1px solid var(--line);
    border-radius: 18px;
    background: linear-gradient(180deg, rgba(93, 75, 255, 0.03), rgba(107, 127, 99, 0.02));
  }

  .message-list li.mine {
    border-color: rgba(93, 75, 255, 0.26);
    background: linear-gradient(180deg, rgba(93, 75, 255, 0.07), rgba(255, 255, 255, 0));
  }

  .message-meta {
    align-items: center;
    justify-content: space-between;
    gap: 14px;
  }

  .message-list p {
    max-width: 82ch;
    line-height: 1.55;
    white-space: pre-wrap;
    word-break: break-word;
  }

  .empty-transcript {
    display: grid;
    gap: 8px;
    padding: 22px;
    border: 1px dashed var(--line);
    border-radius: 18px;
    background: linear-gradient(180deg, rgba(93, 75, 255, 0.05), rgba(255, 255, 255, 0));
  }

  .contract-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 18px;
    max-width: 1280px;
    margin: 18px auto 0;
  }

  .contract-grid article {
    padding: 24px;
  }

  .contract-grid article:nth-child(2) {
    background: linear-gradient(180deg, rgba(93, 75, 255, 0.04), rgba(255, 255, 255, 1));
  }

  .field-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 12px;
    margin-top: 20px;
  }

  .field-chip {
    display: grid;
    gap: 4px;
    min-height: 106px;
    padding: 16px;
    border: 1px solid var(--line);
    border-radius: 18px;
    background: linear-gradient(180deg, rgba(107, 127, 99, 0.06), rgba(255, 255, 255, 0));
  }

  .field-chip strong {
    font-size: 14px;
    letter-spacing: 0.01em;
  }

  .step-list {
    display: grid;
    gap: 14px;
    list-style: none;
    padding: 20px 0 0;
  }

  .step-list li {
    gap: 14px;
    align-items: start;
    padding: 14px 0 0;
    border-top: 1px solid var(--line);
  }

  .step-list span {
    display: grid;
    place-items: center;
    flex: 0 0 auto;
    width: 34px;
    height: 34px;
    border-radius: 999px;
    background: var(--violet-soft);
    color: var(--violet);
    font-weight: 900;
  }

  .step-list p {
    padding-top: 4px;
  }

  @media (max-width: 980px) {
    .hero,
    .contract-grid {
      grid-template-columns: 1fr;
    }

    .slab-head {
      flex-direction: column;
      align-items: start;
    }

    .field-grid {
      grid-template-columns: 1fr;
    }
  }

  @media (max-width: 700px) {
    .top-shell {
      position: static;
      flex-direction: column;
      border-radius: 28px;
      align-items: start;
    }

    .nav-links {
      flex-wrap: wrap;
    }

    .hero,
    .proof-slab,
    .contract-grid article {
      border-radius: 24px;
    }

    h1 {
      max-width: 100%;
      font-size: clamp(46px, 13vw, 62px);
      letter-spacing: -0.06em;
    }

    .message-meta,
    .session-row {
      flex-direction: column;
      align-items: start;
    }

    .session-row strong {
      text-align: left;
    }
  }
</style>

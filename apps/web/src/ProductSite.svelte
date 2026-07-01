<script lang="ts">
  import "./product.css";
  import { productAppURLForHost } from "./productLinks";

  const docsURL = "https://docs.clickclack.chat";
  const appURL = productAppURLForHost(window.location.hostname);
  const repoURL = "https://github.com/openclaw/clickclack";

  const features = [
    ["Single binary", "Go server, embedded Svelte app, embedded migrations, local SQLite and uploads."],
    ["Threads that recover", "Slack-style one-level threads with durable event replay after reconnects."],
    ["Agent-friendly", "A CLI, OpenAPI contract, TypeScript SDK, webhooks, and slash-command shapes."],
    ["Self-host first", "SQLite is the default, not the demo. Postgres can arrive behind the store layer."],
  ];

  const commands = [
    "clickclack serve --data ./data",
    "clickclack login --magic-token mgt_...",
    "clickclack send --channel general \"deploy started\"",
    "clickclack threads reply msg_... --stdin <summary.md",
  ];
</script>

<svelte:head>
  <title>ClickClack - Self-hostable chat with claws</title>
  <meta
    name="description"
    content="ClickClack is a self-hostable chat app with Slack-style threads, durable realtime, an agent-friendly CLI, OpenAPI, and a TypeScript SDK."
  />
  <meta name="color-scheme" content="light dark" />
</svelte:head>

<main class="product-site">
  <section class="hero">
    <div class="hero-bg" aria-hidden="true">
      <div class="workspace-rail">
        <span>cc</span>
        <span>ops</span>
        <span>bots</span>
      </div>
      <div class="timeline-preview">
        <div class="preview-top">
          <span># release-room</span>
          <strong>live</strong>
        </div>
        <article>
          <b>Mira</b>
          <p>Cut v0.1 once e2e and docs are green.</p>
        </article>
        <article>
          <b>build-bot</b>
          <p><code>pnpm test:e2e</code> passed. No skipped checks.</p>
        </article>
        <article class="thread-line">
          <b>Peter</b>
          <p>Thread this with deploy notes and the Hetzner target.</p>
        </article>
      </div>
      <div class="thread-preview">
        <span>Thread</span>
        <p>One level. No reply-tree archaeology.</p>
      </div>
    </div>

    <nav class="product-nav" aria-label="Product navigation">
      <a class="brand-lockup" href="/">
        <span class="brand-mark">cc</span>
        <span>ClickClack</span>
      </a>
      <div>
        <a href={docsURL}>Docs</a>
        <a href={appURL}>App</a>
        <a href={repoURL}>GitHub</a>
      </div>
    </nav>

    <div class="hero-copy">
      <p class="eyebrow">Self-hostable chat. Serious tool. Mild brine.</p>
      <h1>ClickClack</h1>
      <p class="lede">
        A single-binary chat app for teams, communities, bots, and agents:
        Slack-style threads, durable realtime, OpenAPI, SQLite, and a CLI that
        can drive the whole thing from a shell.
      </p>
      <div class="hero-actions">
        <a class="primary-action" href={appURL}>Open app</a>
        <a class="secondary-action" href={docsURL}>Read docs</a>
        <a class="secondary-action" href={repoURL}>GitHub</a>
      </div>
    </div>
  </section>

  <section class="product-band intro-band" aria-label="Product summary">
    <div>
      <p class="section-kicker">What it is</p>
      <h2>Chat infrastructure that stays boring when the socket drops.</h2>
    </div>
    <p>
      WebSocket is the pipe. The database is the truth. Every durable message,
      thread reply, reaction, and channel update can be recovered over HTTP with
      a cursor, so clients and agents can reconnect without drama.
    </p>
  </section>

  <section class="feature-grid" aria-label="Feature highlights">
    {#each features as feature}
      <article>
        <h3>{feature[0]}</h3>
        <p>{feature[1]}</p>
      </article>
    {/each}
  </section>

  <section class="product-band cli-band" aria-label="CLI examples">
    <div>
      <p class="section-kicker">Agent path</p>
      <h2>A friendly CLI, no LLM baked in.</h2>
      <p>
        External agents, CI jobs, and humans use the same public API as the web
        app. Tokens and workspace defaults are scoped per server, so switching
        hosts does not leak credentials or stale IDs.
      </p>
    </div>
    <pre aria-label="CLI command examples">{commands.join("\n")}</pre>
  </section>

  <section class="product-band docs-band" aria-label="Docs and app destinations">
    <div>
      <p class="section-kicker">Destinations</p>
      <h2>Product at the root. Docs and app where people expect them.</h2>
    </div>
    <div class="destination-list">
      <a href="/">clickclack.chat <span>Product website</span></a>
      <a href={docsURL}>docs.clickclack.chat <span>Architecture, API, deploy</span></a>
      <a href={appURL}>app.clickclack.chat <span>Hosted app surface</span></a>
      <a href={repoURL}>github.com/openclaw/clickclack <span>Source and issues</span></a>
    </div>
  </section>
</main>

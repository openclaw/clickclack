<script lang="ts">
  import "./product.css";
  import { productAppURLForHost } from "./productLinks";

  const docsURL = "https://docs.clickclack.chat";
  const appURL = productAppURLForHost(window.location.hostname);
  const repoURL = "https://github.com/openclaw/clickclack";

  const features = [
    {
      icon: "box",
      title: "One binary, zero services",
      body: "A single Go binary with the Svelte app, migrations, and static assets embedded. SQLite and local uploads by default — nothing else to run.",
    },
    {
      icon: "bolt",
      title: "Durable realtime",
      body: "WebSocket is the pipe, the database is the truth. Every event lands in a durable log, so clients reconnect with a cursor and miss nothing.",
    },
    {
      icon: "thread",
      title: "Threads that stay flat",
      body: "Slack-style threads, one level deep. Conversations branch without turning into reply-tree archaeology.",
    },
    {
      icon: "robot",
      title: "Agents are first-class",
      body: "Bots and agents use the same public API as humans: CLI, OpenAPI contract, TypeScript SDK, webhooks, and slash commands.",
    },
    {
      icon: "server",
      title: "Self-host first",
      body: "SQLite is the default, not the demo — WAL, FTS5 search, online backups. Postgres slots in behind the same store interface.",
    },
    {
      icon: "shield",
      title: "Calm moderation",
      body: "Magic-link auth, optional GitHub OAuth, and a guest waiting room with approvals, timeouts, and blocks built in.",
    },
  ];

  const terminalLines = [
    { prompt: true, text: "clickclack serve --data ./data" },
    { prompt: false, text: "listening on :8080 · sqlite ./data/clickclack.db" },
    { prompt: true, text: 'clickclack send --channel general "deploy started"' },
    { prompt: false, text: "msg_01j9x sent to #general" },
    { prompt: true, text: "clickclack threads reply msg_01j9x --stdin <notes.md" },
    { prompt: false, text: "reply posted · thread depth 1" },
  ];

  const quickstart = [
    { step: "Install", code: "pnpm install && pnpm build" },
    { step: "Serve", code: "go run ./apps/api/cmd/clickclack serve" },
    { step: "Chat", code: "open http://localhost:8080" },
  ];

  // Self-hosted instances keep App links on the local /app route, so the
  // card label has to follow the real target instead of the hosted domain.
  const appDestination = appURL.startsWith("http")
    ? { href: appURL, host: "app.clickclack.chat", label: "Hosted app surface" }
    : { href: appURL, host: "/app", label: "The app on this instance" };

  const destinations = [
    { href: docsURL, host: "docs.clickclack.chat", label: "Architecture, API, deploy guides" },
    appDestination,
    { href: repoURL, host: "github.com/openclaw/clickclack", label: "Source, issues, releases" },
  ];
</script>

<svelte:head>
  <title>ClickClack — Team chat for humans and their agents</title>
  <meta
    name="description"
    content="ClickClack is a self-hostable, single-binary chat app: Slack-style threads, durable realtime, SQLite, an agent-friendly CLI, OpenAPI, and a TypeScript SDK."
  />
  <meta name="color-scheme" content="light dark" />
</svelte:head>

<div class="product-site">
  <header class="site-nav-wrap">
    <nav class="site-nav" aria-label="Product navigation">
      <a class="brand" href="/">
        <img class="brand-mark" src="/favicon.svg" alt="" width="34" height="34" />
        <span class="brand-name">ClickClack</span>
      </a>
      <div class="nav-links">
        <a href="#features">Features</a>
        <a href="#agents">Agents</a>
        <a href={docsURL}>Docs</a>
        <a href={repoURL}>GitHub</a>
      </div>
      <a class="btn btn-primary nav-cta" href={appURL}>Open app</a>
    </nav>
  </header>

  <main>
    <section class="hero">
      <p class="hero-badge">
        <span class="badge-dot" aria-hidden="true"></span>
        Open source · MIT · Single Go binary
      </p>
      <h1>Team chat for humans<br /> and their agents.</h1>
      <p class="lede">
        ClickClack is a self-hostable chat app with Slack-style threads, durable
        realtime, and an API surface that bots, CI jobs, and agents can drive
        from a shell.
      </p>
      <div class="hero-actions">
        <a class="btn btn-primary" href={appURL}>Open app</a>
        <a class="btn btn-ghost" href={docsURL}>Read the docs</a>
      </div>
      <code class="hero-install" aria-label="Quick start command">
        <span aria-hidden="true">$</span> go run ./apps/api/cmd/clickclack serve
      </code>

      <div class="app-frame" role="img" aria-label="Preview of the ClickClack app showing a channel timeline and an open thread">
        <div class="app-titlebar" aria-hidden="true">
          <span class="tl-dot"></span>
          <span class="tl-dot"></span>
          <span class="tl-dot"></span>
          <span class="tl-title">clickclack — #release-room</span>
        </div>
        <div class="app-body" aria-hidden="true">
          <aside class="app-sidebar">
            <div class="ws-name">openclaw<span>workspace</span></div>
            <p class="side-label">Channels</p>
            <ul>
              <li># general</li>
              <li class="active"># release-room</li>
              <li># ops</li>
              <li># support</li>
            </ul>
            <p class="side-label">Direct messages</p>
            <ul>
              <li><span class="ps-presence"></span>Mira</li>
              <li><span class="ps-presence bot"></span>build-bot</li>
              <li><span class="ps-presence"></span>Peter</li>
            </ul>
          </aside>
          <div class="app-timeline">
            <div class="ps-timeline-head">
              <strong># release-room</strong>
              <span>Cutting v0.1 — checks, docs, deploy</span>
            </div>
            <div class="ps-msg">
              <span class="ps-avatar a-mira">M</span>
              <div>
                <p class="ps-msg-meta"><b>Mira</b><time>09:41</time></p>
                <p>Cut v0.1 once e2e and docs are green.</p>
              </div>
            </div>
            <div class="ps-msg">
              <span class="ps-avatar a-bot">⌘</span>
              <div>
                <p class="ps-msg-meta"><b>build-bot</b><span class="ps-bot-tag">BOT</span><time>09:42</time></p>
                <p><code>pnpm test:e2e</code> passed — 84 tests, no skips.</p>
                <p class="ps-thread-hint">↳ 2 replies</p>
              </div>
            </div>
            <div class="ps-msg">
              <span class="ps-avatar a-peter">P</span>
              <div>
                <p class="ps-msg-meta"><b>Peter</b><time>09:44</time></p>
                <p>Threading with deploy notes and the Hetzner target.</p>
              </div>
            </div>
            <div class="ps-composer">Message #release-room</div>
          </div>
          <aside class="app-thread">
            <div class="ps-thread-head">Thread</div>
            <div class="ps-msg">
              <span class="ps-avatar a-bot">⌘</span>
              <div>
                <p class="ps-msg-meta"><b>build-bot</b><time>09:42</time></p>
                <p>Artifacts uploaded. Binary is 24&nbsp;MB, static assets embedded.</p>
              </div>
            </div>
            <div class="ps-msg">
              <span class="ps-avatar a-peter">P</span>
              <div>
                <p class="ps-msg-meta"><b>Peter</b><time>09:45</time></p>
                <p>One level, no nesting. Ship it. 🦀</p>
              </div>
            </div>
            <div class="ps-composer">Reply in thread</div>
          </aside>
        </div>
      </div>
    </section>

    <section class="pillars" aria-label="Product summary">
      <p class="section-kicker">Why ClickClack</p>
      <h2>Chat infrastructure that stays boring when the socket drops.</h2>
      <p class="section-lede">
        Every durable message, thread reply, reaction, and channel update can be
        recovered over HTTP with a cursor — clients and agents reconnect without
        drama.
      </p>
    </section>

    <section class="feature-grid" id="features" aria-label="Feature highlights">
      {#each features as feature}
        <article>
          <span class="feature-icon" aria-hidden="true">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
              {#if feature.icon === "box"}
                <path d="M21 8l-9-5-9 5v8l9 5 9-5V8z" /><path d="M3 8l9 5 9-5" /><path d="M12 13v8" />
              {:else if feature.icon === "bolt"}
                <path d="M13 2L4 14h6l-1 8 9-12h-6l1-8z" />
              {:else if feature.icon === "thread"}
                <path d="M21 11a8 8 0 0 1-8 8H4l3-3" /><path d="M3 11a8 8 0 0 1 8-8h1" /><circle cx="17" cy="5" r="1.6" /><circle cx="7" cy="19" r="1.6" />
              {:else if feature.icon === "robot"}
                <rect x="4" y="8" width="16" height="11" rx="2.5" /><path d="M12 4v4" /><circle cx="12" cy="3" r="1" /><path d="M9 13h.01M15 13h.01" /><path d="M9.5 16.5h5" />
              {:else if feature.icon === "server"}
                <rect x="3" y="4" width="18" height="7" rx="2" /><rect x="3" y="13" width="18" height="7" rx="2" /><path d="M7 7.5h.01M7 16.5h.01" />
              {:else}
                <path d="M12 3l8 3v6c0 4.5-3.2 7.8-8 9-4.8-1.2-8-4.5-8-9V6l8-3z" /><path d="M9 12l2 2 4-4" />
              {/if}
            </svg>
          </span>
          <h3>{feature.title}</h3>
          <p>{feature.body}</p>
        </article>
      {/each}
    </section>

    <section class="agent-band" id="agents" aria-label="Agent and CLI workflow">
      <div class="agent-copy">
        <p class="section-kicker">Agent path</p>
        <h2>A friendly CLI. No LLM baked in.</h2>
        <p>
          External agents, CI jobs, and humans hit the same public API as the
          web app. Tokens and workspace defaults are scoped per server, so
          switching hosts never leaks credentials or stale IDs.
        </p>
        <ul class="agent-list">
          <li>OpenAPI contract for every endpoint</li>
          <li>Framework-neutral TypeScript SDK</li>
          <li>Mattermost-shaped webhooks and slash commands</li>
          <li>Durable event cursor for replay after downtime</li>
        </ul>
      </div>
      <div class="terminal" aria-label="CLI session example">
        <div class="terminal-bar" aria-hidden="true">
          <span class="tl-dot"></span>
          <span class="tl-dot"></span>
          <span class="tl-dot"></span>
          <span class="tl-title">agent@ci — zsh</span>
        </div>
        <pre>{#each terminalLines as line}<span class={line.prompt ? "t-prompt" : "t-out"}>{line.prompt ? "$ " : "  "}{line.text}
</span>{/each}</pre>
      </div>
    </section>

    <section class="quickstart" aria-label="Quickstart">
      <p class="section-kicker">Quickstart</p>
      <h2>Running in three commands.</h2>
      <ol class="quickstart-steps">
        {#each quickstart as item, i}
          <li>
            <span class="step-no">{i + 1}</span>
            <div>
              <h3>{item.step}</h3>
              <code>{item.code}</code>
            </div>
          </li>
        {/each}
      </ol>
    </section>

    <section class="destinations" aria-label="Docs and app destinations">
      <div class="dest-intro">
        <p class="section-kicker">Destinations</p>
        <h2>Everything where you expect it.</h2>
      </div>
      <div class="dest-list">
        {#each destinations as dest}
          <a href={dest.href}>
            <strong>{dest.host}</strong>
            <span>{dest.label}</span>
            <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M7 17L17 7" /><path d="M9 7h8v8" /></svg>
          </a>
        {/each}
      </div>
    </section>
  </main>

  <footer class="site-footer">
    <p>ClickClack — self-hostable chat with claws. MIT licensed.</p>
    <div>
      <a href={docsURL}>Docs</a>
      <a href={appURL}>App</a>
      <a href={repoURL}>GitHub</a>
    </div>
  </footer>
</div>

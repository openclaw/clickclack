import { a as attr_class, s as store_get, f as attr, g as attr_style, h as stringify, b as escape_html, u as unsubscribe_stores, d as derived, e as ensure_array_like } from "../../chunks/index.js";
import { i as inspectMode, a as activeMessageId, s as semanticPaneOpen, t as telemetryOpen } from "../../chunks/ui.js";
import { w as writable, g as get } from "../../chunks/index2.js";
import { marked } from "marked";
import DOMPurify from "dompurify";
function html(value) {
  var html2 = String(value ?? "");
  var open = "<!---->";
  return open + html2 + "<!---->";
}
const LS_TOKEN_KEY = "logos_api_token";
function apiBaseURL() {
  if (typeof window === "undefined") return "";
  return (window.__CLICKCLACK_CONFIG__?.apiBaseUrl || "").trim().replace(/\/$/, "");
}
function apiURL(path) {
  const base = apiBaseURL();
  return base ? `${base}${path.startsWith("/") ? path : `/${path}`}` : path;
}
class APIError extends Error {
  constructor(status, body) {
    super(body);
    this.status = status;
  }
}
async function api(path, init = {}) {
  const headers = new Headers(init.headers);
  const method = (init.method ?? "GET").toUpperCase();
  headers.set("Accept", "application/json");
  if (init.body && !(init.body instanceof FormData)) {
    headers.set("Content-Type", "application/json");
  }
  if (!["GET", "HEAD", "OPTIONS", "TRACE"].includes(method)) {
    headers.set("X-ClickClack-CSRF", "1");
  }
  try {
    const stored = localStorage.getItem(LS_TOKEN_KEY);
    if (stored) headers.set("Authorization", `Bearer ${stored}`);
  } catch {
  }
  const response = await fetch(apiURL(path), { ...init, credentials: "include", headers });
  if (!response.ok) {
    throw new APIError(response.status, await response.text());
  }
  if (response.status === 204 || response.status === 205) {
    return void 0;
  }
  const text = await response.text();
  return text ? JSON.parse(text) : void 0;
}
const MESSAGE_PAGE_LIMIT = 50;
const POLL_INTERVAL_MS = 1e4;
function emptySnapshot() {
  return {
    status: "booting",
    workspaces: [],
    channels: [],
    activeWorkspaceId: null,
    activeChannelId: null,
    messages: []
  };
}
const chatState = writable(emptySnapshot());
function update(partial) {
  chatState.update((s) => ({ ...s, ...partial }));
}
let pollTimer = null;
async function loadChannels(workspaceId) {
  try {
    const data = await api(
      `/api/workspaces/${workspaceId}/channels`
    );
    update({ channels: data.channels });
    return data.channels;
  } catch (err) {
    setError("loadChannels", err);
    return [];
  }
}
async function loadMessages(channelId, opts) {
  update({ activeChannelId: channelId, error: void 0 });
  const params = new URLSearchParams();
  params.set("mode", "latest");
  params.set("limit", String(MESSAGE_PAGE_LIMIT));
  try {
    const page = await api(
      `/api/channels/${channelId}/messages?${params.toString()}`
    );
    update({ messages: page.messages });
    return page.messages;
  } catch (err) {
    setError("loadMessages", err);
    return [];
  }
}
async function selectChannel(channelId) {
  stopPolling();
  update({ activeChannelId: channelId, messages: [], error: void 0 });
  const s = get(chatState);
  await loadMessages(channelId);
  if (s.activeWorkspaceId) {
    startPolling(s.activeWorkspaceId, channelId);
  }
}
async function selectWorkspace(workspaceId) {
  stopPolling();
  update({ activeWorkspaceId: workspaceId, activeChannelId: null, channels: [], messages: [] });
  const channels = await loadChannels(workspaceId);
  if (channels.length > 0) {
    await selectChannel(channels[0].id);
  }
}
function startPolling(workspaceId, channelId) {
  stopPolling();
  pollTimer = setInterval(async () => {
    try {
      const s = get(chatState);
      if (s.activeChannelId !== channelId || s.status !== "ready") return;
      const newestSeq = s.messages.length > 0 ? s.messages[s.messages.length - 1].channel_seq : void 0;
      const params = new URLSearchParams();
      params.set("mode", "latest");
      params.set("limit", String(MESSAGE_PAGE_LIMIT));
      if (newestSeq !== void 0) params.set("after_seq", String(newestSeq));
      const page = await api(
        `/api/channels/${channelId}/messages?${params.toString()}`
      );
      if (page.messages.length > 0) {
        chatState.update((prev) => {
          const existing = new Set(prev.messages.map((m) => m.id));
          const fresh = page.messages.filter((m) => !existing.has(m.id));
          if (fresh.length === 0) return prev;
          return {
            ...prev,
            messages: [...prev.messages, ...fresh].slice(-200)
          };
        });
      }
    } catch {
    }
  }, POLL_INTERVAL_MS);
}
function stopPolling() {
  if (pollTimer !== null) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}
function setError(context, err) {
  const message = err instanceof APIError ? `${context}: ${err.status} ${err.message}` : err instanceof Error ? `${context}: ${err.message}` : `${context}: unknown error`;
  update({ error: message });
  console.warn(`[logos/chat] ${message}`);
}
function MessageFrame($$renderer, $$props) {
  $$renderer.component(($$renderer2) => {
    var $$store_subs;
    let { message, onInspect, active = false } = $$props;
    const intentColorVar = derived(() => {
      const map = {
        ask: "var(--intent-ask)",
        command: "var(--intent-command)",
        reflect: "var(--intent-reflect)",
        draft: "var(--intent-draft)",
        clarify: "var(--intent-clarify)",
        explore: "var(--intent-explore)"
      };
      return message.intent ? map[message.intent] ?? "var(--intent-default)" : "var(--intent-default)";
    });
    const intentLabel = derived(() => message.intent ? message.intent.toUpperCase() : "--");
    const personaLabel = derived(() => message.persona ? message.persona.toUpperCase() : "--");
    const confLabel = derived(() => message.confidence != null ? message.confidence.toFixed(2) : "--");
    const threadLabel = derived(() => message.thread_id ? `#${message.thread_id.slice(0, 12)}` : "--");
    const renderedHtml = derived(() => {
      try {
        const raw = marked.parse(message.body, { async: false });
        return DOMPurify.sanitize(raw);
      } catch {
        return DOMPurify.sanitize(message.body);
      }
    });
    $$renderer2.push(`<div${attr_class("msg-frame svelte-dvjmcw", void 0, {
      "msg-active": active,
      "msg-inspect": store_get($$store_subs ??= {}, "$inspectMode", inspectMode)
    })}${attr("data-msg-id", message.id)}><div class="msg-intent-band svelte-dvjmcw"${attr_style(`--intent-color: ${stringify(intentColorVar())}`)}${attr("title", `Intent: ${stringify(intentLabel())}`)}></div> <div class="msg-content svelte-dvjmcw"><div class="msg-meta svelte-dvjmcw"><span class="meta-tag svelte-dvjmcw">INTENT: ${escape_html(intentLabel())}</span> <span class="meta-tag svelte-dvjmcw">PERSONA: ${escape_html(personaLabel())}</span>  <span${attr_class("meta-tag meta-conf svelte-dvjmcw", void 0, { "meta-clickable": onInspect != null })}${attr("role", onInspect ? "button" : void 0)}${attr("tabindex", onInspect ? 0 : void 0)}${attr("title", onInspect ? "Click to inspect" : void 0)}>CONF: ${escape_html(confLabel())}</span> <span class="meta-tag svelte-dvjmcw">THREAD: ${escape_html(threadLabel())}</span> <span class="meta-tag meta-latency svelte-dvjmcw">LATENCY: n/a</span></div> <div class="msg-body svelte-dvjmcw">${html(renderedHtml())}</div> <div class="msg-actions svelte-dvjmcw"><span class="msg-action-prompt svelte-dvjmcw">></span> <button type="button" class="msg-action-btn svelte-dvjmcw">XFORM</button> <button type="button" class="msg-action-btn svelte-dvjmcw">CONDENSE</button> <button type="button" class="msg-action-btn svelte-dvjmcw">EXPAND</button> <button type="button" class="msg-action-btn svelte-dvjmcw">MEM-NODE</button> <button type="button" class="msg-action-btn svelte-dvjmcw">REWRITE</button></div></div></div>`);
    if ($$store_subs) unsubscribe_stores($$store_subs);
  });
}
function InspectorBlade($$renderer, $$props) {
  $$renderer.component(($$renderer2) => {
    let { message, open } = $$props;
    let activeTab = "telemetry";
    const meta = derived(() => message.metadata_json ?? {});
    const telemetry = derived(() => meta().telemetry ?? {});
    const latencyMs = derived(() => telemetry().latency_ms);
    const totalTokens = derived(() => telemetry().total_tokens);
    const modelName = derived(() => telemetry().model);
    const intentVectorScore = derived(() => message.confidence != null ? message.confidence : void 0);
    const tabs = [
      { id: "telemetry", label: "TELEMETRY" },
      { id: "memory", label: "MEMORY" },
      { id: "logprobs", label: "LOGPROBS" },
      { id: "payload", label: "PAYLOAD" },
      { id: "stack", label: "STACK" }
    ];
    $$renderer2.push(`<div${attr_class("inspector-blade svelte-1y3653r", void 0, { "open": open })}><div class="inspector-tabs svelte-1y3653r"><!--[-->`);
    const each_array = ensure_array_like(tabs);
    for (let $$index = 0, $$length = each_array.length; $$index < $$length; $$index++) {
      let tab = each_array[$$index];
      $$renderer2.push(`<button type="button"${attr_class("inspector-tab svelte-1y3653r", void 0, { "active": activeTab === tab.id })}>${escape_html(tab.label)}</button>`);
    }
    $$renderer2.push(`<!--]--> <button type="button" class="inspector-close svelte-1y3653r" aria-label="Close inspector" title="Close inspector (Esc)"><svg viewBox="0 0 24 24" width="11" height="11" aria-hidden="true"><path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" d="M6 6l12 12M18 6L6 18"></path></svg></button></div> <div class="inspector-body svelte-1y3653r">`);
    {
      $$renderer2.push("<!--[0-->");
      $$renderer2.push(`<div class="inspector-section svelte-1y3653r"><div class="inspect-grid svelte-1y3653r"><div class="inspect-row svelte-1y3653r"><span class="inspect-label svelte-1y3653r">LATENCY (MS)</span> <span class="inspect-value svelte-1y3653r">${escape_html(latencyMs() !== void 0 ? `${Math.round(latencyMs())} ms` : "n/a")}</span></div> <div class="inspect-row svelte-1y3653r"><span class="inspect-label svelte-1y3653r">TOTAL TOKENS</span> <span class="inspect-value svelte-1y3653r">${escape_html(totalTokens() !== void 0 ? String(totalTokens()) : "n/a")}</span></div> <div class="inspect-row svelte-1y3653r"><span class="inspect-label svelte-1y3653r">MODEL</span> <span class="inspect-value mono svelte-1y3653r">${escape_html(modelName() ?? "n/a")}</span></div> <div class="inspect-row svelte-1y3653r"><span class="inspect-label svelte-1y3653r">INTENT VECTOR</span> <span class="inspect-value svelte-1y3653r">${escape_html(intentVectorScore() !== void 0 ? (intentVectorScore() * 100).toFixed(1) + "%" : "n/a")}</span></div></div></div>`);
    }
    $$renderer2.push(`<!--]--></div></div>`);
  });
}
function ChatStream($$renderer, $$props) {
  $$renderer.component(($$renderer2) => {
    var $$store_subs;
    let composerText = "";
    let inspecting = null;
    let snapshot = store_get($$store_subs ??= {}, "$chatState", chatState);
    function messageMeta(msg) {
      const raw = msg.metadata;
      if (raw && typeof raw === "object") return raw;
      return {};
    }
    function onWsChange(e) {
      const sel = e.target.value;
      if (sel && sel !== snapshot.activeWorkspaceId) {
        selectWorkspace(sel);
      }
    }
    function messageToLogos(msg) {
      const m = messageMeta(msg);
      return {
        id: String(msg.id ?? ""),
        body: String(msg.body ?? ""),
        intent: m.intent ?? null,
        persona: m.persona ?? null,
        confidence: m.confidence ?? null,
        thread_id: m.thread_id ?? null,
        execution_status: m.execution_status ?? null,
        metadata_json: m ?? null,
        created_at: msg.created_at ? String(msg.created_at) : null
      };
    }
    function onInspect(msg) {
      inspecting = inspecting?.id === msg.id ? null : msg;
      activeMessageId.set(msg.id);
    }
    $$renderer2.push(`<div class="chatstream svelte-12oolee"><div class="cs-topbar logos-mono svelte-12oolee">`);
    if (snapshot.workspaces.length > 0) {
      $$renderer2.push("<!--[0-->");
      $$renderer2.select(
        {
          class: "cs-select",
          value: snapshot.activeWorkspaceId ?? "",
          onchange: onWsChange,
          "aria-label": "Workspace"
        },
        ($$renderer3) => {
          $$renderer3.push(`<!--[-->`);
          const each_array = ensure_array_like(snapshot.workspaces);
          for (let $$index = 0, $$length = each_array.length; $$index < $$length; $$index++) {
            let ws = each_array[$$index];
            $$renderer3.option(
              { value: ws.id, class: "" },
              ($$renderer4) => {
                $$renderer4.push(`${escape_html(ws.name)}`);
              },
              "svelte-12oolee"
            );
          }
          $$renderer3.push(`<!--]-->`);
        },
        "svelte-12oolee"
      );
      $$renderer2.push(` <span class="cs-sep svelte-12oolee">/</span>`);
    } else {
      $$renderer2.push("<!--[-1-->");
    }
    $$renderer2.push(`<!--]--> <div class="cs-channels svelte-12oolee"><!--[-->`);
    const each_array_1 = ensure_array_like(snapshot.channels);
    for (let $$index_1 = 0, $$length = each_array_1.length; $$index_1 < $$length; $$index_1++) {
      let ch = each_array_1[$$index_1];
      $$renderer2.push(`<button${attr_class("cs-channel-btn svelte-12oolee", void 0, { "active": ch.id === snapshot.activeChannelId })}># ${escape_html(ch.name)}</button>`);
    }
    $$renderer2.push(`<!--]--></div> <span class="cs-spacer svelte-12oolee"></span> `);
    if (snapshot.status === "booting") {
      $$renderer2.push("<!--[0-->");
      $$renderer2.push(`<span class="cs-status-booting svelte-12oolee">CONNECTING…</span>`);
    } else if (snapshot.status === "error") {
      $$renderer2.push("<!--[1-->");
      $$renderer2.push(`<span class="cs-status-error svelte-12oolee"${attr("title", snapshot.error ?? "")}>ERR</span>`);
    } else if (snapshot.activeChannelId) {
      $$renderer2.push("<!--[2-->");
      $$renderer2.push(`<span class="cs-status-ready accent-verified svelte-12oolee">LIVE</span>`);
    } else {
      $$renderer2.push("<!--[-1-->");
    }
    $$renderer2.push(`<!--]--></div> <div class="cs-messages svelte-12oolee">`);
    if (snapshot.status === "booting") {
      $$renderer2.push("<!--[0-->");
      $$renderer2.push(`<div class="cs-state logos-mono svelte-12oolee">SUBSTRATE BOOTING — awaiting workspace connection…</div>`);
    } else if (snapshot.status === "error") {
      $$renderer2.push("<!--[1-->");
      $$renderer2.push(`<div class="cs-state cs-error logos-mono svelte-12oolee"><span class="accent-intent svelte-12oolee">[ERR]</span> ${escape_html(snapshot.error ?? "Unknown error")}</div>`);
    } else if (snapshot.messages.length === 0) {
      $$renderer2.push("<!--[2-->");
      $$renderer2.push(`<div class="cs-state logos-mono svelte-12oolee">${escape_html(snapshot.activeChannelId ? "No messages in this channel." : "Select a channel to begin.")}</div>`);
    } else {
      $$renderer2.push("<!--[-1-->");
      $$renderer2.push(`<!--[-->`);
      const each_array_2 = ensure_array_like(snapshot.messages);
      for (let $$index_2 = 0, $$length = each_array_2.length; $$index_2 < $$length; $$index_2++) {
        let msg = each_array_2[$$index_2];
        $$renderer2.push(`<div${attr_class("cs-row svelte-12oolee", void 0, {
          "cs-active": store_get($$store_subs ??= {}, "$activeMessageId", activeMessageId) === msg.id
        })}${attr("data-msg-id", msg.id)}>`);
        MessageFrame($$renderer2, { message: messageToLogos(msg), onInspect });
        $$renderer2.push(`<!----> `);
        if (inspecting?.id === msg.id) {
          $$renderer2.push("<!--[0-->");
          InspectorBlade($$renderer2, { message: inspecting, open: true });
        } else {
          $$renderer2.push("<!--[-1-->");
        }
        $$renderer2.push(`<!--]--></div>`);
      }
      $$renderer2.push(`<!--]-->`);
    }
    $$renderer2.push(`<!--]--></div> `);
    if (snapshot.status === "ready" && snapshot.activeChannelId) {
      $$renderer2.push("<!--[0-->");
      $$renderer2.push(`<div class="cs-composer svelte-12oolee"><textarea class="cs-input logos-mono svelte-12oolee" placeholder="Message…"${attr("rows", 2)}>`);
      const $$body = escape_html(composerText);
      if ($$body) {
        $$renderer2.push(`${$$body}`);
      }
      $$renderer2.push(`</textarea> <button class="cs-send-btn logos-mono svelte-12oolee"${attr("disabled", !composerText.trim(), true)}>SEND</button></div>`);
    } else {
      $$renderer2.push("<!--[-1-->");
    }
    $$renderer2.push(`<!--]--></div>`);
    if ($$store_subs) unsubscribe_stores($$store_subs);
  });
}
let _clusters = /* @__PURE__ */ new Map();
function getClusterMessageCount(clusterId) {
  const cl = _clusters.get(clusterId);
  return cl?.message_ids.length ?? 0;
}
function getClusters() {
  return [..._clusters.values()];
}
function SemanticThreadPane($$renderer, $$props) {
  $$renderer.component(($$renderer2) => {
    let activeTab = "threads";
    let clustering = false;
    let queryText = "";
    let searchResults = [];
    let searching = false;
    let clusters = derived(getClusters);
    let hasClusters = derived(() => clusters().length > 0);
    let hasSearchResults = derived(() => searchResults.length > 0);
    $$renderer2.push(`<aside class="logos-semantic-pane svelte-11ylbfe" aria-label="Semantic thread pane" role="complementary"><div class="logos-pane-tabs svelte-11ylbfe"><button type="button"${attr_class("logos-pane-tab svelte-11ylbfe", void 0, { "active": activeTab === "threads" })}>THREADS</button> <button type="button"${attr_class("logos-pane-tab svelte-11ylbfe", void 0, { "active": activeTab === "memory" })}>MEMORY</button> <button type="button" class="logos-pane-close svelte-11ylbfe" aria-label="Close semantic pane" title="Close (Esc)"><svg viewBox="0 0 24 24" width="11" height="11" aria-hidden="true"><path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" d="M6 6l12 12M18 6L6 18"></path></svg></button></div> `);
    {
      $$renderer2.push("<!--[0-->");
      $$renderer2.push(`<div class="logos-pane-body svelte-11ylbfe"><button type="button" class="logos-pane-btn svelte-11ylbfe"${attr("disabled", clustering, true)}>${escape_html("CLUSTER WORKSPACE")}</button> <div class="logos-pane-search svelte-11ylbfe"><input type="text" class="logos-pane-input svelte-11ylbfe" placeholder="thread retrieval query…"${attr("value", queryText)}${attr("disabled", searching, true)}/></div> `);
      if (hasSearchResults()) {
        $$renderer2.push("<!--[0-->");
        $$renderer2.push(`<div class="logos-pane-section svelte-11ylbfe"><div class="logos-pane-section-title svelte-11ylbfe">RETRIEVAL RESULTS</div> <!--[-->`);
        const each_array = ensure_array_like(searchResults);
        for (let $$index = 0, $$length = each_array.length; $$index < $$length; $$index++) {
          let node = each_array[$$index];
          $$renderer2.push(`<button type="button" class="logos-pane-node svelte-11ylbfe"><span class="node-marker svelte-11ylbfe">#NODE-${escape_html(node.id.slice(0, 8))}</span> <span class="node-score svelte-11ylbfe">(${escape_html(node.score.toFixed(2))})</span> <span class="node-preview svelte-11ylbfe">${escape_html(node.content.slice(0, 60))}</span></button>`);
        }
        $$renderer2.push(`<!--]--></div>`);
      } else {
        $$renderer2.push("<!--[-1-->");
      }
      $$renderer2.push(`<!--]--> `);
      if (hasClusters()) {
        $$renderer2.push("<!--[0-->");
        $$renderer2.push(`<div class="logos-pane-section svelte-11ylbfe"><div class="logos-pane-section-title svelte-11ylbfe">SEMANTIC THREADS</div> <!--[-->`);
        const each_array_1 = ensure_array_like(clusters());
        for (let $$index_1 = 0, $$length = each_array_1.length; $$index_1 < $$length; $$index_1++) {
          let cluster = each_array_1[$$index_1];
          $$renderer2.push(`<button type="button" class="logos-pane-cluster svelte-11ylbfe"><span class="cluster-label svelte-11ylbfe">${escape_html(cluster.label)}</span> <span class="cluster-count svelte-11ylbfe">(${escape_html(getClusterMessageCount(cluster.id))} msgs)</span></button>`);
        }
        $$renderer2.push(`<!--]--></div>`);
      } else if (!hasSearchResults()) {
        $$renderer2.push("<!--[1-->");
        $$renderer2.push(`<div class="logos-pane-empty svelte-11ylbfe">No clusters yet. Click CLUSTER WORKSPACE to analyze the current chat.</div>`);
      } else {
        $$renderer2.push("<!--[-1-->");
      }
      $$renderer2.push(`<!--]--></div>`);
    }
    $$renderer2.push(`<!--]--></aside>`);
  });
}
function _page($$renderer, $$props) {
  $$renderer.component(($$renderer2) => {
    var $$store_subs;
    let persona = "operator";
    $$renderer2.push(`<div class="console svelte-1uha8ag"><header class="console-topbar logos-mono svelte-1uha8ag"><span class="brand svelte-1uha8ag">LOGOS</span> <span class="spacer svelte-1uha8ag"></span> <span class="persona-tag accent-intent svelte-1uha8ag">PERSONA: ${escape_html(persona.toUpperCase())}</span> <button${attr_class("ghost svelte-1uha8ag", void 0, {
      "active": store_get($$store_subs ??= {}, "$semanticPaneOpen", semanticPaneOpen)
    })}>THREADS</button> <button${attr_class("ghost svelte-1uha8ag", void 0, {
      "active": store_get($$store_subs ??= {}, "$telemetryOpen", telemetryOpen)
    })}>TELEMETRY</button></header> <div${attr_class("console-body svelte-1uha8ag", void 0, {
      "semantic-open": store_get($$store_subs ??= {}, "$semanticPaneOpen", semanticPaneOpen)
    })}><section class="pane chat-pane svelte-1uha8ag"><div class="pane-head logos-mono svelte-1uha8ag">CHAT STREAM <span class="accent-thread">· substrate: clickclack API</span></div> <div class="pane-body chat-body svelte-1uha8ag">`);
    ChatStream($$renderer2);
    $$renderer2.push(`<!----></div></section> <aside${attr_class("pane right-pane svelte-1uha8ag", void 0, {
      "open": store_get($$store_subs ??= {}, "$semanticPaneOpen", semanticPaneOpen)
    })}>`);
    SemanticThreadPane($$renderer2);
    $$renderer2.push(`<!----></aside></div> <footer class="console-statusbar logos-mono svelte-1uha8ag"><span>⌘K palette</span><span>·</span><span>Alt inspect</span><span>·</span> <span>j/k navigate</span><span>·</span><span class="accent-thread">:${escape_html(persona)} switch</span></footer></div>`);
    if ($$store_subs) unsubscribe_stores($$store_subs);
  });
}
export {
  _page as default
};

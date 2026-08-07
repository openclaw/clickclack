import { b as escape_html, a as attr_class, s as store_get, u as unsubscribe_stores } from "../../chunks/index.js";
import { s as semanticPaneOpen } from "../../chunks/ui.js";
function _page($$renderer, $$props) {
  $$renderer.component(($$renderer2) => {
    var $$store_subs;
    let persona = "operator";
    $$renderer2.push(`<div class="console svelte-1uha8ag"><header class="console-topbar logos-mono svelte-1uha8ag"><span class="brand svelte-1uha8ag">LOGOS</span> <span class="spacer svelte-1uha8ag"></span> <span class="persona-tag accent-intent svelte-1uha8ag">PERSONA: ${escape_html(persona.toUpperCase())}</span> <button class="ghost svelte-1uha8ag">THREADS</button> <button class="ghost svelte-1uha8ag">TELEMETRY</button></header> <div class="console-body svelte-1uha8ag"><section class="pane chat-pane svelte-1uha8ag"><div class="pane-head logos-mono svelte-1uha8ag">CHAT STREAM <span class="accent-thread">· substrate: clickclack API</span></div> <div class="chat-placeholder logos-mono svelte-1uha8ag"><p>Chat pane — the only piece inherited from clickclack.</p> <p>Wired to the clickclack API (messages, realtime) as substrate.</p> <p class="accent-verified">[STATUS: LOGOS console — awaiting workspace connection]</p></div></section> <aside${attr_class("pane right-pane svelte-1uha8ag", void 0, {
      "open": store_get($$store_subs ??= {}, "$semanticPaneOpen", semanticPaneOpen)
    })}><div class="pane-head logos-mono svelte-1uha8ag">SEMANTIC THREADS</div> <div class="pane-body logos-mono svelte-1uha8ag"><p class="muted svelte-1uha8ag">Cluster workspace → CL-01, CL-02…</p> <p class="muted svelte-1uha8ag">Cross-thread retrieval → #NODE-XX (score)</p></div></aside></div> <footer class="console-statusbar logos-mono svelte-1uha8ag"><span>⌘K palette</span><span>·</span><span>Alt inspect</span><span>·</span> <span>j/k navigate</span><span>·</span><span class="accent-thread">:${escape_html(persona)} switch</span></footer></div>`);
    if ($$store_subs) unsubscribe_stores($$store_subs);
  });
}
export {
  _page as default
};

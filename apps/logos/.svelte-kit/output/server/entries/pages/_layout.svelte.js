import { a as attr_class, e as ensure_array_like, b as escape_html, d as derived, s as store_get, c as slot, u as unsubscribe_stores } from "../../chunks/index.js";
import { t as telemetryOpen, i as inspectMode } from "../../chunks/ui.js";
function SemanticMargin($$renderer, $$props) {
  $$renderer.component(($$renderer2) => {
    let isInspect = false;
    let { messageCount = 0, intents = [] } = $$props;
    $$renderer2.push(`<aside${attr_class("semantic-margin svelte-1lgz3m2", void 0, { "inspect": isInspect })} aria-label="semantic margin"><div class="margin-grid svelte-1lgz3m2" aria-hidden="true"><!--[-->`);
    const each_array = ensure_array_like(Array.from({ length: 24 }));
    for (let i = 0, $$length = each_array.length; i < $$length; i++) {
      each_array[i];
      $$renderer2.push(`<span class="grid-mark svelte-1lgz3m2"></span>`);
    }
    $$renderer2.push(`<!--]--></div> <div class="margin-lines svelte-1lgz3m2"><!--[-->`);
    const each_array_1 = ensure_array_like(Array.from({ length: messageCount }));
    for (let i = 0, $$length = each_array_1.length; i < $$length; i++) {
      each_array_1[i];
      $$renderer2.push(`<span class="line-counter svelte-1lgz3m2">${escape_html(String(i + 1).padStart(3, "0"))}</span>`);
    }
    $$renderer2.push(`<!--]--></div> <div class="margin-intents svelte-1lgz3m2"><!--[-->`);
    const each_array_2 = ensure_array_like(intents);
    for (let $$index_2 = 0, $$length = each_array_2.length; $$index_2 < $$length; $$index_2++) {
      let intent = each_array_2[$$index_2];
      $$renderer2.push(`<span${attr_class("intent-tick svelte-1lgz3m2", void 0, {
        "intent-ask": intent === "ask",
        "intent-command": intent === "command",
        "intent-reflect": intent === "reflect",
        "intent-draft": intent === "draft",
        "intent-clarify": intent === "clarify",
        "intent-explore": intent === "explore"
      })}></span>`);
    }
    $$renderer2.push(`<!--]--></div></aside>`);
  });
}
function CommandPalette($$renderer, $$props) {
  $$renderer.component(($$renderer2) => {
    {
      $$renderer2.push("<!--[-1-->");
    }
    $$renderer2.push(`<!--]-->`);
  });
}
function TelemetryRail($$renderer, $$props) {
  $$renderer.component(($$renderer2) => {
    let {
      intents = null,
      personas = null,
      pipeline = null,
      tokens = null
    } = $$props;
    const fmtIntents = derived(() => intents != null ? String(intents) : "--");
    const fmtPersonas = derived(() => personas != null ? String(personas) : "--");
    const fmtPipeline = derived(() => pipeline ?? "--");
    const fmtTokens = derived(() => tokens != null ? tokens.toLocaleString() : "--");
    $$renderer2.push(`<div class="telemetry-rail svelte-1xkmetj" aria-label="Telemetry rail"><div class="rail-indicator svelte-1xkmetj"><span class="rail-label svelte-1xkmetj">INS</span> <span class="rail-value svelte-1xkmetj">${escape_html(fmtIntents())}</span></div> <div class="rail-indicator svelte-1xkmetj"><span class="rail-label svelte-1xkmetj">PER</span> <span class="rail-value svelte-1xkmetj">${escape_html(fmtPersonas())}</span></div> <div class="rail-indicator svelte-1xkmetj"><span class="rail-label svelte-1xkmetj">PPL</span> <span class="rail-value svelte-1xkmetj">${escape_html(fmtPipeline())}</span></div> <div class="rail-indicator svelte-1xkmetj"><span class="rail-label svelte-1xkmetj">TKN</span> <span class="rail-value svelte-1xkmetj">${escape_html(fmtTokens())}</span></div></div>`);
  });
}
function _layout($$renderer, $$props) {
  $$renderer.component(($$renderer2) => {
    var $$store_subs;
    $$renderer2.push(`<div${attr_class("logos-shell", void 0, {
      "inspect": store_get($$store_subs ??= {}, "$inspectMode", inspectMode),
      "telemetry": store_get($$store_subs ??= {}, "$telemetryOpen", telemetryOpen)
    })}>`);
    SemanticMargin($$renderer2, {});
    $$renderer2.push(`<!----> <main class="logos-main"><!--[-->`);
    slot($$renderer2, $$props, "default", {});
    $$renderer2.push(`<!--]--></main> `);
    if (store_get($$store_subs ??= {}, "$telemetryOpen", telemetryOpen)) {
      $$renderer2.push("<!--[0-->");
      TelemetryRail($$renderer2, { intents: null, personas: null, pipeline: null, tokens: null });
    } else {
      $$renderer2.push("<!--[-1-->");
    }
    $$renderer2.push(`<!--]--> `);
    CommandPalette($$renderer2);
    $$renderer2.push(`<!----></div>`);
    if ($$store_subs) unsubscribe_stores($$store_subs);
  });
}
export {
  _layout as default
};

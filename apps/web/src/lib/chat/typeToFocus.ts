type RedirectTypingOptions = {
  authRequired: boolean;
  isModalOpen: () => boolean;
  messageInput: HTMLTextAreaElement | null;
  replyInput: HTMLTextAreaElement | null;
  target: () => HTMLTextAreaElement | null;
};

let pointerFocusedControl: HTMLElement | null = null;

const KEY_CONSUMING_ROLES = new Set([
  "button",
  "checkbox",
  "combobox",
  "link",
  "listbox",
  "menu",
  "menubar",
  "menuitem",
  "menuitemcheckbox",
  "menuitemradio",
  "option",
  "radio",
  "radiogroup",
  "slider",
  "spinbutton",
  "switch",
  "tab",
  "tablist",
  "textbox",
  "tree",
  "treeitem",
]);

const KEY_CONSUMING_TAGS = new Set([
  "INPUT",
  "TEXTAREA",
  "SELECT",
  "BUTTON",
  "A",
  "DETAILS",
  "SUMMARY",
  "VIDEO",
  "AUDIO",
]);

export function rememberTypeToFocusPointer(event: PointerEvent) {
  const target = event.target instanceof HTMLElement ? event.target : null;
  pointerFocusedControl =
    target?.closest<HTMLElement>("a, button, [role='button'], [role='link'], [tabindex]") || null;
}

function isEditableElement(el: HTMLElement | null): boolean {
  if (!el) return false;
  if (el.isContentEditable) return true;
  if (el instanceof HTMLInputElement) {
    const t = (el.type || "text").toLowerCase();
    return (
      t !== "checkbox" &&
      t !== "radio" &&
      t !== "button" &&
      t !== "submit" &&
      t !== "reset" &&
      t !== "file"
    );
  }
  if (el instanceof HTMLTextAreaElement) return true;
  return false;
}

function consumesKeystrokes(el: HTMLElement | null): boolean {
  if (!el) return false;
  if (isChatSurfaceAction(el)) return false;
  if (
    pointerFocusedControl &&
    (el === pointerFocusedControl || pointerFocusedControl.contains(el))
  ) {
    return false;
  }
  if (KEY_CONSUMING_TAGS.has(el.tagName)) return true;
  const role = el.getAttribute("role");
  if (role && KEY_CONSUMING_ROLES.has(role)) return true;
  const tabindex = el.getAttribute("tabindex");
  if (tabindex !== null && tabindex !== "-1" && el.hasAttribute("aria-keyshortcuts")) return true;
  return false;
}

function isChatSurfaceAction(el: HTMLElement): boolean {
  if (!el.closest(".messages, .thread")) return false;
  if (el instanceof HTMLButtonElement || el instanceof HTMLAnchorElement) return true;
  const role = el.getAttribute("role");
  return role === "button" || role === "link";
}

// Numbered choices inside messages keep their digits; other typing still reaches the composer.
function claimsShortcutKey(el: HTMLElement | null, key: string): boolean {
  const owner = el?.closest<HTMLElement>("[data-shortcut-keys]");
  return owner?.dataset.shortcutKeys?.split(" ").includes(key) ?? false;
}

function hasMessageTextSelection(): boolean {
  const sel = typeof window !== "undefined" ? window.getSelection() : null;
  if (!sel || sel.isCollapsed || sel.rangeCount === 0) return false;
  const node = sel.getRangeAt(0).commonAncestorContainer;
  if (!node) return false;
  const host = node.nodeType === Node.ELEMENT_NODE ? (node as HTMLElement) : node.parentElement;
  return !!host?.closest(".messages, .thread, .markdown");
}

function shouldRedirectKeystroke(event: KeyboardEvent, options: RedirectTypingOptions): boolean {
  if (options.authRequired) return false;
  if (options.isModalOpen()) return false;
  if (event.defaultPrevented) return false;
  if (event.isComposing || event.keyCode === 229) return false;
  if (event.ctrlKey || event.metaKey || event.altKey) return false;
  if (event.key.length !== 1) return false;
  if (hasMessageTextSelection()) return false;
  const active = document.activeElement as HTMLElement | null;
  if (active === options.messageInput || active === options.replyInput) return false;
  if (isEditableElement(active)) return false;
  if (claimsShortcutKey(active, event.key)) return false;
  if (consumesKeystrokes(active)) return false;
  return true;
}

export function redirectTypingToComposer(event: KeyboardEvent, options: RedirectTypingOptions) {
  if (!shouldRedirectKeystroke(event, options)) return;
  const target = options.target();
  if (!target || target.disabled || target.readOnly) return;
  if (event.key === " ") event.preventDefault();
  target.focus({ preventScroll: true });
  const len = target.value.length;
  target.setSelectionRange(len, len);
  if (event.key === " ") {
    const start = target.selectionStart ?? len;
    const end = target.selectionEnd ?? len;
    target.setRangeText(" ", start, end, "end");
    target.dispatchEvent(new Event("input", { bubbles: true }));
  }
}

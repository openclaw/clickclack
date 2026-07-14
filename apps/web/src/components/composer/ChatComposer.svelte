<script lang="ts">
  import { tick } from "svelte";
  import { autoGrow } from "../../lib/actions/autogrow";
  import { avatarInitial, handleLabel } from "../../lib/chat/people";
  import { formatBytes, isImageUpload, uploadURL } from "../../lib/uploads";
  import type { GifItem } from "../../lib/gifs";
  import type { Message, SlashCommand, Upload, User, WorkspaceBotCommand } from "../../lib/types";
  import ComposerToolbar from "./ComposerToolbar.svelte";
  import GifPicker from "./GifPicker.svelte";
  import ReplyPreview from "./ReplyPreview.svelte";

  type ActiveToken = {
    kind: "slash" | "mention";
    start: number;
    end: number;
    query: string;
    raw: string;
  };

  type ComposerSuggestion = {
    id: string;
    kind: "slash" | "mention";
    label: string;
    detail: string;
    insertText: string;
    sortText: string;
    hint?: string;
    source?: string;
  };

  type Props = {
    value: string;
    placeholder: string;
    ariaLabel: string;
    submitLabel: string;
    formClass?: string;
    pendingUpload?: Upload | null;
    replyTarget?: Message | null;
    showUpload?: boolean;
    showToolbar?: boolean;
    showGifPicker?: boolean;
    gifQuery?: string;
    filteredGifs?: GifItem[];
    slashCommands?: SlashCommand[];
    botCommands?: WorkspaceBotCommand[];
    mentionPeople?: User[];
    onValue: (value: string) => void;
    onSubmit: () => void;
    onKeydown: (event: KeyboardEvent) => void;
    onFocus: () => void;
    onInputRef: (node: HTMLTextAreaElement | null) => void;
    onUploadFile?: (event: Event) => void;
    onRemoveUpload?: () => void;
    onClearReply?: () => void;
    onApplyMarkdownWrap?: (before: string, after?: string) => void;
    onAppendToComposer?: (snippet: string) => void;
    onToggleGif?: () => void;
    onGifQuery?: (value: string) => void;
    onPickGif?: (url: string, title: string) => void;
  };

  let {
    value,
    placeholder,
    ariaLabel,
    submitLabel,
    formClass = "composer",
    pendingUpload = null,
    replyTarget = null,
    showUpload = false,
    showToolbar = false,
    showGifPicker = false,
    gifQuery = "",
    filteredGifs = [],
    slashCommands = [],
    botCommands = [],
    mentionPeople = [],
    onValue,
    onSubmit,
    onKeydown,
    onFocus,
    onInputRef,
    onUploadFile = () => {},
    onRemoveUpload = () => {},
    onClearReply = () => {},
    onApplyMarkdownWrap = () => {},
    onAppendToComposer = () => {},
    onToggleGif = () => {},
    onGifQuery = () => {},
    onPickGif = () => {},
  }: Props = $props();

  let input: HTMLTextAreaElement | null = $state(null);
  let caret = $state(0);
  let dismissedToken = $state("");
  let selectedSuggestionIndex = $state(0);

  const activeToken = $derived.by(() => detectActiveToken(value, caret));
  const activeSuggestions = $derived.by(() => {
    if (!activeToken || tokenKey(activeToken) === dismissedToken) return [];
    return activeToken.kind === "slash"
      ? slashSuggestions(activeToken)
      : mentionSuggestions(activeToken);
  });

  $effect(() => {
    onInputRef(input);
    return () => onInputRef(null);
  });

  $effect(() => {
    if (activeSuggestions.length === 0) {
      selectedSuggestionIndex = 0;
      return;
    }
    if (selectedSuggestionIndex >= activeSuggestions.length) selectedSuggestionIndex = 0;
  });

  function detectActiveToken(text: string, position: number): ActiveToken | null {
    const safePosition = Math.max(0, Math.min(position || text.length, text.length));
    const before = text.slice(0, safePosition);
    const match = /(^|\s)([/@][^\s]*)$/.exec(before);
    if (!match) return null;
    const raw = match[2];
    const start = before.length - raw.length;
    if (raw.startsWith("/") && start !== 0) return null;
    return {
      kind: raw.startsWith("/") ? "slash" : "mention",
      start,
      end: safePosition,
      query: raw.slice(1).toLowerCase(),
      raw,
    };
  }

  function tokenKey(token: ActiveToken): string {
    return `${token.kind}:${token.start}:${token.raw}`;
  }

  function updateCaret(node: HTMLTextAreaElement | null = input) {
    caret = node?.selectionStart ?? value.length;
  }

  function normalizedCommand(command: string): string {
    return command.startsWith("/") ? command : `/${command}`;
  }

  function slashSuggestions(token: ActiveToken): ComposerSuggestion[] {
    const query = token.query;
    const registered = slashCommands
      .filter((command) => !command.revoked_at)
      .map((command) => {
        const label = normalizedCommand(command.command);
        return {
          id: command.id,
          kind: "slash" as const,
          label,
          detail: command.description || "Slash command",
          insertText: `${label} `,
          sortText: label.slice(1).toLowerCase(),
        };
      });
    // Bot-declared menu entries merge in behind HTTP-registered commands:
    // on a name collision the registered command wins (it is the one the
    // send path dispatches).
    const taken = new Set(registered.map((suggestion) => suggestion.label));
    const declared = botCommands
      .filter((command) => !taken.has(normalizedCommand(command.command)))
      .map((command) => {
        const label = normalizedCommand(command.command);
        return {
          id: command.id,
          kind: "slash" as const,
          label,
          detail: command.description,
          insertText: `${label} `,
          sortText: label.slice(1).toLowerCase(),
          hint: command.args_hint || undefined,
          source: command.bot.handle ? `@${command.bot.handle}` : command.bot.display_name,
        };
      });
    return [...registered, ...declared]
      .filter((suggestion) => !query || suggestion.sortText.includes(query))
      .sort((a, b) => Number(!a.sortText.startsWith(query)) - Number(!b.sortText.startsWith(query)) || a.sortText.localeCompare(b.sortText))
      .slice(0, 6);
  }

  function mentionText(person: User): string {
    return handleLabel(person.handle || person.display_name.replace(/\s+/g, ""));
  }

  function mentionSuggestions(token: ActiveToken): ComposerSuggestion[] {
    const query = token.query;
    const seen = new Set<string>();
    return mentionPeople
      .filter((person) => {
        if (!person.id || seen.has(person.id)) return false;
        seen.add(person.id);
        return true;
      })
      .map((person) => {
        const label = mentionText(person);
        const searchable = `${person.handle || ""} ${person.display_name}`.trim().toLowerCase();
        return {
          id: person.id,
          kind: "mention" as const,
          label,
          detail: person.kind === "bot" ? `${person.display_name} · bot` : person.display_name,
          insertText: `${label} `,
          sortText: searchable,
        };
      })
      .filter((suggestion) => !query || suggestion.sortText.includes(query))
      .sort((a, b) => Number(!a.sortText.startsWith(query)) - Number(!b.sortText.startsWith(query)) || a.sortText.localeCompare(b.sortText))
      .slice(0, 6);
  }

  function pickSuggestion(suggestion: ComposerSuggestion) {
    if (!activeToken) return;
    const nextValue = `${value.slice(0, activeToken.start)}${suggestion.insertText}${value.slice(activeToken.end)}`;
    const nextCaret = activeToken.start + suggestion.insertText.length;
    onValue(nextValue);
    void tick().then(() => {
      input?.focus();
      input?.setSelectionRange(nextCaret, nextCaret);
      caret = nextCaret;
    });
  }

  function handleInput(event: Event) {
    const node = event.currentTarget as HTMLTextAreaElement;
    onValue(node.value);
    updateCaret(node);
  }

  function handleFocus() {
    updateCaret();
    onFocus();
  }

  function handleKeydown(event: KeyboardEvent) {
    if (activeSuggestions.length > 0) {
      if (event.key === "ArrowDown") {
        event.preventDefault();
        selectedSuggestionIndex = (selectedSuggestionIndex + 1) % activeSuggestions.length;
        return;
      }
      if (event.key === "ArrowUp") {
        event.preventDefault();
        selectedSuggestionIndex = (selectedSuggestionIndex - 1 + activeSuggestions.length) % activeSuggestions.length;
        return;
      }
      if (event.key === "Enter" || event.key === "Tab") {
        event.preventDefault();
        pickSuggestion(activeSuggestions[selectedSuggestionIndex]);
        return;
      }
      if (event.key === "Escape" && activeToken) {
        event.preventDefault();
        dismissedToken = tokenKey(activeToken);
        return;
      }
    }
    onKeydown(event);
  }
</script>

<form
  class={formClass}
  onsubmit={(event) => {
    event.preventDefault();
    onSubmit();
  }}
>
  {#if showGifPicker}
    <GifPicker
      gifs={filteredGifs}
      query={gifQuery}
      onQuery={onGifQuery}
      onPick={onPickGif}
    />
  {/if}
  {#if activeSuggestions.length > 0}
    <div class="composer-suggestions" role="listbox" aria-label={activeToken?.kind === "slash" ? "Slash command suggestions" : "Mention suggestions"}>
      {#each activeSuggestions as suggestion, index (suggestion.id)}
        <button
          type="button"
          class:active={index === selectedSuggestionIndex}
          role="option"
          aria-selected={index === selectedSuggestionIndex}
          onmousedown={(event) => event.preventDefault()}
          onclick={() => pickSuggestion(suggestion)}
        >
          <span class="suggestion-mark" aria-hidden="true">
            {#if suggestion.kind === "slash"}
              /
            {:else}
              {avatarInitial(suggestion.detail)}
            {/if}
          </span>
          <span class="suggestion-copy">
            <strong>{suggestion.label}{#if suggestion.hint}<em class="suggestion-hint"> {suggestion.hint}</em>{/if}</strong>
            <span>{suggestion.detail}</span>
          </span>
          <span class="suggestion-kind">{suggestion.source ?? (suggestion.kind === "slash" ? "command" : "mention")}</span>
        </button>
      {/each}
    </div>
  {/if}
  <div class="composer-card">
    {#if pendingUpload}
      <div class="composer-attachment">
        <span class="attachment-icon" aria-hidden="true">
          <svg viewBox="0 0 24 24" width="14" height="14"><path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" d="M21.44 11.05 12.5 20a6 6 0 0 1-8.49-8.49l8.49-8.48a4 4 0 0 1 5.66 5.66l-8.49 8.49a2 2 0 0 1-2.83-2.83L13.41 7.5"/></svg>
        </span>
        {#if isImageUpload(pendingUpload)}
          <img class="pending-image" src={uploadURL(pendingUpload)} alt={pendingUpload.filename} />
        {/if}
        <span class="attachment-name">{pendingUpload.filename} · {formatBytes(pendingUpload.byte_size)}</span>
        <button type="button" class="attachment-remove" aria-label="Remove attachment" onclick={onRemoveUpload}>×</button>
      </div>
    {/if}
    {#if replyTarget}
      <ReplyPreview target={replyTarget} onClear={onClearReply} />
    {/if}
    <div class="composer-row">
      {#if showUpload}
        <label class="composer-icon" title="Upload file">
          <input type="file" aria-label="Upload file" onchange={onUploadFile} />
          <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
            <path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" d="M21.44 11.05 12.5 20a6 6 0 0 1-8.49-8.49l8.49-8.48a4 4 0 0 1 5.66 5.66l-8.49 8.49a2 2 0 0 1-2.83-2.83L13.41 7.5"/>
          </svg>
        </label>
      {/if}
      <textarea
        bind:this={input}
        value={value}
        use:autoGrow={value}
        rows="1"
        {placeholder}
        aria-label={ariaLabel}
        oninput={handleInput}
        onfocus={handleFocus}
        onkeydown={handleKeydown}
        onkeyup={() => updateCaret()}
        onmouseup={() => updateCaret()}
        onselect={() => updateCaret()}
      ></textarea>
      <button type="submit" class="send" aria-label={submitLabel} disabled={!value.trim()}>
        <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
          <path fill="currentColor" d="M3 3.5 21 12 3 20.5l3.6-7.5L15 12 6.6 11l-3.6-7.5Z"/>
        </svg>
      </button>
    </div>
    {#if showToolbar}
      <ComposerToolbar
        showGifPicker={showGifPicker}
        onWrap={onApplyMarkdownWrap}
        onAppend={onAppendToComposer}
        onToggleGif={onToggleGif}
      />
    {/if}
  </div>
</form>

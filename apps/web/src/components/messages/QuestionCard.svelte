<script lang="ts">
  import { untrack } from "svelte";
  import { APIError, api, readableAPIError } from "../../lib/api";
  import { newNonce } from "../../lib/chat/messages";
  import { markdown } from "../../lib/format";
  import {
    canAnswerQuestion,
    emptyQuestionDraft,
    isQuickQuestion,
    openQuestionOther,
    questionCountdown,
    questionDraftAnswers,
    questionDraftComplete,
    questionHeadline,
    questionItemAnswers,
    questionShortcutKeys,
    questionSummary,
    recallQuestionDraft,
    rememberQuestionDraft,
    setQuestionOther,
    toggleQuestionOption,
    type QuestionDraft,
  } from "../../lib/questions";
  import type { Message, MessageQuestion, QuestionItem, User } from "../../lib/types";

  type Props = {
    message: Message;
    question: MessageQuestion;
    currentUserID?: string;
    people?: User[];
  };

  let { message, question, currentUserID, people = [] }: Props = $props();

  // The card remounts when the question version changes, so these capture that version.
  const draftKey = untrack(() => `${message.id}:${question.version}`);
  let local = $state<MessageQuestion | null>(null);
  let draft = $state<QuestionDraft>(
    untrack(() => recallQuestionDraft(draftKey) ?? emptyQuestionDraft(question)),
  );
  let pending = $state("");
  let error = $state("");
  let now = $state(Date.now());
  const nonce = newNonce();
  const titleID = untrack(() => `question-${message.id}`);

  const shown = $derived(local && local.version > question.version ? local : question);
  const countdown = $derived(questionCountdown(shown.expires_at, now));
  const expiredHere = $derived(shown.status === "open" && countdown.expired);
  const view = $derived(expiredHere ? { ...shown, status: "expired" as const } : shown);
  const askerName = $derived(message.author?.display_name || "the agent");
  const headline = $derived(questionHeadline(view, askerName, now));
  const answerable = $derived(canAnswerQuestion(view, currentUserID));
  const quick = $derived(isQuickQuestion(shown));
  const complete = $derived(questionDraftComplete(shown, draft));
  const summary = $derived(questionSummary(view));
  const responderNames = $derived(
    (shown.responder_user_ids ?? []).map((id) => {
      const person = people.find((candidate) => candidate.id === id);
      return person?.handle ? `@${person.handle}` : person?.display_name || "a listed person";
    }),
  );

  $effect(() => {
    if (view.status !== "open" && view.status !== "submitted") return;
    const timer = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(timer);
  });

  function updateDraft(next: QuestionDraft) {
    draft = next;
    rememberQuestionDraft(draftKey, next);
  }

  async function send(body: Record<string, unknown>, busy: string) {
    if (pending || !answerable) return;
    pending = busy;
    error = "";
    try {
      const data = await api<{ message: Message }>(`/api/messages/${message.id}/question/answers`, {
        method: "POST",
        // The version shown stops a retry from answering a question the bot reopened.
        body: JSON.stringify({ ...body, nonce, expected_version: shown.version }),
      });
      if (data.message.question) local = data.message.question;
    } catch (failure) {
      error = readableAPIError(failure, "Could not send your answer");
      if (failure instanceof APIError && failure.status === 409) {
        try {
          const fresh = await api<{ message: Message }>(`/api/messages/${message.id}`);
          if (fresh.message.question) local = fresh.message.question;
        } catch {
          // The realtime update refreshes the card when this lookup fails.
        }
      }
    } finally {
      pending = "";
    }
  }

  function choose(item: QuestionItem, label: string) {
    if (!answerable || pending) return;
    if (quick) {
      void send({ answers: { [item.id]: [label] } }, label);
      return;
    }
    updateDraft(toggleQuestionOption(draft, item, label));
  }

  function submitForm() {
    if (complete) void send({ answers: questionDraftAnswers(shown, draft) }, "form");
  }

  function focusOther(item: QuestionItem) {
    queueMicrotask(() => document.getElementById(`question-other-${message.id}-${item.id}`)?.focus());
  }

  function handleItemKeydown(event: KeyboardEvent, item: QuestionItem) {
    if (event.target instanceof HTMLInputElement || event.isComposing || event.metaKey || event.ctrlKey || event.altKey) return;
    if (!/^[1-9]$/.test(event.key) || !answerable) return;
    const index = Number(event.key) - 1;
    const options = item.options ?? [];
    if (index < options.length) {
      event.preventDefault();
      choose(item, options[index].label);
    } else if (index === options.length && item.allow_other && !quick) {
      event.preventDefault();
      updateDraft(openQuestionOther(draft, item));
      focusOther(item);
    }
  }

  function handleOtherKeydown(event: KeyboardEvent, item: QuestionItem) {
    if (event.isComposing) return;
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      submitForm();
    } else if (event.key === "Escape") {
      event.preventDefault();
      const selected = item.multi_select ? (draft[item.id]?.selected ?? []) : [];
      updateDraft({ ...draft, [item.id]: { selected, other: "", otherOpen: false } });
    }
  }
</script>

<section
  class="question-card question-card--{headline.tone}"
  role="group"
  aria-labelledby={titleID}
  data-question-status={view.status}
>
  <header class="question-card__head">
    <span class="question-eyebrow" id={titleID} aria-live="polite">
      {#if headline.tone === "open" || headline.tone === "waiting"}<span class="question-eyebrow__dot" aria-hidden="true"></span>{/if}
      {headline.label}
    </span>
    {#if view.status === "open"}
      {#if shown.items.length > 1}<span class="question-card__meta">{shown.items.length} questions</span>{/if}
      <span class="question-timer" class:question-timer--urgent={countdown.urgent} title="Expires {new Date(shown.expires_at).toLocaleString()}">
        <span aria-hidden="true">⏱</span> {countdown.label}
      </span>
    {:else if view.response?.responder}
      <span class="question-card__meta">by {view.response.responder.display_name}</span>
    {/if}
  </header>

  {#if shown.title && view.status === "open"}
    <p class="question-card__title">{shown.title}</p>
  {/if}
  {#if view.note && view.status === "open"}
    <!-- A reopened question explains what the bot could not use. -->
    <p class="question-reopened" role="note">{view.note}</p>
  {/if}

  {#if view.status === "open"}
    {#each shown.items as item, itemIndex (item.id)}
      {@const entry = draft[item.id]}
      {@const answered = questionItemAnswers(item, entry).length > 0}
      <div class="question-item" class:is-answered={answered && !quick}>
        <div class="question-item__label">
          {#if shown.items.length > 1}<span class="question-item__number" aria-hidden="true">{itemIndex + 1}</span>{/if}
          <span class="question-chip">{item.header}</span>
          {#if item.multi_select}<span class="question-item__hint">Choose any</span>{/if}
        </div>
        <div class="question-item__prompt markdown">{@html markdown(item.prompt)}</div>
        {#if (item.options?.length ?? 0) > 0}
          <!-- svelte-ignore a11y_interactive_supports_focus -->
          <div
            class="question-options"
            class:question-options--stack={quick}
            role={item.multi_select ? "group" : "radiogroup"}
            aria-label={item.header}
            data-shortcut-keys={questionShortcutKeys(item, quick)}
            onkeydown={(event) => handleItemKeydown(event, item)}
          >
            {#each item.options ?? [] as option, optionIndex (option.label)}
              {@const selected = entry?.selected.includes(option.label) ?? false}
              <button
                type="button"
                class="question-key"
                class:is-selected={selected || pending === option.label}
                class:is-pending={pending === option.label}
                role={item.multi_select ? "checkbox" : "radio"}
                aria-checked={selected}
                aria-keyshortcuts={optionIndex < 9 ? String(optionIndex + 1) : undefined}
                disabled={!answerable || Boolean(pending)}
                onclick={() => choose(item, option.label)}
              >
                {#if optionIndex < 9}<kbd>{optionIndex + 1}</kbd>{/if}
                <span class="question-key__label">{option.label}</span>
                <span class="question-key__mark" class:question-key__mark--box={item.multi_select} aria-hidden="true"></span>
                {#if option.description}<small>{option.description}</small>{/if}
              </button>
            {/each}
            {#if item.allow_other && !quick}
              {#if entry?.otherOpen}
                <label class="question-other">
                  {#if (item.options?.length ?? 0) < 9}<kbd>{(item.options?.length ?? 0) + 1}</kbd>{/if}
                  <input
                    id="question-other-{message.id}-{item.id}"
                    type="text"
                    maxlength="2000"
                    autocomplete="off"
                    placeholder={item.other_placeholder || "Your answer"}
                    aria-label="Other answer for {item.header}"
                    data-handles-escape
                    value={entry.other}
                    disabled={!answerable || Boolean(pending)}
                    oninput={(event) => updateDraft(setQuestionOther(draft, item, event.currentTarget.value))}
                    onkeydown={(event) => handleOtherKeydown(event, item)}
                  />
                </label>
              {:else}
                <button
                  type="button"
                  class="question-key question-key--other"
                  aria-keyshortcuts={(item.options?.length ?? 0) < 9 ? String((item.options?.length ?? 0) + 1) : undefined}
                  disabled={!answerable || Boolean(pending)}
                  onclick={() => {
                    updateDraft(openQuestionOther(draft, item));
                    focusOther(item);
                  }}
                >
                  {#if (item.options?.length ?? 0) < 9}<kbd>{(item.options?.length ?? 0) + 1}</kbd>{/if}
                  <span class="question-key__label">{item.other_placeholder || "Other..."}</span>
                </button>
              {/if}
            {/if}
          </div>
        {:else}
          <input
            class="question-free"
            type="text"
            maxlength="2000"
            autocomplete="off"
            placeholder="Your answer"
            aria-label={item.header}
            value={entry?.other ?? ""}
            disabled={!answerable || Boolean(pending)}
            oninput={(event) => updateDraft(setQuestionOther(draft, item, event.currentTarget.value))}
            onkeydown={(event) => {
              if (event.key === "Enter" && !event.isComposing) {
                event.preventDefault();
                submitForm();
              }
            }}
          />
        {/if}
        {#if item.url}
          <a class="question-link" href={item.url} target="_blank" rel="noopener noreferrer">Open link ↗</a>
        {/if}
      </div>
    {/each}

    {#if !answerable}
      <p class="question-lock">
        {#if !currentUserID}
          Sign in to answer this question.
        {:else}
          Only {responderNames.join(", ")} can answer this question.
        {/if}
      </p>
    {/if}

    {#if error}
      <p class="question-error" role="alert">{error}</p>
    {/if}

    {#if answerable}
      <footer class="question-card__foot">
        {#if quick}
          <span class="question-card__hint">{pending ? `Sending "${pending}"...` : "Tap an answer or press its number"}</span>
        {:else}
          <button type="button" class="question-send" disabled={!complete || Boolean(pending)} onclick={submitForm}>
            {pending === "form" ? "Sending..." : shown.items.length > 1 ? "Send answers" : "Send answer"}
          </button>
        {/if}
        {#if shown.allow_skip}
          <button type="button" class="question-skip" disabled={Boolean(pending)} onclick={() => void send({ skip: true }, "skip")}>
            {pending === "skip" ? "Skipping..." : "Skip"}
          </button>
        {/if}
        {#if !quick && shown.items.length > 1}
          <span class="question-progress" aria-label="{shown.items.filter((item) => questionItemAnswers(item, draft[item.id]).length > 0).length} of {shown.items.length} answered">
            {#each shown.items as item (item.id)}
              <i class:is-on={questionItemAnswers(item, draft[item.id]).length > 0}></i>
            {/each}
          </span>
        {/if}
        {#if responderNames.length > 0}
          <span class="question-card__note">Only {responderNames.join(", ")} can answer</span>
        {/if}
      </footer>
    {/if}
  {:else}
    {#if summary.length > 0}
      <dl class="question-summary">
        {#each summary as line (line.id)}
          <dt>{line.header}</dt>
          <dd>
            {#each line.values as value (value.text)}
              <span class="question-answer" class:question-answer--typed={value.typed}>{value.typed ? `"${value.text}"` : value.text}</span>
            {/each}
          </dd>
        {/each}
      </dl>
    {:else if view.status === "expired"}
      <p class="question-card__detail">No answer before {new Date(shown.expires_at).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" })}.</p>
    {/if}
    {#if view.note}
      <p class="question-card__detail">{view.note}</p>
    {/if}
  {/if}
</section>

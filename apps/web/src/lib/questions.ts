import type { MessageQuestion, QuestionItem } from "./types";

export type QuestionItemDraft = {
  selected: string[];
  other: string;
  otherOpen: boolean;
};

export type QuestionDraft = Record<string, QuestionItemDraft>;

export function emptyQuestionDraft(question: Pick<MessageQuestion, "items">): QuestionDraft {
  return Object.fromEntries(
    question.items.map((item) => [item.id, { selected: [], other: "", otherOpen: false }]),
  );
}

function itemDraft(draft: QuestionDraft, item: QuestionItem): QuestionItemDraft {
  return draft[item.id] ?? { selected: [], other: "", otherOpen: false };
}

// Choosing a declared option in a single-choice item replaces any typed answer.
export function toggleQuestionOption(
  draft: QuestionDraft,
  item: QuestionItem,
  label: string,
): QuestionDraft {
  const current = itemDraft(draft, item);
  if (!item.multi_select) {
    return { ...draft, [item.id]: { selected: [label], other: "", otherOpen: false } };
  }
  const selected = current.selected.includes(label)
    ? current.selected.filter((value) => value !== label)
    : [...current.selected, label];
  return { ...draft, [item.id]: { ...current, selected } };
}

export function openQuestionOther(draft: QuestionDraft, item: QuestionItem): QuestionDraft {
  const current = itemDraft(draft, item);
  const selected = item.multi_select ? current.selected : [];
  return { ...draft, [item.id]: { ...current, selected, otherOpen: true } };
}

export function setQuestionOther(
  draft: QuestionDraft,
  item: QuestionItem,
  text: string,
): QuestionDraft {
  const current = itemDraft(draft, item);
  const selected = item.multi_select ? current.selected : [];
  return { ...draft, [item.id]: { selected, other: text, otherOpen: true } };
}

export function questionItemAnswers(
  item: QuestionItem,
  entry: QuestionItemDraft | undefined,
): string[] {
  if (!entry) return [];
  const other = entry.other.trim();
  const acceptsText = (item.options?.length ?? 0) === 0 || item.allow_other;
  return [...entry.selected, ...(acceptsText && other ? [other] : [])];
}

export function questionDraftAnswers(
  question: Pick<MessageQuestion, "items">,
  draft: QuestionDraft,
): Record<string, string[]> {
  return Object.fromEntries(
    question.items.map((item) => [item.id, questionItemAnswers(item, draft[item.id])]),
  );
}

export function questionDraftComplete(
  question: Pick<MessageQuestion, "items">,
  draft: QuestionDraft,
): boolean {
  return question.items.every((item) => questionItemAnswers(item, draft[item.id]).length > 0);
}

// One tap answers a single declared choice; everything else is a form.
export function isQuickQuestion(question: Pick<MessageQuestion, "items">): boolean {
  const [item] = question.items;
  return (
    question.items.length === 1 &&
    Boolean(item?.options?.length) &&
    !item?.multi_select &&
    !item?.allow_other
  );
}

// Digits pick options in order, then open the free-text answer.
export function questionShortcutKeys(
  item: Pick<QuestionItem, "options" | "allow_other">,
  quick: boolean,
): string {
  const count = (item.options?.length ?? 0) + (item.allow_other && !quick ? 1 : 0);
  return Array.from({ length: Math.min(count, 9) }, (_, index) => String(index + 1)).join(" ");
}

export function canAnswerQuestion(
  question: Pick<MessageQuestion, "status" | "responder_user_ids">,
  userID: string | undefined,
): boolean {
  if (!userID || question.status !== "open") return false;
  const responders = question.responder_user_ids ?? [];
  return responders.length === 0 || responders.includes(userID);
}

export type QuestionCountdown = { label: string; urgent: boolean; expired: boolean };

export function questionCountdown(expiresAt: string, now: number): QuestionCountdown {
  const remaining = Date.parse(expiresAt) - now;
  if (!Number.isFinite(remaining) || remaining <= 0)
    return { label: "0:00", urgent: true, expired: true };
  const seconds = Math.ceil(remaining / 1000);
  if (seconds >= 2 * 86400)
    return { label: `${Math.floor(seconds / 86400)} d`, urgent: false, expired: false };
  if (seconds >= 3600)
    return { label: `${Math.floor(seconds / 3600)} h`, urgent: false, expired: false };
  const minutes = Math.floor(seconds / 60);
  const label = `${minutes}:${String(seconds % 60).padStart(2, "0")}`;
  return { label, urgent: seconds <= 120, expired: false };
}

export type QuestionTone = "open" | "waiting" | "answered" | "muted" | "failed";

export function questionHeadline(
  question: Pick<MessageQuestion, "status" | "response" | "expires_at">,
  askerName: string,
  now: number,
): { label: string; tone: QuestionTone } {
  const asker = askerName || "the agent";
  switch (question.status) {
    case "open":
      return { label: "Needs your answer", tone: "open" };
    case "submitted":
      return Date.parse(question.expires_at) <= now
        ? { label: `Sent · not confirmed by ${asker}`, tone: "waiting" }
        : { label: `Sent · waiting for ${asker}`, tone: "waiting" };
    case "answered":
      return {
        label: question.response?.source === "external" ? "Answered elsewhere" : "Answered",
        tone: "answered",
      };
    case "cancelled":
      return { label: question.response?.skipped ? "Skipped" : "Cancelled", tone: "muted" };
    case "expired":
      return { label: "Expired", tone: "muted" };
    case "failed":
      return { label: "Not delivered", tone: "failed" };
  }
}

export type QuestionSummaryLine = {
  id: string;
  header: string;
  values: { text: string; typed: boolean }[];
};

export function questionSummary(
  question: Pick<MessageQuestion, "items" | "response">,
): QuestionSummaryLine[] {
  const answers = question.response?.answers;
  if (!answers) return [];
  return question.items
    .filter((item) => (answers[item.id]?.length ?? 0) > 0)
    .map((item) => ({
      id: item.id,
      header: item.header,
      values: answers[item.id].map((text) => ({
        text,
        // Only answers typed beside declared options are marked; free-text items have no options.
        typed:
          Boolean(item.options?.length) && !item.options?.some((option) => option.label === text),
      })),
    }));
}

const QUESTION_DRAFT_LIMIT = 50;
const questionDrafts = new Map<string, QuestionDraft>();

// Drafts outlive virtualized rows and thread panel switches for the same question version.
export function recallQuestionDraft(key: string): QuestionDraft | undefined {
  return questionDrafts.get(key);
}

export function rememberQuestionDraft(key: string, draft: QuestionDraft) {
  questionDrafts.delete(key);
  questionDrafts.set(key, draft);
  while (questionDrafts.size > QUESTION_DRAFT_LIMIT) {
    const oldest = questionDrafts.keys().next().value;
    if (oldest === undefined) break;
    questionDrafts.delete(oldest);
  }
}

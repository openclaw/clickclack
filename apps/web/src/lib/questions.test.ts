import test from "node:test";
import assert from "node:assert/strict";
import {
  canAnswerQuestion,
  emptyQuestionDraft,
  isQuickQuestion,
  openQuestionOther,
  questionCountdown,
  questionDraftAnswers,
  questionDraftComplete,
  questionHeadline,
  questionShortcutKeys,
  questionSummary,
  setQuestionOther,
  toggleQuestionOption,
} from "./questions.ts";
import type { MessageQuestion, QuestionItem } from "./types";

const date: QuestionItem = {
  id: "ship_date",
  header: "Fecha",
  prompt: "¿Qué día?",
  options: [{ label: "Lun 15 sep" }, { label: "Mar 16 sep" }],
  allow_other: true,
};
const boxes: QuestionItem = { id: "boxes", header: "Cajas", prompt: "¿Cuántas?" };
const extras: QuestionItem = {
  id: "extras",
  header: "Extras",
  prompt: "¿Algo más?",
  options: [{ label: "Capuchón" }, { label: "UPC" }],
  multi_select: true,
};

function question(overrides: Partial<MessageQuestion> = {}): MessageQuestion {
  return {
    status: "open",
    expires_at: "2026-09-12T15:15:00Z",
    allow_skip: true,
    items: [date, boxes, extras],
    version: 1,
    ...overrides,
  };
}

test("question drafts track choices, typed answers, and completeness", () => {
  const q = question();
  let draft = emptyQuestionDraft(q);
  assert.equal(questionDraftComplete(q, draft), false);
  draft = toggleQuestionOption(draft, date, "Lun 15 sep");
  draft = setQuestionOther(draft, boxes, " 120 ");
  draft = toggleQuestionOption(draft, extras, "UPC");
  draft = toggleQuestionOption(draft, extras, "Capuchón");
  draft = toggleQuestionOption(draft, extras, "UPC");
  assert.deepEqual(questionDraftAnswers(q, draft), {
    ship_date: ["Lun 15 sep"],
    boxes: ["120"],
    extras: ["Capuchón"],
  });
  assert.equal(questionDraftComplete(q, draft), true);

  draft = openQuestionOther(draft, date);
  assert.deepEqual(questionDraftAnswers(q, draft).ship_date, []);
  draft = setQuestionOther(draft, date, "Jue 18 sep");
  assert.deepEqual(questionDraftAnswers(q, draft).ship_date, ["Jue 18 sep"]);
  draft = toggleQuestionOption(draft, date, "Mar 16 sep");
  assert.deepEqual(questionDraftAnswers(q, draft).ship_date, ["Mar 16 sep"]);
});

test("typed text only counts where the item accepts it", () => {
  const q = question({ items: [extras] });
  const draft = setQuestionOther(emptyQuestionDraft(q), extras, "Comida");
  assert.deepEqual(questionDraftAnswers(q, draft), { extras: [] });
});

test("quick questions are one declared choice without free text", () => {
  assert.equal(isQuickQuestion(question({ items: [{ ...date, allow_other: false }] })), true);
  assert.equal(isQuickQuestion(question({ items: [date] })), false);
  assert.equal(isQuickQuestion(question({ items: [extras] })), false);
  assert.equal(isQuickQuestion(question({ items: [boxes] })), false);
});

test("digit shortcuts cover the options and the free-text answer up to nine", () => {
  assert.equal(questionShortcutKeys(date, false), "1 2 3");
  assert.equal(questionShortcutKeys({ ...date, allow_other: false }, true), "1 2");
  assert.equal(questionShortcutKeys(boxes, false), "");
  const options = Array.from({ length: 10 }, (_, index) => ({ label: `Option ${index}` }));
  assert.equal(questionShortcutKeys({ options, allow_other: true }, false), "1 2 3 4 5 6 7 8 9");
});

test("only open questions for listed responders can be answered", () => {
  assert.equal(canAnswerQuestion(question(), "usr_1"), true);
  assert.equal(canAnswerQuestion(question({ responder_user_ids: ["usr_2"] }), "usr_1"), false);
  assert.equal(canAnswerQuestion(question({ status: "submitted" }), "usr_1"), false);
  assert.equal(canAnswerQuestion(question(), undefined), false);
});

test("questionCountdown formats minutes, hours, and days", () => {
  const now = Date.parse("2026-09-12T15:00:00Z");
  assert.deepEqual(questionCountdown("2026-09-12T15:14:32Z", now), {
    label: "14:32",
    urgent: false,
    expired: false,
  });
  assert.deepEqual(questionCountdown("2026-09-12T15:01:48Z", now), {
    label: "1:48",
    urgent: true,
    expired: false,
  });
  assert.equal(questionCountdown("2026-09-12T18:30:00Z", now).label, "3 h");
  assert.equal(questionCountdown("2026-09-15T15:00:00Z", now).label, "3 d");
  assert.equal(questionCountdown("2026-09-12T14:59:00Z", now).expired, true);
});

test("questionHeadline names each state", () => {
  const now = Date.parse("2026-09-12T15:00:00Z");
  assert.deepEqual(questionHeadline(question(), "Atlas", now), {
    label: "Needs your answer",
    tone: "open",
  });
  assert.equal(
    questionHeadline(question({ status: "submitted" }), "Atlas", now).label,
    "Sent · waiting for Atlas",
  );
  assert.equal(
    questionHeadline(
      question({ status: "submitted", expires_at: "2026-09-12T14:00:00Z" }),
      "Atlas",
      now,
    ).label,
    "Sent · not confirmed by Atlas",
  );
  assert.equal(
    questionHeadline(
      question({ status: "answered", response: { source: "external" } }),
      "Atlas",
      now,
    ).label,
    "Answered elsewhere",
  );
  assert.equal(
    questionHeadline(
      question({ status: "cancelled", response: { source: "clickclack", skipped: true } }),
      "Atlas",
      now,
    ).label,
    "Skipped",
  );
  assert.equal(questionHeadline(question({ status: "failed" }), "Atlas", now).tone, "failed");
});

test("questionSummary marks answers typed beside declared options", () => {
  const summary = questionSummary(
    question({
      status: "answered",
      response: {
        source: "clickclack",
        answers: { ship_date: ["Jue 18 sep"], boxes: ["120"], extras: ["UPC"] },
      },
    }),
  );
  assert.deepEqual(summary, [
    { id: "ship_date", header: "Fecha", values: [{ text: "Jue 18 sep", typed: true }] },
    { id: "boxes", header: "Cajas", values: [{ text: "120", typed: false }] },
    { id: "extras", header: "Extras", values: [{ text: "UPC", typed: false }] },
  ]);
});

// PROJECT LOGOS — Cognition Service: Adaptive Response Generator (§5.2, §6.1)
//
// Generates companion replies adapted to user input style (cognitive mirroring),
// intent-aware, persona-driven, with optional memory citations.
//
// Style detection (§5.2 Cognitive Mirroring Architecture):
//   terse    → concise, high-density output
//   analytical → structured, logically broken-down frameworks
//   brainstorming → generative, exploratory output
//   neutral  → balanced, conversational
//
// Heuristics used: length, word count, sentence count, question marks,
// structural indicators (bullets, colons, numbers), brainstorming keywords.

import type {
  Intent,
  Persona,
  RespondRequest,
  RespondResult,
} from "../types.js";
import { INTENTS, PERSONAS } from "../types.js";
import { getPersonaDef } from "./personas.js";
import type { LlmClient } from "./llm.js";
import type { JsonFileStore } from "./store.js";

// ─── Style classification ────────────────────────────────────────────────────

export type StyleTag = "terse" | "analytical" | "brainstorming" | "neutral";

export interface StyleResult {
  tag: StyleTag;
  /** Natural-language instruction injected into persona prompt */
  instruction: string;
  /** Heuristic signals used for classification */
  signals: {
    charCount: number;
    wordCount: number;
    sentenceCount: number;
    questionMarks: number;
    hasBulletPoints: boolean;
    hasStructuredPrefix: boolean;
    hasBrainstormKeywords: boolean;
    startsCommandLike: boolean;
  };
}

const BRAINSTORM_KEYWORDS = [
  "brainstorm",
  "what if",
  "imagine",
  "could we",
  "how might",
  "possibility",
  "ideate",
  "blue sky",
  "no constraints",
  "spitball",
  "free association",
];

function countWords(text: string): number {
  return text.trim().split(/\s+/).filter(Boolean).length;
}

function countSentences(text: string): number {
  return text.split(/[.!?]+/).filter(Boolean).length;
}

/** Lightweight heuristic style detection — no LLM call needed. */
export function detectStyle(content: string): StyleResult {
  const trimmed = content.trim();
  const charCount = trimmed.length;
  const wordCount = countWords(trimmed);
  const sentenceCount = Math.max(1, countSentences(trimmed));
  const questionMarks = (trimmed.match(/\?/g) ?? []).length;
  const hasBulletPoints = /^[\s]*[-\*•]/.test(trimmed) || /[\n\r][\s]*[-\*•]/.test(trimmed);
  const hasStructuredPrefix = /^[\s]*(\d+[.)]\s|\w+:\s*$)/.test(trimmed);
  const lower = trimmed.toLowerCase();
  const startsCommandLike = /^[\/!]/.test(trimmed);
  const hasBrainstormKeywords = BRAINSTORM_KEYWORDS.some((kw) =>
    lower.includes(kw),
  );

  const signals = {
    charCount,
    wordCount,
    sentenceCount,
    questionMarks,
    hasBulletPoints,
    hasStructuredPrefix,
    hasBrainstormKeywords,
    startsCommandLike,
  };

  // Classification rules (priority order: command-like → brainstorming → terse → analytical → neutral)
  if (startsCommandLike) {
    return {
      tag: "terse",
      instruction:
        "MIRROR STYLE: The user's input is terse and command-like. " +
        "Respond with maximum density — no preamble, no explanation unless essential, " +
        "concise bullet-point or imperative output. Every word must earn its place. " +
        "Skip all pleasantries.",
      signals,
    };
  }

  if (hasBrainstormKeywords) {
    return {
      tag: "brainstorming",
      instruction:
        "MIRROR STYLE: The user is in brainstorming/exploratory mode. " +
        "Generate multiple divergent possibilities. Use 'what if' framing, " +
        "metaphor, lateral connections. Prioritize breadth over precision. " +
        "Flag speculative claims as such. End with 1-2 provocative questions.",
      signals,
    };
  }

  // Terse: very short inputs, command-like density
  if (charCount <= 80 && wordCount <= 12) {
    return {
      tag: "terse",
      instruction:
        "MIRROR STYLE: The user's input is terse. " +
        "Respond with maximum density — no preamble, no fluff. " +
        "Use bullet-point or imperative output. Match the user's brevity.",
      signals,
    };
  }

  // Analytical: longer, multi-sentence, questions, or structured
  if (
    charCount > 150 &&
    (questionMarks > 0 || sentenceCount >= 2 || hasBulletPoints || hasStructuredPrefix)
  ) {
    return {
      tag: "analytical",
      instruction:
        "MIRROR STYLE: The user's input is analytical. " +
        "Structure your response with explicit logical hierarchy: " +
        "thesis → evidence → analysis → conclusion. Use numbered points, " +
        "label assumptions, separate observation from interpretation. " +
        "Weigh claims by confidence.",
      signals,
    };
  }

  // Brainstorming: implicit — open-ended questions, "how would", "what should"
  if (
    questionMarks > 0 &&
    wordCount > 10 &&
    /\b(how|what|why|explore|ideas?|options?|ways? to)\b/i.test(trimmed) &&
    !hasStructuredPrefix
  ) {
    return {
      tag: "brainstorming",
      instruction:
        "MIRROR STYLE: The user is in open-ended exploration mode. " +
        "Generate multiple possibilities or angles. Avoid premature convergence. " +
        "Flag speculative claims. End with a provocative follow-up question.",
      signals,
    };
  }

  // Neutral default
  return {
    tag: "neutral",
    instruction:
      "MIRROR STYLE: Match the user's tone and density. " +
      "Be direct and helpful. No forced structure — adapt naturally.",
    signals,
  };
}

// ─── Intent resolution ───────────────────────────────────────────────────────

async function resolveIntent(
  req: RespondRequest,
  llm: LlmClient,
  executionStack: string[],
): Promise<{
  intent: Intent;
  confidence: number;
  model: string;
  clarificationQuestion?: string;
}> {
  // If caller supplied intent, validate and use it
  if (req.intent && (INTENTS as readonly string[]).includes(req.intent)) {
    return { intent: req.intent, confidence: 1.0, model: "caller" };
  }

  // Run analyze via LLM
  try {
    const analysis = await llm.analyze({ content: req.content });
    executionStack.push("intent_parser()");
    return {
      intent: analysis.intent,
      confidence: analysis.confidence,
      model: analysis.model,
      clarificationQuestion: analysis.clarification_question,
    };
  } catch (err) {
    console.warn("[cognition:respond] intent classification failed:", err);
    // Fallback: simple heuristic
    const lower = req.content.toLowerCase();
    let intent: Intent = "reflect";
    if (lower.includes("?")) intent = "ask";
    else if (/^[\/!]/.test(req.content.trim())) intent = "command";
    else if (
      lower.includes("draft") ||
      lower.includes("write")
    )
      intent = "draft";
    else if (
      lower.includes("explore") ||
      lower.includes("idea") ||
      lower.includes("brainstorm")
    )
      intent = "explore";
    executionStack.push("intent_parser()");
    return { intent, confidence: 0.5, model: "heuristic-fallback" };
  }
}

// ─── Style instruction for persona prompt ─────────────────────────────────────

function buildStyleInstruction(style: StyleResult): string {
  return `\n\nCOGNITIVE MIRROR (style: ${style.tag}) — detected signals: ` +
    `${style.signals.charCount}c, ${style.signals.wordCount}w, ` +
    `${style.signals.sentenceCount}s, ?×${style.signals.questionMarks}, ` +
    `bullets=${style.signals.hasBulletPoints}, structured=${style.signals.hasStructuredPrefix}, ` +
    `brainstorm=${style.signals.hasBrainstormKeywords}, cmd=${style.signals.startsCommandLike}\n` +
    style.instruction;
}

function buildIntentInstruction(intent: Intent): string {
  switch (intent) {
    case "ask":
      return "INTENT MODE: Answer directly first. Add only the most decision-relevant nuance, constraint, or next move.";
    case "command":
      return "INTENT MODE: Convert the request into execution. Lead with the action or deliverable, then sequence the essentials.";
    case "draft":
      return "INTENT MODE: Produce usable language, not commentary about language. Deliver polished draft output by default.";
    case "clarify":
      return "INTENT MODE: Reduce ambiguity. State the likely interpretation, isolate uncertainty, and ask the highest-value clarifying question only if needed.";
    case "explore":
      return "INTENT MODE: Generate multiple viable directions, compare trade-offs, and converge toward the strongest option.";
    case "reflect":
    default:
      return "INTENT MODE: Help the user think clearly. Synthesize signal, expose tensions, and move toward a crisp insight.";
  }
}

function buildContextBlock(title: string, items: string[]): string {
  if (items.length === 0) return "";
  return `\n\n${title}:\n${items.map((item, index) => `${index + 1}. ${item}`).join("\n")}`;
}

function deriveFollowups(intent: Intent, content: string): string[] {
  const compact = content.replace(/\s+/g, " ").trim();
  if (!compact) return ["Summarize the core point in one sentence"];

  const segments = compact
    .split(/(?<=[.!?])\s+/)
    .map((part) => part.trim())
    .filter((part) => part.length > 12)
    .slice(0, 3);

  const followups = new Set<string>();
  for (const segment of segments) {
    const cleaned = segment.replace(/^[-*]\s*/, "").replace(/[.?!]+$/, "");
    if (intent === "draft") {
      followups.add(`Rewrite this with a sharper tone: ${cleaned}`);
    } else if (intent === "explore") {
      followups.add(`Push this direction further: ${cleaned}`);
    } else if (intent === "command") {
      followups.add(`Turn this into a checklist: ${cleaned}`);
    } else {
      followups.add(`Pressure-test this point: ${cleaned}`);
    }
  }

  return Array.from(followups).slice(0, 3);
}

// ─── Respond handler ─────────────────────────────────────────────────────────

export async function handleRespond(
  req: RespondRequest,
  llm: LlmClient,
  store: JsonFileStore | null,
): Promise<RespondResult> {
  const t0 = Date.now();
  const executionStack: string[] = [];

  // 1. Resolve intent (classify if not provided)
  const { intent, confidence, clarificationQuestion } = await resolveIntent(
    req,
    llm,
    executionStack,
  );

  // 2. Style detection (cognitive mirroring)
  const style = detectStyle(req.content);
  executionStack.push("style_mirror()");

  // 3. Persona resolution
  const persona: Persona = req.persona &&
    (PERSONAS as readonly string[]).includes(req.persona)
    ? req.persona
    : "operator";
  const personaDef = getPersonaDef(persona);
  executionStack.push("persona_engine()");

  // 4. Memory retrieval (weighted for ask/command, but available when hinted)
  let memoryCitations: string[] | undefined;
  let memoryPreviews: RespondResult["meta"]["memory_previews"];
  let memoryContextLines: string[] = [];
  if (
    store &&
    (intent === "ask" || intent === "command" || (req.memory_hint_ids?.length ?? 0) > 0)
  ) {
    try {
      const hintIds = new Set(req.memory_hint_ids ?? []);
      const nodes = await store.queryAnchors(req.content, 5);
      const selected = nodes
        .filter((node) => hintIds.has(node.id) || (node.score ?? 0) > 0.32)
        .sort((a, b) => {
          const aHint = hintIds.has(a.id) ? 1 : 0;
          const bHint = hintIds.has(b.id) ? 1 : 0;
          if (aHint !== bHint) return bHint - aHint;
          return (b.score ?? 0) - (a.score ?? 0);
        })
        .slice(0, 3);
      if (selected.length > 0) {
        memoryCitations = selected.map((node) => node.id);
        memoryPreviews = selected.map((node) => ({
          id: node.id,
          content: node.content.slice(0, 220),
          ...(typeof node.score === "number" ? { score: node.score } : {}),
          ...(node.source_message_id ? { source_message_id: node.source_message_id } : {}),
          ...(node.tags?.length ? { tags: node.tags } : {}),
        }));
        memoryContextLines = selected.map((node) => {
          const score = typeof node.score === "number" ? ` score=${node.score.toFixed(3)}` : "";
          const tags = node.tags?.length ? ` tags=${node.tags.join(",")}` : "";
          return `#NODE-${node.id.slice(0, 8)}${score}${tags} :: ${node.content.slice(0, 180)}`;
        });
      }
      executionStack.push("memory_retrieval()");
    } catch (err) {
      console.warn("[cognition:respond] memory retrieval failed:", err);
    }
  }

  // 5. Build system prompt: persona + mirror instruction + intent + context
  const styleInstruction = buildStyleInstruction(style);
  const intentInstruction = buildIntentInstruction(intent);
  const memoryBlock = buildContextBlock("RELEVANT MEMORY", memoryContextLines);
  const conversationBlock = buildContextBlock(
    "RECENT CONVERSATION",
    (req.context_messages ?? [])
      .slice(-6)
      .map((message) => `${message.role.toUpperCase()}: ${message.content.slice(0, 220)}`),
  );
  const systemPrompt = personaDef.systemPrompt + "\n\n---\n\n" + styleInstruction +
    "\n\n" + intentInstruction + memoryBlock + conversationBlock +
    "\n\nPROTOCOL: Respond naturally as the companion. Lead with the answer or action. Use memory context only when it genuinely sharpens the reply. Do NOT fabricate prior context. Do NOT wrap your answer in JSON. Do NOT mention the style mirroring or persona unless directly relevant to the question.";

  // 6. LLM chat — generate the actual reply
  let content: string;
  let model: string;

  try {
    executionStack.push("llm_chat()");
    const result = await llm.respond({
      systemPrompt,
      userContent: req.content,
      contextMessages: req.context_messages,
    });
    content = result.content;
    model = result.model;
  } catch (err) {
    console.error("[cognition:respond] LLM chat failed:", err);
    // Graceful stub fallback
    content =
      "[Fallback] Unable to generate a response at this time. The cognition service encountered an error.";
    model = "fallback";
  }

  const latencyMs = Date.now() - t0;

  return {
    content,
    ...(clarificationQuestion ? { clarification_question: clarificationQuestion } : {}),
    suggested_followups: deriveFollowups(intent, content),
    meta: {
      intent,
      persona,
      confidence: Math.round(confidence * 1000) / 1000,
      model,
      latency_ms: latencyMs,
      memory_citations: memoryCitations,
      memory_previews: memoryPreviews,
      execution_stack: executionStack,
    },
  };
}

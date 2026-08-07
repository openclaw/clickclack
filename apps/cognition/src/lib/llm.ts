// PROJECT LOGOS — Cognition Service: LLM client interface + stub
//
// LLM client is the sole integration point for AI intelligence.
// The interface is defined here; implementations are swappable via LLM_PROVIDER env.
// StubLlmClient returns deterministic placeholder results — used for dev/CI.
//
// T3 handoff task: wire real providers (DeepSeek, OpenAI) behind this interface.

import type {
  AnalyzeRequest,
  AnalyzeResult,
  Intent,
  Persona,
  TransformOp,
  TransformRequest,
  TransformResult,
} from "../types.js";

import { INTENTS, PERSONAS } from "../types.js";

// ─── Interface ───────────────────────────────────────────────────────────────

export interface LlmClient {
  /** Analyze content for intent, persona, and confidence. */
  analyze(req: AnalyzeRequest): Promise<AnalyzeResult>;

  /** Apply a transform op to content, optionally filtered through a persona. */
  transform(req: TransformRequest): Promise<TransformResult>;

  /** Generate an embedding vector for the given text. */
  embed(text: string): Promise<number[]>;
}

// ─── Provider resolution ─────────────────────────────────────────────────────

export function createLlmClient(): LlmClient {
  const provider = process.env.LLM_PROVIDER ?? "stub";

  switch (provider) {
    case "stub":
      console.log("[cognition] LLM provider: stub (deterministic fake)");
      return new StubLlmClient();
    // TODO (T3 handoff): real providers
    // case "deepseek":
    //   return new DeepSeekLlmClient(process.env.DEEPSEEK_API_KEY);
    // case "openai":
    //   return new OpenAILlmClient(process.env.OPENAI_API_KEY);
    default:
      console.warn(
        `[cognition] Unknown LLM_PROVIDER="${provider}", falling back to stub`,
      );
      return new StubLlmClient();
  }
}

// ─── Stub implementation ─────────────────────────────────────────────────────

export class StubLlmClient implements LlmClient {
  private callCount = 0;

  async analyze(req: AnalyzeRequest): Promise<AnalyzeResult> {
    this.callCount++;
    console.log("[cognition:stub] analyze()", {
      contentLen: req.content.length,
      contextLen: req.context?.length ?? 0,
      call: this.callCount,
    });

    // Deterministic stub: pick intent/persona based on content signals
    const lower = req.content.toLowerCase();

    let intent: Intent = "reflect";
    if (lower.includes("?")) intent = "ask";
    else if (lower.startsWith("/") || lower.startsWith("!")) intent = "command";
    else if (lower.includes("draft") || lower.includes("write")) intent = "draft";
    else if (lower.includes("clarify") || lower.includes("mean"))
      intent = "clarify";
    else if (
      lower.includes("explore") ||
      lower.includes("idea") ||
      lower.includes("brainstorm")
    )
      intent = "explore";

    const persona: Persona = PERSONAS[this.callCount % PERSONAS.length];

    return {
      intent,
      persona,
      confidence: 0.78,
      context_tags: ["stub", "placeholder"],
      model: "stub",
    };
  }

  async transform(req: TransformRequest): Promise<TransformResult> {
    this.callCount++;
    console.log("[cognition:stub] transform()", {
      contentLen: req.content.length,
      op: req.op,
      persona: req.persona ?? "default",
      call: this.callCount,
    });

    // Deterministic stub: prepend a note, return content pseudo-transformed
    const note = `[stub transform: ${req.op}]`;
    const transformed = `${note}\n\n${req.content}`;

    return {
      original_content: req.content,
      transformed_content: transformed,
      op: req.op,
      persona: req.persona ?? "operator",
      confidence: 0.72,
      model: "stub",
    };
  }

  async embed(_text: string): Promise<number[]> {
    this.callCount++;
    console.log("[cognition:stub] embed()", {
      textLen: _text.length,
      call: this.callCount,
    });

    // Deterministic stub: fixed-length zero vector
    // Real embedding dims: DeepSeek=4096, OpenAI text-embedding-3-small=1536
    return new Array(1536).fill(0);
  }
}

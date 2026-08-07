// PROJECT LOGOS — Cognition Service: LLM client interface + real providers
//
// LLM client is the sole integration point for AI intelligence.
// The interface is defined here; implementations are swappable via LLM_PROVIDER env.
// StubLlmClient returns deterministic placeholder results — used for dev/CI.
//
// Real providers:
//   DeepSeekLlmClient — OpenAI-compatible chat completions for analyze/transform
//
// Embeddings are SEPARATE (see embed.ts). DeepSeek has no embeddings API.

import type {
  AnalyzeRequest,
  AnalyzeResult,
  Intent,
  Persona,
  Telemetry,
  TransformOp,
  TransformRequest,
  TransformResult,
} from "../types.js";

import { INTENTS, PERSONAS } from "../types.js";
import { withPersona, getPersonaDef } from "./personas.js";

// ─── Interface ───────────────────────────────────────────────────────────────

/** Parameters for the respond (chat) method */
export interface RespondParams {
  systemPrompt: string;
  userContent: string;
  contextMessages?: { role: "user" | "assistant"; content: string }[];
}

/** Result from the respond (chat) method */
export interface RespondLlmResult {
  content: string;
  model: string;
  totalTokens?: number;
}

export interface LlmClient {
  /** Analyze content for intent, persona, and confidence. */
  analyze(req: AnalyzeRequest): Promise<AnalyzeResult>;

  /** Apply a transform op to content, optionally filtered through a persona. */
  transform(req: TransformRequest): Promise<TransformResult>;

  /** Generate a companion reply with a fully-built system prompt (persona + style). */
  respond(params: RespondParams): Promise<RespondLlmResult>;
}

// ─── Provider resolution ─────────────────────────────────────────────────────

export function createLlmClient(): LlmClient {
  const provider = process.env.LLM_PROVIDER ?? "stub";

  switch (provider) {
    case "stub":
      console.log("[cognition] LLM provider: stub (deterministic fake)");
      return new StubLlmClient();
    case "deepseek": {
      const apiKey = process.env.DEEPSEEK_API_KEY;
      if (!apiKey) {
        console.warn(
          "[cognition] LLM_PROVIDER=deepseek but DEEPSEEK_API_KEY is not set, falling back to stub",
        );
        return new StubLlmClient();
      }
      const model = process.env.LLM_MODEL ?? "deepseek-chat";
      console.log(`[cognition] LLM provider: deepseek (model: ${model})`);
      return new DeepSeekLlmClient(apiKey, model);
    }
    default:
      console.warn(
        `[cognition] Unknown LLM_PROVIDER="${provider}", falling back to stub`,
      );
      return new StubLlmClient();
  }
}

// ─── Helper: fetch with timeout ──────────────────────────────────────────────

async function fetchWithTimeout(
  url: string,
  init: RequestInit,
  timeoutMs = 60_000,
): Promise<Response> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const res = await fetch(url, { ...init, signal: controller.signal });
    return res;
  } finally {
    clearTimeout(timer);
  }
}

// ─── Chat result (includes token usage) ─────────────────────────────────────

interface ChatResult {
  content: string;
  total_tokens?: number;
}

// ─── Stub implementation ─────────────────────────────────────────────────────

export class StubLlmClient implements LlmClient {
  private callCount = 0;

  async analyze(req: AnalyzeRequest): Promise<AnalyzeResult> {
    this.callCount++;
    const t0 = Date.now();
    console.log("[cognition:stub] analyze()", {
      contentLen: req.content.length,
      contextLen: req.context?.length ?? 0,
      call: this.callCount,
    });

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
    const execution_stack = ["intent_parser()", "persona_engine()", "classify_local()"];

    const telemetry: Telemetry = {
      latency_ms: Date.now() - t0,
      total_tokens: Math.ceil(req.content.length / 4),
      model: "stub",
      intent_vector_score: 0.78,
      execution_stack,
    };

    return {
      intent,
      persona,
      confidence: 0.78,
      context_tags: ["stub", "placeholder"],
      model: "stub",
      telemetry,
    };
  }

  async transform(req: TransformRequest): Promise<TransformResult> {
    this.callCount++;
    const t0 = Date.now();
    console.log("[cognition:stub] transform()", {
      contentLen: req.content.length,
      op: req.op,
      persona: req.persona ?? "default",
      call: this.callCount,
    });

    const note = `[stub transform: ${req.op}]`;
    const transformed = `${note}\n\n${req.content}`;
    const persona = req.persona ?? "operator";
    const execution_stack = [
      "transform_engine()",
      `persona_filter(${persona})`,
    ];

    const telemetry: Telemetry = {
      latency_ms: Date.now() - t0,
      total_tokens: Math.ceil((req.content.length + transformed.length) / 4),
      model: "stub",
      execution_stack,
    };

    return {
      original_content: req.content,
      transformed_content: transformed,
      op: req.op,
      persona,
      confidence: 0.72,
      model: "stub",
      meta: {
        op: req.op,
        model: "stub",
        confidence: 0.72,
      },
      telemetry,
    };
  }

  async respond(params: RespondParams): Promise<RespondLlmResult> {
    this.callCount++;
    console.log("[cognition:stub] respond()", {
      userLen: params.userContent.length,
      contextLen: params.contextMessages?.length ?? 0,
      call: this.callCount,
    });

    // Simple stub that echoes the input with a persona-aware prefix
    const prefix = "[STUB RESPONSE — cognition offline]";
    return {
      content: `${prefix}\n\nInput: ${params.userContent.slice(0, 200)}`,
      model: "stub",
    };
  }
}

// ─── DeepSeek LLM Client ─────────────────────────────────────────────────────

const DEEPSEEK_BASE = "https://api.deepseek.com";

export class DeepSeekLlmClient implements LlmClient {
  private apiKey: string;
  private model: string;
  private callCount = 0;

  constructor(apiKey: string, model = "deepseek-chat") {
    this.apiKey = apiKey;
    this.model = model;
  }

  // ── Chat completion helper (returns content + token usage) ──────────────

  private async chat(
    systemPrompt: string,
    userPrompt: string,
    opts?: { temperature?: number; maxTokens?: number },
  ): Promise<ChatResult> {
    const res = await fetchWithTimeout(`${DEEPSEEK_BASE}/v1/chat/completions`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${this.apiKey}`,
      },
      body: JSON.stringify({
        model: this.model,
        messages: [
          { role: "system", content: systemPrompt },
          { role: "user", content: userPrompt },
        ],
        temperature: opts?.temperature ?? 0.3,
        max_tokens: opts?.maxTokens ?? 2048,
      }),
    });

    if (!res.ok) {
      const text = await res.text();
      throw new Error(`DeepSeek API error ${res.status}: ${text}`);
    }

    const data = (await res.json()) as {
      choices: { message: { content: string } }[];
      usage?: { total_tokens: number };
    };
    return {
      content: data.choices[0]?.message?.content ?? "",
      total_tokens: data.usage?.total_tokens,
    };
  }

  // ── analyze ─────────────────────────────────────────────────────────────

  async analyze(req: AnalyzeRequest): Promise<AnalyzeResult> {
    this.callCount++;
    const t0 = Date.now();
    console.log("[cognition:deepseek] analyze()", {
      contentLen: req.content.length,
      call: this.callCount,
    });

    const execution_stack: string[] = ["intent_parser()", "persona_engine()"];

    try {
      const systemPrompt = `You are an intent classification engine for a cognitive messaging system.
Your job: classify the user's message into exactly one intent and suggest a persona.

Intents (choose exactly one):
- ask: information-seeking question
- command: explicit actionable directive or instruction
- reflect: open-ended thinking, processing, musing
- draft: request for content creation or writing
- clarify: refinement, correction, or asking for clarification
- explore: lateral ideation, brainstorming, open-ended exploration

Personas (choose exactly one):
- operator: terse tactical communication
- analyst: structured logical multi-layered output
- creative: generative lateral metaphorical
- socratic: probing, challenging, questioning
- archivist: memory-grounded, historical, pattern-aware

Context tags: extract 2-5 short keyword tags describing the topic domain.

AMBIGUITY: If the message is genuinely ambiguous (could reasonably be 2+ intents, 
or the user's intent is unclear), set clarification_question to a brief clarifying question.

Output ONLY valid JSON — no markdown, no preamble, no explanation:
{
  "intent": "<intent>",
  "persona": "<persona>",
  "confidence": <0.0-1.0>,
  "context_tags": ["tag1", "tag2"],
  "clarification_question": "<question or null>"
}`;

      execution_stack.push("llm_chat()");
      const chatResult = await this.chat(systemPrompt, req.content);
      const parsed = this.parseAnalyzeJson(chatResult.content, req.content);

      const telemetry: Telemetry = {
        latency_ms: Date.now() - t0,
        total_tokens: chatResult.total_tokens ?? Math.ceil(req.content.length / 4),
        model: this.model,
        intent_vector_score: parsed.confidence,
        execution_stack,
      };

      return {
        intent: parsed.intent,
        persona: parsed.persona,
        confidence: parsed.confidence,
        context_tags: parsed.context_tags,
        model: this.model,
        ...(parsed.clarification_question
          ? { clarification_question: parsed.clarification_question }
          : {}),
        telemetry,
      };
    } catch (err) {
      console.warn(
        "[cognition:deepseek] analyze failed, falling back to stub:",
        err,
      );
      return new StubLlmClient().analyze(req);
    }
  }

  private parseAnalyzeJson(
    raw: string,
    content: string,
  ): {
    intent: Intent;
    persona: Persona;
    confidence: number;
    context_tags: string[];
    clarification_question: string | null;
  } {
    try {
      const cleaned = raw
        .replace(/```json\s*/gi, "")
        .replace(/```\s*/g, "")
        .trim();
      const obj = JSON.parse(cleaned);

      const intent = INTENTS.includes(obj.intent) ? obj.intent : "reflect";
      const persona = PERSONAS.includes(obj.persona)
        ? obj.persona
        : PERSONAS[0];
      const confidence =
        typeof obj.confidence === "number"
          ? Math.max(0, Math.min(1, obj.confidence))
          : 0.5;
      const context_tags: string[] = Array.isArray(obj.context_tags)
        ? obj.context_tags.slice(0, 10).map(String)
        : [];
      const clarification_question: string | null =
        typeof obj.clarification_question === "string" &&
        obj.clarification_question !== "null"
          ? obj.clarification_question
          : null;

      return { intent, persona, confidence, context_tags, clarification_question };
    } catch {
      console.warn("[cognition:deepseek] JSON parse failed, using fallback", raw.slice(0, 200));
      const lower = content.toLowerCase();
      let intent: Intent = "reflect";
      if (lower.includes("?")) intent = "ask";
      else if (lower.startsWith("/") || lower.startsWith("!"))
        intent = "command";
      return {
        intent,
        persona: "analyst",
        confidence: 0.5,
        context_tags: ["fallback"],
        clarification_question: null,
      };
    }
  }

  // ── transform ───────────────────────────────────────────────────────────

  async transform(req: TransformRequest): Promise<TransformResult> {
    this.callCount++;
    const t0 = Date.now();
    console.log("[cognition:deepseek] transform()", {
      contentLen: req.content.length,
      op: req.op,
      persona: req.persona ?? "default",
      call: this.callCount,
    });

    const personaId = req.persona ?? "operator";
    const execution_stack: string[] = [
      "transform_engine()",
      `persona_filter(${personaId})`,
    ];

    try {
      const opPrompt = buildTransformPrompt(req.op, personaId);

      execution_stack.push("llm_chat()");
      const chatResult = await this.chat(
        opPrompt.system,
        `${opPrompt.user}\n\n---CONTENT---\n${req.content}`,
        { temperature: opPrompt.temperature ?? 0.5, maxTokens: opPrompt.maxTokens ?? 2048 },
      );

      const telemetry: Telemetry = {
        latency_ms: Date.now() - t0,
        total_tokens: chatResult.total_tokens ?? Math.ceil(
          (req.content.length + chatResult.content.length) / 4,
        ),
        model: this.model,
        execution_stack,
      };

      return {
        original_content: req.content,
        transformed_content: chatResult.content.trim(),
        op: req.op,
        persona: personaId,
        confidence: 0.85,
        model: this.model,
        meta: {
          op: req.op,
          model: this.model,
          confidence: 0.85,
        },
        telemetry,
      };
    } catch (err) {
      console.warn(
        "[cognition:deepseek] transform failed, falling back to stub:",
        err,
      );
      return new StubLlmClient().transform(req);
    }
  }

  // ── respond (adaptive response generator) ───────────────────────────────

  async respond(params: RespondParams): Promise<RespondLlmResult> {
    this.callCount++;
    console.log("[cognition:deepseek] respond()", {
      userLen: params.userContent.length,
      contextLen: params.contextMessages?.length ?? 0,
      call: this.callCount,
    });

    try {
      // Build messages array: system + optional context + user
      const messages: { role: string; content: string }[] = [
        { role: "system", content: params.systemPrompt },
      ];

      if (params.contextMessages && params.contextMessages.length > 0) {
        for (const msg of params.contextMessages.slice(0, 10)) {
          messages.push({ role: msg.role, content: msg.content });
        }
      }

      messages.push({ role: "user", content: params.userContent });

      const chatResult = await this.chatMessages(messages);

      return {
        content: chatResult.content,
        model: this.model,
        totalTokens: chatResult.total_tokens,
      };
    } catch (err) {
      console.warn(
        "[cognition:deepseek] respond failed, falling back to stub:",
        err,
      );
      return new StubLlmClient().respond(params);
    }
  }

  /** Chat with explicit messages array (for respond which needs context injection) */
  private async chatMessages(
    messages: { role: string; content: string }[],
    opts?: { temperature?: number; maxTokens?: number },
  ): Promise<ChatResult> {
    const res = await fetchWithTimeout(`${DEEPSEEK_BASE}/v1/chat/completions`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${this.apiKey}`,
      },
      body: JSON.stringify({
        model: this.model,
        messages,
        temperature: opts?.temperature ?? 0.5,
        max_tokens: opts?.maxTokens ?? 2048,
      }),
    });

    if (!res.ok) {
      const text = await res.text();
      throw new Error(`DeepSeek API error ${res.status}: ${text}`);
    }

    const data = (await res.json()) as {
      choices: { message: { content: string } }[];
      usage?: { total_tokens: number };
    };
    return {
      content: data.choices[0]?.message?.content ?? "",
      total_tokens: data.usage?.total_tokens,
    };
  }
}

// ─── Transform prompt builder ────────────────────────────────────────────────

interface TransformPrompt {
  system: string;
  user: string;
  temperature?: number;
  maxTokens?: number;
}

function buildTransformPrompt(op: TransformOp, personaId: Persona): TransformPrompt {
  const personaDef = getPersonaDef(personaId);
  const personaLayer = `\n\nPERSONA CONTEXT (adapt tone/structure):\n${personaDef.systemPrompt}`;

  const prompts: Record<TransformOp, TransformPrompt> = {
    summarize: {
      system: `You are a precision summarizer. Distill content to its essential core.${personaLayer}`,
      user: "Summarize the following content. Keep only the key points, decisions, and conclusions. Be concise.",
      temperature: 0.2,
      maxTokens: 1024,
    },
    expand: {
      system: `You are an elaboration engine. Add depth, examples, and connective tissue.${personaLayer}`,
      user: "Expand the following content. Add relevant detail, examples, context, and explanation. Flesh out any thin areas.",
      temperature: 0.5,
      maxTokens: 2048,
    },
    rewrite: {
      system: `You are a precision rewriter. Improve clarity, flow, and impact without changing meaning.${personaLayer}`,
      user: "Rewrite the following content for maximum clarity and impact. Preserve all factual content and intent. Fix grammar, structure, and flow.",
      temperature: 0.4,
      maxTokens: 2048,
    },
    counterargument: {
      system: `You are a steel-manner. Construct the strongest possible counterargument.${personaLayer}`,
      user: "Present the strongest counterargument to the following content. Address its core claims directly. Be fair but thorough.",
      temperature: 0.6,
      maxTokens: 2048,
    },
    alternative_framing: {
      system: `You are a perspective engine. Reframe content through different lenses.${personaLayer}`,
      user: "Reframe the following content from 2-3 alternative perspectives or worldviews. Show how different frames change the interpretation.",
      temperature: 0.7,
      maxTokens: 2048,
    },
    diagram: {
      system: `You are a visual thinker using ASCII/text diagrams. Create clear structural representations.${personaLayer}`,
      user: "Create an ASCII/text diagram representing the structure, flow, or relationships in the following content. Use box-drawing characters (─│┌┐└┘├┤┬┴┼) and clear labels.",
      temperature: 0.3,
      maxTokens: 1536,
    },
    checklist: {
      system: `You are an action extractor. Convert content into executable checklists.${personaLayer}`,
      user: "Extract all actionable items from the following content into a structured checklist. Use checkbox format: [ ] for incomplete, [x] for items already completed per the content.",
      temperature: 0.2,
      maxTokens: 1536,
    },
    plan: {
      system: `You are a strategic planner. Convert concepts into structured execution plans.${personaLayer}`,
      user: "Convert the following content into a structured execution plan with phases, milestones, dependencies, and estimated timelines. Include success criteria.",
      temperature: 0.4,
      maxTokens: 2048,
    },
    persona_rewrite: {
      system: `${personaDef.systemPrompt}`,
      user: "Rewrite the following content in your voice. Maintain all factual content but adapt the tone, structure, and framing to your persona.",
      temperature: 0.5,
      maxTokens: 2048,
    },
    condense: {
      system: `You are a compression engine. Reduce content to its absolute minimum.${personaLayer}`,
      user: "Condense the following content to its absolute essentials. Remove all redundancy. Every remaining word must carry weight. Target: 25-30% of original length.",
      temperature: 0.1,
      maxTokens: 1024,
    },
    extract: {
      system: `You are an insight miner. Pull key insights, patterns, and metadata from content.${personaLayer}`,
      user: "Extract the key insights, entities, decisions, and patterns from the following content. Return as labeled sections (Insights, Entities, Decisions, Patterns).",
      temperature: 0.3,
      maxTokens: 1536,
    },
    invert: {
      system: `You are a logic inverter. Reverse perspectives, assumptions, and conclusions.${personaLayer}`,
      user: "Invert the perspective of the following content. Reverse the assumptions, flip the conclusions, and explore what the opposite framing reveals.",
      temperature: 0.6,
      maxTokens: 2048,
    },
    simulate: {
      system: `You are a dialogue simulator. Generate multi-party conversation from content.${personaLayer}`,
      user: "Simulate a multi-party dialogue based on the following content. Create 2-4 distinct voices/perspectives discussing the topic. Format as a script with speaker labels.",
      temperature: 0.7,
      maxTokens: 2048,
    },
    draft: {
      system: `You are a drafting engine. Compose polished, ready-to-send communications.${personaLayer}`,
      user: "Draft a polished, ready-to-use communication based on the following content. Make it complete and self-contained — suitable for sending to its intended audience.",
      temperature: 0.5,
      maxTokens: 2048,
    },
    diagnose: {
      system: `You are a diagnostic engine. Identify logical weaknesses, ambiguities, and gaps.${personaLayer}`,
      user: "Diagnose the following content for logical weaknesses, internal ambiguities, missing premises, and structural gaps. Report each issue with severity and suggested fix.",
      temperature: 0.4,
      maxTokens: 2048,
    },
  };

  return prompts[op];
}

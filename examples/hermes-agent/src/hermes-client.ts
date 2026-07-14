export type HermesConversationMessage = {
  role: "assistant" | "user";
  content: string;
};

export type HermesCapabilities = {
  object?: string;
  platform: string;
  model?: string;
  features: Record<string, unknown>;
};

export type HermesRun = {
  runId: string;
  status: string;
  sessionId?: string;
};

export type HermesRunEvent = {
  event: string;
  run_id?: string;
  timestamp?: number;
  tool?: string;
  preview?: string;
  duration?: number;
  error?: string | boolean;
  delta?: string;
  output?: string;
  [key: string]: unknown;
};

export type HermesClientOptions = {
  baseUrl: string;
  apiKey?: string;
  fetch?: typeof fetch;
  maxErrorBytes?: number;
  maxEventBytes?: number;
};

export class HermesProtocolError extends Error {
  readonly status?: number;

  constructor(message: string, status?: number) {
    super(message);
    this.name = "HermesProtocolError";
    this.status = status;
  }
}

const DEFAULT_MAX_ERROR_BYTES = 8 * 1024;
const DEFAULT_MAX_EVENT_BYTES = 1024 * 1024;
const REQUIRED_FEATURES = ["run_submission", "run_status", "run_events_sse", "run_stop"] as const;

export class HermesClient {
  private readonly baseUrl: string;
  private readonly apiKey?: string;
  private readonly fetcher: typeof fetch;
  private readonly maxErrorBytes: number;
  private readonly maxEventBytes: number;

  constructor(options: HermesClientOptions) {
    this.baseUrl = options.baseUrl.replace(/\/$/, "");
    this.apiKey = options.apiKey?.trim() || undefined;
    this.fetcher = options.fetch ?? fetch;
    this.maxErrorBytes = options.maxErrorBytes ?? DEFAULT_MAX_ERROR_BYTES;
    this.maxEventBytes = options.maxEventBytes ?? DEFAULT_MAX_EVENT_BYTES;
    if (!this.baseUrl) throw new HermesProtocolError("Hermes base URL is required");
    if (this.maxErrorBytes < 1) throw new HermesProtocolError("maxErrorBytes must be positive");
    if (this.maxEventBytes < 1) throw new HermesProtocolError("maxEventBytes must be positive");
  }

  async assertCompatible(signal?: AbortSignal): Promise<HermesCapabilities> {
    const capabilities = await this.requestJson<HermesCapabilities>("/v1/capabilities", {
      signal,
    });
    if (capabilities.platform !== "hermes-agent" || !capabilities.features) {
      throw new HermesProtocolError("Endpoint is not a Hermes Agent API server");
    }
    const missing = REQUIRED_FEATURES.filter((feature) => capabilities.features[feature] !== true);
    if (missing.length > 0) {
      throw new HermesProtocolError(
        `Hermes API server is missing required features: ${missing.join(", ")}`,
      );
    }
    return capabilities;
  }

  async startRun(input: {
    input: string;
    conversationHistory?: HermesConversationMessage[];
    instructions?: string;
    sessionId: string;
    sessionKey: string;
    signal?: AbortSignal;
  }): Promise<HermesRun> {
    const message = input.input.trim();
    if (!message) throw new HermesProtocolError("Hermes run input must not be empty");
    validateSessionIdentity(input.sessionId, "sessionId");
    validateSessionIdentity(input.sessionKey, "sessionKey");
    const body = {
      input: message,
      ...(input.conversationHistory?.length
        ? { conversation_history: input.conversationHistory }
        : {}),
      ...(input.instructions?.trim() ? { instructions: input.instructions.trim() } : {}),
      session_id: input.sessionId,
    };
    const result = await this.requestJson<Record<string, unknown>>("/v1/runs", {
      method: "POST",
      body: JSON.stringify(body),
      headers: { "X-Hermes-Session-Key": input.sessionKey },
      signal: input.signal,
    });
    if (typeof result.run_id !== "string" || !result.run_id) {
      throw new HermesProtocolError("Hermes run response is missing run_id");
    }
    return {
      runId: result.run_id,
      status: typeof result.status === "string" ? result.status : "queued",
      ...(typeof result.session_id === "string" ? { sessionId: result.session_id } : {}),
    };
  }

  async *streamRunEvents(runId: string, signal?: AbortSignal): AsyncGenerator<HermesRunEvent> {
    if (!runId) throw new HermesProtocolError("Hermes run ID is required");
    const response = await this.fetcher(
      `${this.baseUrl}/v1/runs/${encodeURIComponent(runId)}/events`,
      {
        method: "GET",
        headers: this.headers({ Accept: "text/event-stream" }),
        signal,
      },
    );
    if (!response.ok) throw await this.responseError(response);
    const contentType = response.headers.get("content-type")?.toLowerCase() ?? "";
    if (!contentType.includes("text/event-stream")) {
      const detail = await readTextLimited(response, this.maxErrorBytes);
      throw new HermesProtocolError(
        `Hermes run events returned ${contentType || "an unknown content type"}${detail ? `: ${detail}` : ""}`,
        response.status,
      );
    }
    if (!response.body) throw new HermesProtocolError("Hermes run events response has no body");

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        let boundary = findEventBoundary(buffer);
        while (boundary) {
          const frame = buffer.slice(0, boundary.index);
          buffer = buffer.slice(boundary.index + boundary.length);
          if (frame.length > this.maxEventBytes) {
            throw new HermesProtocolError("Hermes SSE event exceeded the configured size limit");
          }
          const event = decodeSseFrame(frame);
          if (event === DONE) return;
          if (event) yield event;
          boundary = findEventBoundary(buffer);
        }
        if (buffer.length > this.maxEventBytes) {
          throw new HermesProtocolError("Hermes SSE event exceeded the configured size limit");
        }
      }
      buffer += decoder.decode();
      if (buffer.length > this.maxEventBytes) {
        throw new HermesProtocolError("Hermes SSE event exceeded the configured size limit");
      }
      if (buffer.trim()) {
        const event = decodeSseFrame(buffer);
        if (event && event !== DONE) yield event;
      }
    } finally {
      reader.releaseLock();
    }
  }

  async stopRun(runId: string, signal?: AbortSignal): Promise<void> {
    if (!runId) throw new HermesProtocolError("Hermes run ID is required");
    await this.requestJson(`/v1/runs/${encodeURIComponent(runId)}/stop`, {
      method: "POST",
      body: "{}",
      signal,
    });
  }

  private headers(extra: HeadersInit = {}): Headers {
    const headers = new Headers(extra);
    if (!headers.has("Accept")) headers.set("Accept", "application/json");
    if (this.apiKey) headers.set("Authorization", `Bearer ${this.apiKey}`);
    return headers;
  }

  private async requestJson<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers = this.headers(init.headers);
    if (init.body && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");
    const response = await this.fetcher(`${this.baseUrl}${path}`, { ...init, headers });
    if (!response.ok) throw await this.responseError(response);
    const text = await readTextLimited(response, this.maxEventBytes);
    if (!text) return undefined as T;
    try {
      return JSON.parse(text) as T;
    } catch {
      throw new HermesProtocolError(`Hermes returned invalid JSON for ${path}`, response.status);
    }
  }

  private async responseError(response: Response): Promise<HermesProtocolError> {
    const detail = await readTextLimited(response, this.maxErrorBytes);
    return new HermesProtocolError(
      `Hermes request failed with ${response.status}${detail ? `: ${detail}` : ""}`,
      response.status,
    );
  }
}

function validateSessionIdentity(value: string, label: string): void {
  if (!value || value.length > 256 || hasControlCharacters(value)) {
    throw new HermesProtocolError(`${label} must be 1-256 characters without control characters`);
  }
}

function hasControlCharacters(value: string): boolean {
  for (const character of value) {
    const codePoint = character.codePointAt(0) ?? 0;
    if (codePoint <= 31 || codePoint === 127) return true;
  }
  return false;
}

async function readTextLimited(response: Response, limit: number): Promise<string> {
  if (!response.body) return (await response.text()).slice(0, limit);
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    while (size < limit) {
      const { done, value } = await reader.read();
      if (done) break;
      const remaining = limit - size;
      const chunk = value.byteLength > remaining ? value.subarray(0, remaining) : value;
      chunks.push(chunk);
      size += chunk.byteLength;
      if (value.byteLength > remaining) {
        await reader.cancel();
        break;
      }
    }
  } finally {
    reader.releaseLock();
  }
  const bytes = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return new TextDecoder().decode(bytes);
}

const DONE = Symbol("sse-done");

type EventBoundary = { index: number; length: number };

function findEventBoundary(buffer: string): EventBoundary | undefined {
  const match = /\r?\n\r?\n/u.exec(buffer);
  return match ? { index: match.index, length: match[0].length } : undefined;
}

function decodeSseFrame(frame: string): HermesRunEvent | typeof DONE | undefined {
  let eventName = "";
  const data: string[] = [];
  for (const line of frame.split(/\r?\n/u)) {
    if (!line || line.startsWith(":")) continue;
    const separator = line.indexOf(":");
    const field = separator === -1 ? line : line.slice(0, separator);
    let value = separator === -1 ? "" : line.slice(separator + 1);
    if (value.startsWith(" ")) value = value.slice(1);
    if (field === "event") eventName = value;
    if (field === "data") data.push(value);
  }
  if (data.length === 0) return undefined;
  const raw = data.join("\n");
  if (raw === "[DONE]") return DONE;
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    throw new HermesProtocolError("Hermes returned invalid JSON in an SSE event");
  }
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new HermesProtocolError("Hermes returned a non-object SSE event");
  }
  const event = value as Record<string, unknown>;
  const resolvedName = typeof event.event === "string" && event.event ? event.event : eventName;
  if (!resolvedName) throw new HermesProtocolError("Hermes SSE event is missing an event name");
  return { ...event, event: resolvedName } as HermesRunEvent;
}

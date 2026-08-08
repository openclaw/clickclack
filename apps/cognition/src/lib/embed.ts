// PROJECT LOGOS — Cognition Service: Embedding provider
//
// Pluggable embedding backends: local (transformers.js), OpenAI, stub.
// Select via EMBED_PROVIDER env (default: local).
//
// LocalEmbedder uses @xenova/transformers with a small ONNX model
// (~90 MB download on first use, cached to HF_HOME or node_modules/.cache).
// Falls back gracefully to stub (zero vectors + substring search) on failure.

// ─── Interface ───────────────────────────────────────────────────────────────

export interface EmbedProvider {
  /** Generate a normalized embedding vector for text. */
  embed(text: string): Promise<number[]>;

  /** Provider name for telemetry. */
  readonly provider: string;

  /** Whether embeddings are real (non-zero). */
  readonly isReal: boolean;

  /** Kick off a warmup embed in the background; returns when the model is ready. */
  warmup(): Promise<void>;
}

// ─── Factory ─────────────────────────────────────────────────────────────────

export function createEmbedProvider(): EmbedProvider {
  const provider = (process.env.EMBED_PROVIDER ?? "local").toLowerCase();

  switch (provider) {
    case "local":
      console.log("[cognition] embed provider: local (transformers.js)");
      return new LocalEmbedder();
    case "openai": {
      const key = process.env.OPENAI_API_KEY;
      if (!key) {
        console.warn(
          "[cognition] EMBED_PROVIDER=openai but OPENAI_API_KEY is not set, falling back to local",
        );
        return new LocalEmbedder();
      }
      const model = process.env.EMBED_MODEL ?? "text-embedding-3-small";
      console.log(`[cognition] embed provider: openai (${model})`);
      return new OpenAIEmbedder(key, model);
    }
    case "stub":
      console.log("[cognition] embed provider: stub (zero vectors)");
      return new StubEmbedder();
    default:
      console.warn(
        `[cognition] unknown EMBED_PROVIDER="${provider}", falling back to local`,
      );
      return new LocalEmbedder();
  }
}

// ─── Local embedder (transformers.js) ────────────────────────────────────────

const LOCAL_MODEL = "Xenova/all-MiniLM-L6-v2";
// all-MiniLM-L6-v2: 384-dim embeddings, ~90 MB download, fast on CPU.

class LocalEmbedder implements EmbedProvider {
  readonly provider = "local";
  private _ready = false;
  private _failed = false;
  private _loadPromise: Promise<void> | null = null;
  private _pipeline: unknown = null; // Pipeline from @xenova/transformers
  private _featureExtraction: ((text: string) => Promise<{ data: Float32Array }>) | null = null;

  get isReal(): boolean {
    return this._ready && !this._failed;
  }

  async embed(text: string): Promise<number[]> {
    // Lazy-load on first use
    if (!this._ready && !this._failed) {
      if (!this._loadPromise) {
        this._loadPromise = this._loadModel();
      }
      await this._loadPromise;
    }

    if (this._failed || !this._featureExtraction) {
      // Graceful fallback: zero vector + substring search (via store)
      console.warn("[cognition:embed] local model unavailable, using zero-vector fallback");
      return new Array(384).fill(0);
    }

    try {
      const result = await this._featureExtraction(text.slice(0, 512));
      const vec = Array.from(result.data);
      return this._normalize(vec);
    } catch (err) {
      console.warn("[cognition:embed] inference failed:", err);
      return new Array(384).fill(0);
    }
  }

  async warmup(): Promise<void> {
    if (!this._loadPromise) {
      this._loadPromise = this._loadModel();
    }
    await this._loadPromise;
  }

  private async _loadModel(): Promise<void> {
    try {
      console.log(`[cognition:embed] loading local model: ${LOCAL_MODEL}...`);
      const start = Date.now();

      // Dynamic import — transformers.js pulls ONNX runtime + model weights
      const { pipeline } = await import("@xenova/transformers");
      this._pipeline = await pipeline("feature-extraction", LOCAL_MODEL, {
        // quantized: true reduces download size for ONNX models
        quantized: true,
      });

      const pipe = this._pipeline as { (text: string, options?: { pooling?: string; normalize?: boolean }): Promise<{ data: Float32Array }> };
      this._featureExtraction = async (text: string) => {
        return pipe(text, { pooling: "mean", normalize: true });
      };

      this._ready = true;
      console.log(
        `[cognition:embed] model loaded in ${Date.now() - start}ms: ${LOCAL_MODEL}`,
      );
    } catch (err) {
      this._failed = true;
      console.warn(
        `[cognition:embed] FAILED to load ${LOCAL_MODEL}:`,
        err instanceof Error ? err.message : String(err),
      );
      console.warn(
        "[cognition:embed] semantic search will fall back to substring matching",
      );
    }
  }

  private _normalize(vec: number[]): number[] {
    let sumSq = 0;
    for (const v of vec) sumSq += v * v;
    const mag = Math.sqrt(sumSq);
    if (mag === 0) return vec;
    return vec.map((v) => v / mag);
  }
}

// ─── OpenAI embedder ─────────────────────────────────────────────────────────

class OpenAIEmbedder implements EmbedProvider {
  readonly provider = "openai";
  readonly isReal = true;
  private apiKey: string;
  private model: string;

  constructor(apiKey: string, model: string) {
    this.apiKey = apiKey;
    this.model = model;
  }

  async embed(text: string): Promise<number[]> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 30_000);

    try {
      const res = await fetch("https://api.openai.com/v1/embeddings", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${this.apiKey}`,
        },
        body: JSON.stringify({
          model: this.model,
          input: text.slice(0, 8191),
        }),
        signal: controller.signal,
      });

      if (!res.ok) {
        const errText = await res.text();
        throw new Error(`OpenAI embeddings error ${res.status}: ${errText}`);
      }

      const data = (await res.json()) as {
        data: { embedding: number[] }[];
      };
      return data.data[0]?.embedding ?? new Array(1536).fill(0);
    } catch (err) {
      console.warn("[cognition:embed] OpenAI embed failed:", err);
      return new Array(1536).fill(0);
    } finally {
      clearTimeout(timer);
    }
  }

  async warmup(): Promise<void> {
    // OpenAI doesn't need warmup (stateless API)
  }
}

// ─── Stub embedder ───────────────────────────────────────────────────────────

class StubEmbedder implements EmbedProvider {
  readonly provider = "stub";
  readonly isReal = false;

  async embed(_text: string): Promise<number[]> {
    return new Array(384).fill(0);
  }

  async warmup(): Promise<void> {
    // no-op
  }
}

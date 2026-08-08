// PROJECT LOGOS — Cognition Service: storage interface + JSON file store
//
// Minimal persistence for memory anchors and semantic thread clusters.
// Interface is abstract; current implementation is a JSON file store.
// Swap for SQLite/Postgres later by implementing MemoryStore + ThreadStore.
//
// Data layout:
//   ./data/anchors.json → MemoryAnchor[]
//   ./data/threads.json  → ThreadCluster[]

import * as fs from "node:fs";
import * as path from "node:path";
import { randomUUID } from "node:crypto";

import type { AnchorRequest, AnchorResult, MemoryNode } from "../types.js";
import type { EmbedProvider } from "./embed.js";

// ─── Data types ───────────────────────────────────────────────────────────────

interface MemoryAnchor {
  id: string;
  content: string;
  source_message_id?: string;
  created_at: string;
  tags: string[];
  /** Pre-computed embedding vector for semantic search */
  embedding?: number[];
}

interface ThreadCluster {
  id: string;
  label: string;
  message_ids: string[];
  created_at: string;
  updated_at: string;
}

// ─── Store interface ─────────────────────────────────────────────────────────

export interface MemoryStore {
  saveAnchor(req: AnchorRequest): Promise<AnchorResult>;
  /** Query anchors by semantic similarity (embedding-based) or fallback to text match */
  queryAnchors(q: string, limit?: number): Promise<MemoryNode[]>;
  listAnchors(): Promise<MemoryNode[]>;
  /** Set the embed provider for semantic search */
  setEmbedProvider(provider: EmbedProvider): void;
}

export interface ThreadStore {
  saveThread(label: string, messageIds: string[]): Promise<ThreadCluster>;
  getThread(id: string): Promise<ThreadCluster | null>;
  listThreads(): Promise<ThreadCluster[]>;
  deleteThread(id: string): Promise<boolean>;
}

// ─── Cosine similarity ──────────────────────────────────────────────────────

export function cosineSimilarity(a: number[], b: number[]): number {
  if (a.length !== b.length) return 0;
  let dot = 0;
  let magA = 0;
  let magB = 0;
  for (let i = 0; i < a.length; i++) {
    dot += a[i] * b[i];
    magA += a[i] * a[i];
    magB += b[i] * b[i];
  }
  const denom = Math.sqrt(magA) * Math.sqrt(magB);
  return denom === 0 ? 0 : dot / denom;
}

// ─── JSON file store implementation ──────────────────────────────────────────

export class JsonFileStore implements MemoryStore, ThreadStore {
  private dataDir: string;
  private anchorsFile: string;
  private threadsFile: string;
  private embedProvider: EmbedProvider | null = null;

  constructor(dataDir?: string) {
    this.dataDir = dataDir ?? process.env.DATA_DIR ?? "./data";
    this.anchorsFile = path.join(this.dataDir, "anchors.json");
    this.threadsFile = path.join(this.dataDir, "threads.json");
    this.ensureDataDir();
  }

  setEmbedProvider(provider: EmbedProvider): void {
    this.embedProvider = provider;
  }

  /** Expose embed provider for use by clustering and memory citations */
  getEmbedProvider(): EmbedProvider | null {
    return this.embedProvider;
  }

  // ── Memory anchors ───────────────────────────────────────────────────────

  async saveAnchor(req: AnchorRequest): Promise<AnchorResult> {
    const anchors = this.readAnchors();
    const now = new Date().toISOString();

    // Generate embedding if embed provider available and real
    let embedding: number[] | undefined;
    if (this.embedProvider) {
      try {
        embedding = await this.embedProvider.embed(req.content);
        // Only store non-stub embeddings
        const isStub = embedding.every((v) => v === 0);
        if (isStub) {
          embedding = undefined;
          console.log("[cognition:store] embed returned stub, storing anchor without vector");
        }
      } catch (err) {
        console.warn("[cognition:store] embed failed for anchor, storing without:", err);
      }
    }

    const record: MemoryAnchor = {
      id: randomUUID(),
      content: req.content,
      source_message_id: req.source_message_id,
      created_at: now,
      tags: req.tags ?? [],
      embedding,
    };
    anchors.push(record);
    this.writeAnchors(anchors);

    const { embedding: _, ...result } = record;
    return result;
  }

  async queryAnchors(q: string, limit = 10): Promise<MemoryNode[]> {
    const anchors = this.readAnchors();

    // Try semantic search if we have a real embed provider
    if (this.embedProvider && this.embedProvider.isReal) {
      try {
        const queryEmbedding = await this.embedProvider.embed(q);

        // Check if embedding is real (non-stub)
        const isStub = queryEmbedding.every((v) => v === 0);

        if (!isStub) {
          // Semantic search: cosine similarity over stored embeddings
          const scored = anchors
            .map((a) => {
              if (!a.embedding || a.embedding.length === 0) {
                return { ...a, score: 0 };
              }
              if (a.embedding.length !== queryEmbedding.length) {
                // Dimension mismatch (e.g., old OpenAI 1536-dim anchors with new 384-dim model)
                return { ...a, score: 0 };
              }
              const score = cosineSimilarity(queryEmbedding, a.embedding);
              return { ...a, score };
            })
            .sort((a, b) => b.score - a.score)
            .slice(0, limit);

          return scored.map(({ embedding: _, ...rest }) => rest);
        }
      } catch (err) {
        console.warn("[cognition:store] semantic search failed, falling back to text:", err);
      }
    }

    // Fallback: substring search (original behavior)
    console.log("[cognition:store] using substring fallback for query:", q.slice(0, 60));
    const lower = q.toLowerCase();
    const scored = anchors
      .map((a) => {
        const contentLower = a.content.toLowerCase();
        const tagMatch = a.tags.some((t) => lower.includes(t.toLowerCase()));
        const contentMatch = contentLower.includes(lower);
        const score = tagMatch ? 0.9 : contentMatch ? 0.6 : 0.0;
        return { ...a, score };
      })
      .filter((a) => a.score > 0)
      .sort((a, b) => b.score - a.score)
      .slice(0, limit);

    return scored.map(({ embedding: _, ...rest }) => rest);
  }

  async listAnchors(): Promise<MemoryNode[]> {
    const anchors = this.readAnchors();
    return anchors.map(({ embedding: _, ...rest }) => rest);
  }

  // ── Thread clusters ──────────────────────────────────────────────────────

  async saveThread(
    label: string,
    messageIds: string[],
  ): Promise<ThreadCluster> {
    const threads = this.readThreads();
    const now = new Date().toISOString();
    const record: ThreadCluster = {
      id: randomUUID(),
      label,
      message_ids: messageIds,
      created_at: now,
      updated_at: now,
    };
    threads.push(record);
    this.writeThreads(threads);
    return record;
  }

  async getThread(id: string): Promise<ThreadCluster | null> {
    return this.readThreads().find((t) => t.id === id) ?? null;
  }

  async listThreads(): Promise<ThreadCluster[]> {
    return this.readThreads();
  }

  async deleteThread(id: string): Promise<boolean> {
    const threads = this.readThreads();
    const idx = threads.findIndex((t) => t.id === id);
    if (idx === -1) return false;
    threads.splice(idx, 1);
    this.writeThreads(threads);
    return true;
  }

  // ── Internal JSON file I/O ───────────────────────────────────────────────

  private ensureDataDir(): void {
    if (!fs.existsSync(this.dataDir)) {
      fs.mkdirSync(this.dataDir, { recursive: true });
    }
  }

  private readAnchors(): MemoryAnchor[] {
    try {
      if (!fs.existsSync(this.anchorsFile)) return [];
      const raw = fs.readFileSync(this.anchorsFile, "utf-8");
      return JSON.parse(raw) as MemoryAnchor[];
    } catch {
      return [];
    }
  }

  private writeAnchors(anchors: MemoryAnchor[]): void {
    fs.writeFileSync(this.anchorsFile, JSON.stringify(anchors, null, 2), "utf-8");
  }

  private readThreads(): ThreadCluster[] {
    try {
      if (!fs.existsSync(this.threadsFile)) return [];
      const raw = fs.readFileSync(this.threadsFile, "utf-8");
      return JSON.parse(raw) as ThreadCluster[];
    } catch {
      return [];
    }
  }

  private writeThreads(threads: ThreadCluster[]): void {
    fs.writeFileSync(this.threadsFile, JSON.stringify(threads, null, 2), "utf-8");
  }
}

// ─── Singleton ────────────────────────────────────────────────────────────────

let _store: JsonFileStore | undefined;

export function getStore(): JsonFileStore {
  if (!_store) {
    _store = new JsonFileStore();
    console.log(`[cognition] store: JSON file at ${_store["dataDir"]}`);
  }
  return _store;
}

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

// ─── Data types ───────────────────────────────────────────────────────────────

interface MemoryAnchor {
  id: string;
  content: string;
  source_message_id?: string;
  created_at: string;
  tags: string[];
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
  queryAnchors(q: string, limit?: number): Promise<MemoryNode[]>;
  listAnchors(): Promise<MemoryNode[]>;
}

export interface ThreadStore {
  saveThread(label: string, messageIds: string[]): Promise<ThreadCluster>;
  getThread(id: string): Promise<ThreadCluster | null>;
  listThreads(): Promise<ThreadCluster[]>;
  deleteThread(id: string): Promise<boolean>;
}

// ─── JSON file store implementation ──────────────────────────────────────────

export class JsonFileStore implements MemoryStore, ThreadStore {
  private dataDir: string;
  private anchorsFile: string;
  private threadsFile: string;

  constructor(dataDir?: string) {
    this.dataDir = dataDir ?? process.env.DATA_DIR ?? "./data";
    this.anchorsFile = path.join(this.dataDir, "anchors.json");
    this.threadsFile = path.join(this.dataDir, "threads.json");
    this.ensureDataDir();
  }

  // ── Memory anchors ───────────────────────────────────────────────────────

  async saveAnchor(req: AnchorRequest): Promise<AnchorResult> {
    const anchors = this.readAnchors();
    const now = new Date().toISOString();
    const record: MemoryAnchor = {
      id: randomUUID(),
      content: req.content,
      source_message_id: req.source_message_id,
      created_at: now,
      tags: req.tags ?? [],
    };
    anchors.push(record);
    this.writeAnchors(anchors);
    return record;
  }

  async queryAnchors(q: string, limit = 10): Promise<MemoryNode[]> {
    const anchors = this.readAnchors();
    const lower = q.toLowerCase();
    // Naive substring match (placeholder — real impl uses embeddings + vector search)
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

    return scored;
  }

  async listAnchors(): Promise<MemoryNode[]> {
    return this.readAnchors();
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

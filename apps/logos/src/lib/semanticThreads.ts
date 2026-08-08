/**
 * PROJECT LOGOS — Semantic Thread Cluster Cache
 *
 * Module-level cache for cluster results. Survives component remounts.
 *
 * API:
 *   runClustering(items)      — batch-call /threads/cluster, chunk ≤20, merge, CL-01 labels
 *   getClusterId(messageId)   — cluster ID string | null
 *   getClusterLabel(msgId)    — "CL-01" format
 *   getClusterMessageIds(id)  — message ids in a cluster
 *   getClusters()             — all cluster entries
 *   isRunning()               — whether a clustering run is in progress
 *   clear()                   — reset all cache
 */

import { cluster, type ClusterResult, type ClusterInfo } from "./cognition";

// ── Module-level cache ──

/** Message id → cluster id lookup. */
let _assignments = new Map<string, string>();

/** Cluster id → cluster metadata. */
let _clusters = new Map<string, ClusterInfo>();

/** Whether a clustering run is in progress. */
let _running = false;

// ── Public API ──

export function getClusterId(messageId: string): string | null {
  return _assignments.get(messageId) ?? null;
}

export function getClusterLabel(messageId: string): string | null {
  const cid = getClusterId(messageId);
  if (!cid) return null;
  const cl = _clusters.get(cid);
  return cl ? cl.label : cid;
}

export function getClusterMessageCount(clusterId: string): number {
  const cl = _clusters.get(clusterId);
  return cl?.message_ids.length ?? 0;
}

export function getClusterMessageIds(clusterId: string): string[] {
  const cl = _clusters.get(clusterId);
  return cl?.message_ids ?? [];
}

export function getClusters(): ClusterInfo[] {
  return [..._clusters.values()];
}

export function isRunning(): boolean {
  return _running;
}

export function clear(): void {
  _assignments = new Map();
  _clusters = new Map();
  _running = false;
}

/**
 * Run clustering on a batch of messages. Chunked at 20 items per call.
 * Clusters are merged with CL-01, CL-02, ... labels.
 */
export async function runClustering(
  items: Array<{ id: string; content: string }>,
): Promise<void> {
  if (_running) return;
  _running = true;

  try {
    const CHUNK_SIZE = 20;
    const allAssignments = new Map<string, string>();
    const allClusters = new Map<string, ClusterInfo>();
    let clusterCounter = 0;

    for (let i = 0; i < items.length; i += CHUNK_SIZE) {
      const chunk = items.slice(i, i + CHUNK_SIZE);
      const result = await cluster(chunk);
      if (!result) continue;

      // Remap server cluster IDs to sequential CL-01, CL-02, ...
      const idRemap = new Map<string, string>();
      for (const cl of result.clusters) {
        clusterCounter++;
        const newId = `CL-${String(clusterCounter).padStart(2, "0")}`;
        idRemap.set(cl.id, newId);
        allClusters.set(newId, {
          id: newId,
          label: `CL-${String(clusterCounter).padStart(2, "0")}`,
          message_ids: cl.message_ids,
        });
      }

      for (const a of result.assignments) {
        const remappedId = idRemap.get(a.cluster_id) ?? a.cluster_id;
        allAssignments.set(a.message_id, remappedId);
      }
    }

    _assignments = allAssignments;
    _clusters = allClusters;
  } catch (err) {
    console.error("[logos:semanticThreads] clustering failed:", err);
  } finally {
    _running = false;
  }
}

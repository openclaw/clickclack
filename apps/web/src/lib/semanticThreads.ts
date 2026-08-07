/**
 * PROJECT LOGOS — Semantic Thread Cluster Cache (Track B)
 *
 * Module-level cache for cluster results. Survives component remounts.
 *
 * API:
 *   getClusterId(messageId)  → cluster ID string | null
 *   getClusterLabel(msgId)   → "CL-01" format
 *   runClustering(items)     → batch-call /threads/cluster, store results
 *   clear()
 */

import { cluster, type ClusterResult } from './cognition';

// ── Module-level cache ──

/** Most recent cluster result (one per conversation at a time). */
let _lastResult: ClusterResult | null = null;

/** Message id → cluster id lookup. */
let _assignments = new Map<string, string>();

/** Cluster id → cluster metadata. */
let _clusters = new Map<string, { id: string; label: string; message_ids: string[] }>();

/** Whether a clustering run is in progress. */
let _running = false;

// ── Public API ──

export function getClusterId(messageId: string): string | null {
  return _assignments.get(messageId) ?? null;
}

export function getClusterLabel(messageId: string): string | null {
  const cid = getClusterId(messageId);
  if (!cid) return null;
  const cluster = _clusters.get(cid);
  return cluster ? cluster.label : cid;
}

export function getClusterMessageCount(clusterId: string): number {
  const cluster = _clusters.get(clusterId);
  return cluster?.message_ids.length ?? 0;
}

export function getClusterMessageIds(clusterId: string): string[] {
  const cluster = _clusters.get(clusterId);
  return cluster?.message_ids ?? [];
}

export function getClusters(): Array<{ id: string; label: string; message_ids: string[] }> {
  return [..._clusters.values()];
}

export function getAssignments(): Map<string, string> {
  return new Map(_assignments);
}

export function isRunning(): boolean {
  return _running;
}

/**
 * Run clustering on a batch of message contents. Max 20 per call (chunked).
 * Results are merged into the module-level cache.
 */
export async function runClustering(
  items: Array<{ id: string; content: string }>,
): Promise<void> {
  if (_running) return; // Prevent concurrent runs
  _running = true;

  try {
    const CHUNK_SIZE = 20;
    const allAssignments = new Map<string, string>();
    const allClusters = new Map<string, { id: string; label: string; message_ids: string[] }>();
    let clusterCounter = 0;

    for (let i = 0; i < items.length; i += CHUNK_SIZE) {
      const chunk = items.slice(i, i + CHUNK_SIZE);

      // Build contents array mapped to indices for assignment remapping
      const chunkItems = chunk.map(item => ({ id: item.id, content: item.content }));
      const idMap = chunk.map(item => item.id);

      const result = await cluster(chunkItems);
      if (!result) continue;

      // Remap cluster IDs to avoid collisions across chunks
      const idRemap = new Map<string, string>();
      for (const cl of result.clusters) {
        clusterCounter++;
        const newId = `CL-${String(clusterCounter).padStart(2, '0')}`;
        const newLabel = `CL-${String(clusterCounter).padStart(2, '0')}`;
        idRemap.set(cl.id, newId);
        allClusters.set(newId, {
          id: newId,
          label: newLabel,
          // Server returns real message IDs now (message_ids echoed from request)
          message_ids: cl.message_ids.map((mid: string) => (idMap.includes(mid) ? mid : mid)),
        });
      }

      for (const assignment of result.assignments) {
        const originalClusterId = assignment.cluster_id;
        const remappedClusterId = idRemap.get(originalClusterId) ?? originalClusterId;
        // Server echoes real message IDs (request message_ids)
        allAssignments.set(assignment.message_id, remappedClusterId);
      }
    }

    _assignments = allAssignments;
    _clusters = allClusters;
    _lastResult = { clusters: [...allClusters.values()], assignments: [] };
  } catch (err) {
    console.error('[semanticThreads] clustering failed:', err);
  } finally {
    _running = false;
  }
}

export function clear(): void {
  _lastResult = null;
  _assignments = new Map();
  _clusters = new Map();
  _running = false;
}

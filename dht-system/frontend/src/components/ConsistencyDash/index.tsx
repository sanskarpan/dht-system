import React, { useState, useEffect, useCallback, useRef } from 'react';
import * as d3 from 'd3';
import { useDHTStore } from '@/store/dhtStore';
import type { DHTStore } from '@/store/dhtStore';
import { getKeyReplicas, deleteKey } from '@/api/client';
import type { ValueEntry } from '@/types/dht';

// ─── Types ────────────────────────────────────────────────────────────────────

interface ReplicaDetail {
  nodeId: string;
  entry: ValueEntry;
}

interface ReplicaCache {
  [keyId: string]: {
    loading: boolean;
    error: string | null;
    replicas: ReplicaDetail[];
  };
}

interface ConflictEvent {
  timestamp: string;
  key: string;
  nodeId: string;
  strategy: string;
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

function shortId(id: string, len = 12): string {
  if (!id) return '—';
  return id.length > len ? id.slice(0, len) + '…' : id;
}

function valuePreview(b64: string, maxChars = 50): string {
  if (!b64) return '—';
  try {
    const decoded = atob(b64);
    return decoded.length > maxChars ? decoded.slice(0, maxChars) + '…' : decoded;
  } catch {
    return b64.length > maxChars ? b64.slice(0, maxChars) + '…' : b64;
  }
}

function ttlRemaining(entry: ValueEntry): string {
  if (!entry.ttlNs || entry.ttlNs <= 0) return '∞';
  const writtenAt = new Date(entry.timestamp).getTime();
  const ttlMs = entry.ttlNs / 1_000_000;
  const expiresAt = writtenAt + ttlMs;
  const remaining = expiresAt - Date.now();
  if (remaining <= 0) return 'Expired';
  if (remaining < 60_000) return `${Math.floor(remaining / 1000)}s`;
  if (remaining < 3_600_000) return `${Math.floor(remaining / 60_000)}m`;
  return `${Math.floor(remaining / 3_600_000)}h`;
}

// ─── Sub-components ───────────────────────────────────────────────────────────

function VectorClockTags({ entries }: { entries: Record<string, number> }) {
  const pairs = Object.entries(entries);
  if (pairs.length === 0) return <span className="text-slate-600 text-xs italic">empty</span>;
  return (
    <span className="flex flex-wrap gap-1">
      {pairs.map(([node, ver]) => (
        <span
          key={node}
          className="px-1.5 py-0.5 rounded bg-slate-700 border border-slate-600 text-slate-300 font-mono text-xs"
        >
          {shortId(node, 8)}: {ver}
        </span>
      ))}
    </span>
  );
}

// ─── Vector Clock DAG (force graph) ──────────────────────────────────────────

interface VCNode {
  id: string;
  maxVersion: number;
  replicaCount: number;
}

interface VCLink {
  source: string;
  target: string;
}

function VectorClockDAG({ replicas }: { replicas: ReplicaDetail[] }) {
  const svgRef = useRef<SVGSVGElement>(null);
  const W = 320;
  const H = 180;

  useEffect(() => {
    if (!svgRef.current || replicas.length === 0) return;
    const svg = d3.select(svgRef.current);
    svg.selectAll('*').remove();

    // Build node set: each unique writer nodeId that appears in any clock
    const nodeMap = new Map<string, VCNode>();
    replicas.forEach((r) => {
      const clockEntries = r.entry?.clock ?? {};
      Object.entries(clockEntries).forEach(([writerNodeId, version]) => {
        const existing = nodeMap.get(writerNodeId);
        nodeMap.set(writerNodeId, {
          id: writerNodeId,
          maxVersion: Math.max(version, existing?.maxVersion ?? 0),
          replicaCount: (existing?.replicaCount ?? 0) + 1,
        });
      });
    });

    const nodes: VCNode[] = Array.from(nodeMap.values());
    if (nodes.length === 0) return;

    // Build edges: two writers are linked if they appear in the same replica's clock
    const edgeSet = new Set<string>();
    const links: VCLink[] = [];
    replicas.forEach((r) => {
      const writers = Object.keys(r.entry?.clock ?? {});
      for (let i = 0; i < writers.length; i++) {
        for (let j = i + 1; j < writers.length; j++) {
          const key = [writers[i], writers[j]].sort().join('|');
          if (!edgeSet.has(key)) {
            edgeSet.add(key);
            links.push({ source: writers[i], target: writers[j] });
          }
        }
      }
    });

    svg.append('rect').attr('width', W).attr('height', H).attr('fill', '#0d1117');
    svg.append('text')
      .attr('x', 8).attr('y', 14).attr('font-size', 9).attr('fill', '#475569')
      .text('Vector Clock Contributors');

    const maxVer = Math.max(...nodes.map((n) => n.maxVersion), 1);

    const simulation = d3.forceSimulation(nodes as d3.SimulationNodeDatum[])
      .force('link', d3.forceLink(links).id((d) => (d as VCNode).id).distance(60).strength(0.5))
      .force('charge', d3.forceManyBody().strength(-80))
      .force('center', d3.forceCenter(W / 2, H / 2))
      .force('collision', d3.forceCollide(18));

    // Edges
    const linkSel = svg.append('g').selectAll('line')
      .data(links)
      .join('line')
      .attr('stroke', '#334155')
      .attr('stroke-width', 1);

    // Nodes
    const nodeG = svg.append('g').selectAll('g')
      .data(nodes)
      .join('g')
      .attr('cursor', 'default');

    nodeG.append('circle')
      .attr('r', (d) => 6 + (d.maxVersion / maxVer) * 6)
      .attr('fill', (d) => d3.interpolateBlues(0.3 + 0.7 * (d.maxVersion / maxVer)))
      .attr('stroke', '#475569')
      .attr('stroke-width', 1);

    nodeG.append('text')
      .attr('dy', 3).attr('text-anchor', 'middle')
      .attr('font-size', 6).attr('font-family', 'monospace')
      .attr('fill', '#e2e8f0')
      .text((d) => d.id.slice(0, 6));

    nodeG.append('text')
      .attr('dy', -10).attr('text-anchor', 'middle')
      .attr('font-size', 6).attr('fill', '#64748b')
      .text((d) => `v${d.maxVersion}`);

    nodeG.append('title')
      .text((d) => `Node: ${d.id}\nMax version: ${d.maxVersion}\nIn ${d.replicaCount} replica clock(s)`);

    simulation.on('tick', () => {
      linkSel
        .attr('x1', (d) => Math.max(12, Math.min(W - 12, (d.source as d3.SimulationNodeDatum).x!)))
        .attr('y1', (d) => Math.max(12, Math.min(H - 12, (d.source as d3.SimulationNodeDatum).y!)))
        .attr('x2', (d) => Math.max(12, Math.min(W - 12, (d.target as d3.SimulationNodeDatum).x!)))
        .attr('y2', (d) => Math.max(12, Math.min(H - 12, (d.target as d3.SimulationNodeDatum).y!)));

      nodeG.attr('transform', (d) => {
        const nd = d as d3.SimulationNodeDatum;
        return `translate(${Math.max(16, Math.min(W - 16, nd.x!))},${Math.max(16, Math.min(H - 16, nd.y!))})`;
      });
    });

    return () => { simulation.stop(); };
  }, [replicas, W, H]);

  if (replicas.length === 0) return null;

  return (
    <div className="mt-3 pt-3 border-t border-slate-700">
      <p className="text-xs text-slate-500 uppercase tracking-wider mb-2">Vector Clock DAG</p>
      <svg ref={svgRef} width={W} height={H} className="rounded" />
      <p className="text-[10px] text-slate-600 mt-1">
        Node size = max version. Edges = writers seen in same replica clock.
      </p>
    </div>
  );
}

function ReplicaDetailRow({ replica, nodeMap }: {
  replica: ReplicaDetail;
  nodeMap: Map<string, { addr: string }>;
}) {
  const node = nodeMap.get(replica.nodeId);
  const clock = replica.entry?.clock ?? {};

  return (
    <tr className="border-b border-slate-700/40 hover:bg-slate-800/40">
      <td className="px-3 py-2 font-mono text-slate-400 text-xs whitespace-nowrap">
        {shortId(replica.nodeId, 10)}
      </td>
      <td className="px-3 py-2 text-slate-400 text-xs whitespace-nowrap">
        {node?.addr ?? <span className="text-slate-600 italic">unknown</span>}
      </td>
      <td className="px-3 py-2 font-mono text-slate-300 text-xs max-w-[180px] truncate">
        {valuePreview(replica.entry?.value ?? '', 50)}
      </td>
      <td className="px-3 py-2 text-xs">
        <VectorClockTags entries={clock} />
      </td>
      <td className="px-3 py-2 text-slate-400 text-xs whitespace-nowrap">
        {ttlRemaining(replica.entry)}
      </td>
    </tr>
  );
}

function ExpandedReplicaSection({
  keyId,
  keyStr,
  cache,
  nodeMap,
  onLoad,
}: {
  keyId: string;
  keyStr: string;
  cache: ReplicaCache;
  nodeMap: Map<string, { addr: string }>;
  onLoad: (keyId: string, keyStr: string) => void;
}) {
  const state = cache[keyId];

  useEffect(() => {
    if (!state) onLoad(keyId, keyStr);
  }, [keyId, keyStr, state, onLoad]);

  if (!state || state.loading) {
    return (
      <tr>
        <td colSpan={4} className="px-6 py-4 bg-slate-800/50">
          <div className="flex items-center gap-2 text-slate-500 text-xs">
            <span className="animate-pulse">Loading replica details…</span>
          </div>
        </td>
      </tr>
    );
  }

  if (state.error) {
    return (
      <tr>
        <td colSpan={4} className="px-6 py-4 bg-slate-800/50">
          <p className="text-red-400 text-xs">Error: {state.error}</p>
        </td>
      </tr>
    );
  }

  if (state.replicas.length === 0) {
    return (
      <tr>
        <td colSpan={4} className="px-6 py-4 bg-slate-800/50">
          <p className="text-slate-500 text-xs italic">No replica details available.</p>
        </td>
      </tr>
    );
  }

  return (
    <tr>
      <td colSpan={4} className="px-4 py-3 bg-slate-800/50 border-b border-slate-700">
        <div className="overflow-x-auto rounded border border-slate-700">
          <table className="w-full text-xs">
            <thead>
              <tr className="bg-slate-800 text-slate-500 border-b border-slate-700">
                <th className="text-left px-3 py-2">Node ID</th>
                <th className="text-left px-3 py-2">Address</th>
                <th className="text-left px-3 py-2">Value Preview</th>
                <th className="text-left px-3 py-2">Vector Clock</th>
                <th className="text-left px-3 py-2">TTL</th>
              </tr>
            </thead>
            <tbody>
              {state.replicas.map((r) => (
                <ReplicaDetailRow
                  key={r.nodeId}
                  replica={r}
                  nodeMap={nodeMap}
                />
              ))}
            </tbody>
          </table>
        </div>
        <VectorClockDAG replicas={state.replicas} />
      </td>
    </tr>
  );
}

// ─── Conflict Log ─────────────────────────────────────────────────────────────

function ConflictLog({ conflicts }: { conflicts: ConflictEvent[] }) {
  return (
    <div className="bg-slate-900 rounded border border-slate-800 mb-4">
      <div className="px-4 py-3 border-b border-slate-800 flex items-center justify-between">
        <p className="text-sm text-slate-300">Conflict Log</p>
        {conflicts.length > 0 && (
          <span className="px-2 py-0.5 rounded-full bg-red-900/60 text-red-300 text-xs font-mono">
            {conflicts.length}
          </span>
        )}
      </div>

      {conflicts.length === 0 ? (
        <p className="p-4 text-slate-500 text-sm italic">No conflicts detected.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="text-slate-500 border-b border-slate-800">
                <th className="text-left px-4 py-2 whitespace-nowrap">Time</th>
                <th className="text-left px-4 py-2">Key</th>
                <th className="text-left px-4 py-2">Strategy</th>
                <th className="text-left px-4 py-2">Resolution</th>
              </tr>
            </thead>
            <tbody>
              {conflicts.map((c, i) => (
                <tr key={i} className="border-b border-slate-800/50 hover:bg-slate-800/30">
                  <td className="px-4 py-2 font-mono text-slate-500 whitespace-nowrap">
                    {new Date(c.timestamp).toLocaleTimeString()}
                  </td>
                  <td className="px-4 py-2 font-mono text-slate-400">
                    {shortId(c.key, 14)}
                  </td>
                  <td className="px-4 py-2">
                    <span className="px-1.5 py-0.5 rounded bg-orange-900/50 text-orange-300 font-mono uppercase text-xs">
                      {c.strategy || 'LWW'}
                    </span>
                  </td>
                  <td className="px-4 py-2">
                    <span className="px-1.5 py-0.5 rounded bg-green-900/40 text-green-400 text-xs">
                      Resolved
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// ─── Convergence Meter ────────────────────────────────────────────────────────

function ConvergenceMeter({
  keys,
  replicationN,
}: {
  keys: Array<{ id: string; replicaCount: number }>;
  replicationN: number;
}) {
  if (keys.length === 0) {
    return (
      <div className="bg-slate-900 rounded border border-slate-800 mb-4">
        <div className="px-4 py-3 border-b border-slate-800">
          <p className="text-sm text-slate-300">Convergence</p>
        </div>
        <p className="p-4 text-slate-500 text-sm italic">No keys to measure.</p>
      </div>
    );
  }

  const fullyReplicated = keys.filter((k) => k.replicaCount >= replicationN).length;
  const total = keys.length;
  const score = Math.round((fullyReplicated / total) * 100);

  const barColor =
    score === 100
      ? 'bg-green-500'
      : score >= 70
      ? 'bg-yellow-500'
      : score >= 40
      ? 'bg-orange-500'
      : 'bg-red-500';

  const labelColor =
    score === 100
      ? 'text-green-400'
      : score >= 70
      ? 'text-yellow-400'
      : score >= 40
      ? 'text-orange-400'
      : 'text-red-400';

  return (
    <div className="bg-slate-900 rounded border border-slate-800 mb-4">
      <div className="px-4 py-3 border-b border-slate-800 flex items-center justify-between">
        <p className="text-sm text-slate-300">Convergence</p>
        <span className={`text-xs font-mono font-semibold ${labelColor}`}>
          {fullyReplicated}/{total} keys fully replicated
        </span>
      </div>

      <div className="px-4 py-4 space-y-3">
        {/* Main progress bar */}
        <div>
          <div className="flex items-center justify-between mb-1.5">
            <span className="text-xs text-slate-500">Consistency Score</span>
            <span className={`text-sm font-bold font-mono ${labelColor}`}>{score}%</span>
          </div>
          <div className="h-3 w-full bg-slate-800 rounded-full overflow-hidden border border-slate-700">
            <div
              className={`h-full rounded-full transition-all duration-500 ${barColor}`}
              style={{ width: `${score}%` }}
            />
          </div>
        </div>

        {/* Per-key mini bars */}
        <div className="space-y-1.5 max-h-36 overflow-y-auto pr-1">
          {keys.map((k) => {
            const pct = replicationN > 0
              ? Math.min(100, Math.round((k.replicaCount / replicationN) * 100))
              : 0;
            const mini =
              k.replicaCount >= replicationN
                ? 'bg-green-600'
                : k.replicaCount > 0
                ? 'bg-yellow-600'
                : 'bg-red-600';
            return (
              <div key={k.id} className="flex items-center gap-2">
                <span className="font-mono text-slate-500 text-xs w-28 shrink-0 truncate">
                  {shortId(k.id, 10)}
                </span>
                <div className="flex-1 h-1.5 bg-slate-800 rounded-full overflow-hidden">
                  <div
                    className={`h-full rounded-full transition-all duration-300 ${mini}`}
                    style={{ width: `${pct}%` }}
                  />
                </div>
                <span className="text-slate-500 text-xs font-mono w-10 text-right shrink-0">
                  {k.replicaCount}/{replicationN}
                </span>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

// ─── Main Component ───────────────────────────────────────────────────────────

export default function ConsistencyDash() {
  const keyMap = useDHTStore((s: DHTStore) => s.keys);
  const config = useDHTStore((s: DHTStore) => s.config);
  const events = useDHTStore((s: DHTStore) => s.events);
  const nodes = useDHTStore((s: DHTStore) => s.nodes);
  const keys = React.useMemo(() => Array.from(keyMap.values()), [keyMap]);

  const replicationN = config?.replicationN ?? 3;

  // ── Expand state ────────────────────────────────────────────────────────────
  const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set());
  const [deletingKey, setDeletingKey] = useState<string | null>(null);
  const [replicaCache, setReplicaCache] = useState<ReplicaCache>({});

  const toggleRow = (keyId: string) => {
    setExpandedRows((prev) => {
      const next = new Set(prev);
      if (next.has(keyId)) next.delete(keyId);
      else next.add(keyId);
      return next;
    });
  };

  const loadReplicas = useCallback(async (keyId: string, keyStr: string) => {
    setReplicaCache((prev) => ({
      ...prev,
      [keyId]: { loading: true, error: null, replicas: [] },
    }));
    try {
      const data = await getKeyReplicas(keyStr || keyId);
      setReplicaCache((prev) => ({
        ...prev,
        [keyId]: { loading: false, error: null, replicas: data.replicas },
      }));
    } catch (err) {
      setReplicaCache((prev) => ({
        ...prev,
        [keyId]: {
          loading: false,
          error: err instanceof Error ? err.message : 'Failed to load replicas',
          replicas: [],
        },
      }));
    }
  }, []);

  const handleDeleteKey = async (keyStr: string, keyId: string) => {
    setDeletingKey(keyId);
    try {
      await deleteKey(keyStr || keyId);
    } catch {
      // Key may not exist; ignore
    } finally {
      setDeletingKey(null);
    }
  };

  // Build a lightweight nodeMap for address lookups
  const nodeMap = new Map(
    Array.from(nodes.values()).map((n) => [n.id, { addr: n.addr }])
  );

  // ── Conflict events ──────────────────────────────────────────────────────────
  const conflictEvents: ConflictEvent[] = events
    .filter((e) => e.type === 'conflict_detected')
    .map((e) => {
      const p = e.payload as { key?: string; nodeId?: string; strategy?: string };
      return {
        timestamp: e.timestamp,
        key: p?.key ?? '—',
        nodeId: p?.nodeId ?? '—',
        strategy: p?.strategy ?? 'lww',
      };
    });

  // ── Consistency events (existing) ────────────────────────────────────────────
  const consistencyEvents = events.filter((e) =>
    ['write_quorum', 'read_quorum', 'conflict_detected', 'gossip_sync'].includes(e.type)
  );

  return (
    <div className="p-6 max-w-5xl mx-auto">
      <h2 className="text-slate-200 font-semibold mb-4">Consistency Dashboard</h2>

      {/* Quorum config cards */}
      <div className="grid grid-cols-3 gap-4 mb-6">
        <div className="bg-slate-900 rounded border border-slate-800 p-4 text-center">
          <p className="text-2xl font-bold text-slate-200">{config?.writeQuorum ?? 2}</p>
          <p className="text-xs text-slate-500">Write Quorum (W)</p>
        </div>
        <div className="bg-slate-900 rounded border border-slate-800 p-4 text-center">
          <p className="text-2xl font-bold text-slate-200">{config?.readQuorum ?? 2}</p>
          <p className="text-xs text-slate-500">Read Quorum (R)</p>
        </div>
        <div className="bg-slate-900 rounded border border-slate-800 p-4 text-center">
          <p className="text-2xl font-bold text-slate-200">{replicationN}</p>
          <p className="text-xs text-slate-500">Replication Factor (N)</p>
        </div>
      </div>

      {/* Convergence meter */}
      <ConvergenceMeter keys={keys} replicationN={replicationN} />

      {/* Key replication table with expandable rows */}
      <div className="bg-slate-900 rounded border border-slate-800 mb-4">
        <div className="px-4 py-3 border-b border-slate-800 flex items-center justify-between">
          <p className="text-sm text-slate-300">Key Replication Status</p>
          <span className="text-xs text-slate-600">Click a row to inspect replicas</span>
        </div>

        {keys.length === 0 ? (
          <p className="p-4 text-slate-500 text-sm">No keys stored yet.</p>
        ) : (
          <table className="w-full text-xs">
            <thead>
              <tr className="text-slate-500 border-b border-slate-800">
                <th className="text-left px-4 py-2 w-6" />
                <th className="text-left px-4 py-2">Key ID</th>
                <th className="text-left py-2">Replicas</th>
                <th className="text-left py-2">Status</th>
                <th className="py-2" />
              </tr>
            </thead>
            <tbody>
              {keys.map((k) => {
                const isExpanded = expandedRows.has(k.id);
                return (
                  <React.Fragment key={k.id}>
                    <tr
                      onClick={() => toggleRow(k.id)}
                      className="border-b border-slate-800/50 hover:bg-slate-800/30 cursor-pointer select-none"
                    >
                      {/* Chevron */}
                      <td className="px-4 py-2 text-slate-600 w-6">
                        <svg
                          xmlns="http://www.w3.org/2000/svg"
                          viewBox="0 0 16 16"
                          fill="currentColor"
                          className={`w-3 h-3 transition-transform duration-200 ${isExpanded ? 'rotate-90' : ''}`}
                        >
                          <path
                            fillRule="evenodd"
                            d="M6.22 4.22a.75.75 0 0 1 1.06 0l3.25 3.25a.75.75 0 0 1 0 1.06l-3.25 3.25a.75.75 0 0 1-1.06-1.06L9.19 8 6.22 5.03a.75.75 0 0 1 0-1.06Z"
                            clipRule="evenodd"
                          />
                        </svg>
                      </td>
                      <td className="px-4 py-2 font-mono text-slate-400">
                        {shortId(k.id, 12)}
                      </td>
                      <td className="py-2 text-slate-300">
                        {k.replicaCount}/{replicationN}
                      </td>
                      <td className="py-2">
                        <span
                          className={`px-1.5 py-0.5 rounded text-xs ${
                            k.replicaCount >= replicationN
                              ? 'bg-green-900 text-green-300'
                              : k.replicaCount > 0
                              ? 'bg-yellow-900 text-yellow-300'
                              : 'bg-red-900 text-red-300'
                          }`}
                        >
                          {k.replicaCount >= replicationN
                            ? 'Healthy'
                            : k.replicaCount > 0
                            ? 'Under-replicated'
                            : 'Lost'}
                        </span>
                      </td>
                      <td className="py-2 pr-3" onClick={(e) => e.stopPropagation()}>
                        <button
                          onClick={() => handleDeleteKey(k.key, k.id)}
                          disabled={deletingKey === k.id}
                          className="text-red-600 hover:text-red-400 text-xs px-1.5 py-0.5 rounded hover:bg-red-900/30 transition-colors disabled:opacity-40"
                          title="Delete key from all replicas"
                        >
                          {deletingKey === k.id ? '...' : 'Del'}
                        </button>
                      </td>
                    </tr>

                    {isExpanded && (
                      <ExpandedReplicaSection
                        keyId={k.id}
                        keyStr={k.key}
                        cache={replicaCache}
                        nodeMap={nodeMap}
                        onLoad={loadReplicas}
                      />
                    )}
                  </React.Fragment>
                );
              })}
            </tbody>
          </table>
        )}
      </div>

      {/* Conflict log */}
      <ConflictLog conflicts={conflictEvents} />

      {/* Recent consistency events (existing) */}
      <div className="bg-slate-900 rounded border border-slate-800">
        <div className="px-4 py-3 border-b border-slate-800">
          <p className="text-sm text-slate-300">Recent Events</p>
        </div>
        <div className="max-h-48 overflow-y-auto">
          {consistencyEvents.slice(0, 20).map((ev, i) => (
            <div
              key={i}
              className="px-4 py-2 text-xs border-b border-slate-800/50 font-mono flex gap-3"
            >
              <span className="text-slate-600 shrink-0">
                {new Date(ev.timestamp).toLocaleTimeString()}
              </span>
              <span
                className={`shrink-0 ${
                  ev.type === 'conflict_detected'
                    ? 'text-red-400'
                    : ev.type === 'gossip_sync'
                    ? 'text-green-400'
                    : 'text-blue-400'
                }`}
              >
                {ev.type}
              </span>
            </div>
          ))}
          {consistencyEvents.length === 0 && (
            <p className="px-4 py-3 text-slate-500 text-sm">No consistency events yet.</p>
          )}
        </div>
      </div>
    </div>
  );
}

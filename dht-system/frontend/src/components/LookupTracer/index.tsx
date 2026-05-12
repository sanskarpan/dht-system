import { useState, useCallback, useEffect, useRef } from 'react';
import * as d3 from 'd3';
import * as api from '@/api/client';
import type { LookupTrace, Protocol, HopEvent } from '@/types/dht';
import { useDHTStore } from '@/store/dhtStore';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface HistoryEntry {
  id: string;                // unique id for React key
  key: string;               // original key string
  keyTruncated: string;      // key truncated to 20 chars for display
  totalHops: number;
  latencyMs: number;
  timestamp: Date;
  protocol: Protocol;
  trace: LookupTrace;        // full trace for restore
}

// Comparison row: a key that has been run under both protocols
interface ComparisonRow {
  key: string;
  chordHops: number | null;
  chordLatency: number | null;
  kademliaHops: number | null;
  kademliaLatency: number | null;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const MAX_HISTORY = 20;

function relativeTime(date: Date): string {
  const diffMs = Date.now() - date.getTime();
  const diffSec = Math.floor(diffMs / 1000);
  if (diffSec < 60) return `${diffSec}s ago`;
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHr = Math.floor(diffMin / 60);
  return `${diffHr}h ago`;
}

function hopBadgeClass(hops: number): string {
  if (hops <= 3) return 'bg-green-800 text-green-200';
  if (hops <= 6) return 'bg-yellow-800 text-yellow-200';
  return 'bg-red-800 text-red-200';
}

function truncateKey(k: string, maxLen = 20): string {
  return k.length > maxLen ? k.slice(0, maxLen) + '...' : k;
}

function buildComparisonRows(history: HistoryEntry[]): ComparisonRow[] {
  // Collect latest entry per (key, protocol) pair
  const map = new Map<string, { chord?: HistoryEntry; kademlia?: HistoryEntry }>();
  // Iterate oldest-first so newest overwrites
  for (const entry of [...history].reverse()) {
    const existing = map.get(entry.key) ?? {};
    if (entry.protocol === 'chord') existing.chord = entry;
    else existing.kademlia = entry;
    map.set(entry.key, existing);
  }

  const rows: ComparisonRow[] = [];
  map.forEach((val, key) => {
    // Only include keys that appear under at least one protocol
    rows.push({
      key,
      chordHops: val.chord?.totalHops ?? null,
      chordLatency: val.chord?.latencyMs ?? null,
      kademliaHops: val.kademlia?.totalHops ?? null,
      kademliaLatency: val.kademlia?.latencyMs ?? null,
    });
  });

  // Sort: keys with both protocols first, then alphabetical
  rows.sort((a, b) => {
    const aBoth = a.chordHops !== null && a.kademliaHops !== null ? 0 : 1;
    const bBoth = b.chordHops !== null && b.kademliaHops !== null ? 0 : 1;
    if (aBoth !== bBoth) return aBoth - bBoth;
    return a.key.localeCompare(b.key);
  });

  return rows;
}

function winner(row: ComparisonRow): string {
  if (row.chordHops === null || row.kademliaHops === null) return '-';
  if (row.chordHops < row.kademliaHops) return 'Chord';
  if (row.kademliaHops < row.chordHops) return 'Kademlia';
  return 'Tie';
}

// ---------------------------------------------------------------------------
// Mini ring diagram sub-component
// ---------------------------------------------------------------------------

function hexToAngle(hexId: string): number {
  const partial = hexId.slice(0, 13);
  const value = BigInt('0x' + partial);
  const max = BigInt('0x' + 'f'.repeat(13));
  const fraction = Number(value) / Number(max);
  return fraction * 2 * Math.PI - Math.PI / 2;
}

interface MiniRingDiagramProps {
  hops: HopEvent[];
  keyHash: string;
}

function MiniRingDiagram({ hops, keyHash }: MiniRingDiagramProps) {
  const svgRef = useRef<SVGSVGElement>(null);
  const W = 280;
  const H = 220;
  const cx = W / 2;
  const cy = H / 2;
  const R = Math.min(W, H) * 0.36;

  useEffect(() => {
    if (!svgRef.current || hops.length === 0) return;
    const svg = d3.select(svgRef.current);
    svg.selectAll('*').remove();

    // Collect unique node IDs involved in the lookup
    const nodeIds = Array.from(new Set(hops.flatMap((h) => [h.fromNode, h.toNode])));

    // Positions on the ring
    const pos = (id: string): [number, number] => {
      const a = hexToAngle(id);
      return [cx + R * Math.cos(a), cy + R * Math.sin(a)];
    };

    // Background
    svg.append('rect').attr('width', W).attr('height', H).attr('fill', '#0d1117');

    // Full ring (faint)
    svg.append('circle')
      .attr('cx', cx).attr('cy', cy).attr('r', R)
      .attr('fill', 'none').attr('stroke', '#1e293b').attr('stroke-width', 1.5);

    // Key dot position
    const keyAngle = hexToAngle(keyHash);
    const [kx, ky] = [cx + R * Math.cos(keyAngle), cy + R * Math.sin(keyAngle)];
    svg.append('circle')
      .attr('cx', kx).attr('cy', ky).attr('r', 4)
      .attr('fill', '#6366f1').attr('stroke', '#818cf8').attr('stroke-width', 1);
    svg.append('text')
      .attr('x', kx).attr('y', ky - 7).attr('text-anchor', 'middle')
      .attr('font-size', 7).attr('fill', '#818cf8').attr('font-family', 'monospace')
      .text('key');

    // Hop lines with sequential animation
    hops.forEach((hop, i) => {
      const [fx, fy] = pos(hop.fromNode);
      const [tx, ty] = pos(hop.toNode);

      const line = svg.append('line')
        .attr('x1', fx).attr('y1', fy).attr('x2', tx).attr('y2', ty)
        .attr('stroke', '#f59e0b').attr('stroke-width', 1.5)
        .attr('stroke-dasharray', '3 2').attr('opacity', 0);

      line.transition().delay(i * 250).duration(200).attr('opacity', 0.6);

      // Arrow marker via a small triangle at the end
      const pulse = svg.append('circle')
        .attr('cx', fx).attr('cy', fy).attr('r', 4)
        .attr('fill', '#fbbf24').attr('opacity', 0);

      pulse.transition()
        .delay(i * 250).duration(0).attr('opacity', 1)
        .transition().duration(300).ease(d3.easeCubicOut)
        .attr('cx', tx).attr('cy', ty)
        .transition().duration(200).attr('r', 7).attr('opacity', 0);
    });

    // Node circles on ring for involved nodes
    nodeIds.forEach((id, i) => {
      const [nx, ny] = pos(id);
      const isLast = id === hops[hops.length - 1].toNode;
      const isFirst = id === hops[0].fromNode;

      svg.append('circle')
        .attr('cx', nx).attr('cy', ny).attr('r', isLast ? 7 : 5)
        .attr('fill', isLast ? '#22c55e' : isFirst ? '#3b82f6' : '#475569')
        .attr('stroke', '#0d1117').attr('stroke-width', 1)
        .attr('opacity', 0)
        .transition().delay(i * 30).duration(200).attr('opacity', 1);

      svg.append('text')
        .attr('x', nx).attr('y', ny + 14).attr('text-anchor', 'middle')
        .attr('font-size', 6).attr('fill', '#94a3b8').attr('font-family', 'monospace')
        .text(id.slice(0, 6));
    });

    // Hop count badge
    svg.append('text')
      .attr('x', W - 8).attr('y', 16).attr('text-anchor', 'end')
      .attr('font-size', 9).attr('fill', '#f59e0b').attr('font-family', 'monospace')
      .text(`${hops.length} hop${hops.length !== 1 ? 's' : ''}`);

    // Legend
    const legend = [
      { color: '#3b82f6', label: 'origin' },
      { color: '#22c55e', label: 'target' },
      { color: '#6366f1', label: 'key' },
    ];
    legend.forEach((l, i) => {
      svg.append('circle').attr('cx', 10).attr('cy', H - 16 + i * 12 - (legend.length - 1) * 6).attr('r', 3).attr('fill', l.color);
      svg.append('text').attr('x', 17).attr('y', H - 13 + i * 12 - (legend.length - 1) * 6)
        .attr('font-size', 7).attr('fill', '#64748b').text(l.label);
    });

  }, [hops, keyHash, W, H, cx, cy, R]);

  if (hops.length === 0) return null;

  return (
    <div className="mt-4 border-t border-slate-800 pt-4">
      <p className="text-xs text-slate-500 uppercase tracking-wider mb-2">Lookup Path</p>
      <svg ref={svgRef} width={W} height={H} className="rounded bg-[#0d1117]" />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export default function LookupTracer() {
  const protocol = useDHTStore((s) => s.protocol);

  const [key, setKey] = useState('');
  const [trace, setTrace] = useState<LookupTrace | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Feature 1: lookup history
  const [history, setHistory] = useState<HistoryEntry[]>([]);

  // Feature 2: compare panel toggle
  const [compareOpen, setCompareOpen] = useState(false);

  // ---------------------------------------------------------------------------
  // Lookup handler
  // ---------------------------------------------------------------------------

  const handleTrace = useCallback(async () => {
    if (!key) return;
    setLoading(true);
    setError(null);
    try {
      const result = await api.traceLookup(key);
      setTrace(result);

      // Prepend to history (max 20)
      const entry: HistoryEntry = {
        id: `${Date.now()}-${Math.random()}`,
        key: result.key,
        keyTruncated: truncateKey(result.key),
        totalHops: result.totalHops,
        latencyMs: result.latencyMs,
        timestamp: new Date(),
        protocol,
        trace: result,
      };
      setHistory((prev) => [entry, ...prev].slice(0, MAX_HISTORY));
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Lookup failed');
    } finally {
      setLoading(false);
    }
  }, [key, protocol]);

  // Restore a history entry as the current trace
  const restoreEntry = (entry: HistoryEntry) => {
    setTrace(entry.trace);
    setKey(entry.key);
  };

  // ---------------------------------------------------------------------------
  // Hash preview
  // ---------------------------------------------------------------------------

  const keyHash = key
    ? key.split('').reduce((h, c) => ((h << 5) - h + c.charCodeAt(0)) | 0, 0).toString(16).padStart(8, '0')
    : '';

  // ---------------------------------------------------------------------------
  // Comparison data
  // ---------------------------------------------------------------------------

  const comparisonRows = buildComparisonRows(history);

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

  return (
    <div className="p-6 max-w-3xl mx-auto space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-slate-200 font-semibold">Lookup Tracer</h2>
        <button
          onClick={() => setCompareOpen((v) => !v)}
          className={`px-3 py-1.5 text-xs rounded font-medium border transition-colors ${
            compareOpen
              ? 'bg-indigo-700 border-indigo-500 text-white'
              : 'bg-slate-800 border-slate-600 text-slate-300 hover:bg-slate-700'
          }`}
        >
          {compareOpen ? 'Hide Compare' : 'Compare Protocols'}
        </button>
      </div>

      {/* ------------------------------------------------------------------ */}
      {/* Input row                                                           */}
      {/* ------------------------------------------------------------------ */}
      <div>
        <div className="flex gap-3 mb-2">
          <input
            value={key}
            onChange={(e) => setKey(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleTrace()}
            placeholder="Enter key to trace..."
            className="flex-1 bg-slate-800 border border-slate-700 text-slate-200 text-sm rounded px-3 py-2 outline-none focus:border-blue-500"
          />
          <button
            onClick={handleTrace}
            disabled={!key || loading}
            className="px-4 py-2 bg-purple-700 hover:bg-purple-600 text-white text-sm rounded font-medium disabled:opacity-50"
          >
            {loading ? 'Tracing...' : 'Trace Lookup'}
          </button>
        </div>

        <div className="flex items-center gap-3">
          {key && (
            <p className="text-xs text-slate-500 font-mono">
              SHA-1 preview: <span className="text-slate-400">{keyHash}...</span>
            </p>
          )}
          <p className="text-xs text-slate-600 ml-auto">
            Protocol: <span className="text-slate-400 capitalize">{protocol}</span>
          </p>
        </div>
      </div>

      {error && (
        <div className="bg-red-900/30 border border-red-800 rounded p-3 text-red-300 text-sm">{error}</div>
      )}

      {/* ------------------------------------------------------------------ */}
      {/* Current trace result                                                */}
      {/* ------------------------------------------------------------------ */}
      {trace && (
        <div className="bg-slate-900 rounded border border-slate-800 p-4">
          <div className="grid grid-cols-3 gap-4 mb-4 text-center">
            <div>
              <p className="text-2xl font-bold text-slate-200">{trace.totalHops}</p>
              <p className="text-xs text-slate-500">Hops</p>
            </div>
            <div>
              <p className="text-2xl font-bold text-slate-200">{trace.latencyMs}ms</p>
              <p className="text-xs text-slate-500">Latency</p>
            </div>
            <div>
              <p className="text-sm font-mono text-slate-200 truncate">{trace.targetNode.slice(0, 10)}...</p>
              <p className="text-xs text-slate-500">Target Node</p>
            </div>
          </div>

          <div className="border-t border-slate-800 pt-4">
            <p className="text-xs text-slate-500 uppercase tracking-wider mb-3">Hop Trace</p>
            {trace.hops.length === 0 ? (
              <p className="text-slate-400 text-sm">Key found locally (0 hops)</p>
            ) : (
              <div className="space-y-2">
                {trace.hops.map((hop, i) => (
                  <div key={i} className="flex items-center gap-3 text-xs font-mono">
                    <span className="text-slate-600 w-4">{i + 1}</span>
                    <span className="text-slate-400">{hop.fromNode.slice(0, 8)}</span>
                    <span className="text-slate-600">→</span>
                    <span className="text-blue-400">{hop.toNode.slice(0, 8)}</span>
                    <span className="text-slate-500 ml-auto">{hop.mechanism}</span>
                  </div>
                ))}
              </div>
            )}
          </div>

          <MiniRingDiagram hops={trace.hops} keyHash={trace.keyHash} />

          <div className="border-t border-slate-800 pt-3 mt-3">
            <p className="text-xs font-mono text-slate-500">
              Key hash: <span className="text-slate-400">{trace.keyHash.slice(0, 16)}...</span>
            </p>
          </div>
        </div>
      )}

      {/* ------------------------------------------------------------------ */}
      {/* Feature 1: History panel                                            */}
      {/* ------------------------------------------------------------------ */}
      {history.length > 0 && (
        <div className="bg-slate-900 rounded border border-slate-800">
          <div className="flex items-center justify-between px-4 py-2 border-b border-slate-800">
            <p className="text-xs text-slate-400 uppercase tracking-wider font-medium">
              History
            </p>
            <p className="text-xs text-slate-600">{history.length} / {MAX_HISTORY}</p>
          </div>
          <div className="divide-y divide-slate-800">
            {history.map((entry) => (
              <button
                key={entry.id}
                onClick={() => restoreEntry(entry)}
                className="w-full flex items-center gap-3 px-4 py-2.5 text-left hover:bg-slate-800/60 transition-colors group"
              >
                {/* Timestamp */}
                <span className="text-xs text-slate-600 w-16 shrink-0 tabular-nums">
                  {relativeTime(entry.timestamp)}
                </span>

                {/* Protocol badge */}
                <span
                  className={`text-xs px-1.5 py-0.5 rounded font-mono shrink-0 ${
                    entry.protocol === 'chord'
                      ? 'bg-purple-900/60 text-purple-300'
                      : 'bg-blue-900/60 text-blue-300'
                  }`}
                >
                  {entry.protocol === 'chord' ? 'CHD' : 'KAD'}
                </span>

                {/* Key */}
                <span className="text-xs font-mono text-slate-400 flex-1 truncate group-hover:text-slate-200 transition-colors">
                  {entry.keyTruncated}
                </span>

                {/* Hop count badge */}
                <span
                  className={`text-xs px-2 py-0.5 rounded font-medium shrink-0 ${hopBadgeClass(entry.totalHops)}`}
                >
                  {entry.totalHops} hop{entry.totalHops !== 1 ? 's' : ''}
                </span>

                {/* Latency */}
                <span className="text-xs text-slate-500 w-14 text-right shrink-0 tabular-nums">
                  {entry.latencyMs}ms
                </span>
              </button>
            ))}
          </div>
        </div>
      )}

      {/* ------------------------------------------------------------------ */}
      {/* Feature 2: Compare protocols panel                                  */}
      {/* ------------------------------------------------------------------ */}
      {compareOpen && (
        <div className="bg-slate-900 rounded border border-slate-800">
          <div className="px-4 py-3 border-b border-slate-800">
            <p className="text-xs text-slate-400 uppercase tracking-wider font-medium mb-1">
              Protocol Comparison
            </p>
            <p className="text-xs text-slate-600 leading-relaxed">
              Run the same key under both Chord and Kademlia (switch backend protocol between lookups).
              Rows with data from both protocols show a winner. Configure backend protocol in the
              settings panel, then run the same key again.
            </p>
          </div>

          {comparisonRows.length === 0 ? (
            <div className="px-4 py-6 text-center text-slate-600 text-sm">
              No lookup history yet. Run some lookups to populate this table.
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead>
                  <tr className="border-b border-slate-800 text-slate-500 uppercase tracking-wider">
                    <th className="text-left px-4 py-2 font-medium">Key</th>
                    <th className="text-center px-3 py-2 font-medium">
                      <span className="text-purple-400">Chord</span> hops
                    </th>
                    <th className="text-center px-3 py-2 font-medium">
                      <span className="text-purple-400">Chord</span> ms
                    </th>
                    <th className="text-center px-3 py-2 font-medium">
                      <span className="text-blue-400">Kademlia</span> hops
                    </th>
                    <th className="text-center px-3 py-2 font-medium">
                      <span className="text-blue-400">Kademlia</span> ms
                    </th>
                    <th className="text-center px-3 py-2 font-medium">Winner</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-800/60">
                  {comparisonRows.map((row) => {
                    const w = winner(row);
                    const hasBoth = row.chordHops !== null && row.kademliaHops !== null;
                    return (
                      <tr
                        key={row.key}
                        className={`transition-colors ${hasBoth ? 'hover:bg-slate-800/40' : 'opacity-60'}`}
                      >
                        <td className="px-4 py-2 font-mono text-slate-400 max-w-[140px] truncate">
                          {truncateKey(row.key)}
                        </td>

                        {/* Chord hops */}
                        <td className="px-3 py-2 text-center">
                          {row.chordHops !== null ? (
                            <span className={`px-1.5 py-0.5 rounded ${hopBadgeClass(row.chordHops)}`}>
                              {row.chordHops}
                            </span>
                          ) : (
                            <span className="text-slate-700">—</span>
                          )}
                        </td>

                        {/* Chord latency */}
                        <td className="px-3 py-2 text-center text-slate-500 tabular-nums">
                          {row.chordLatency !== null ? `${row.chordLatency}` : <span className="text-slate-700">—</span>}
                        </td>

                        {/* Kademlia hops */}
                        <td className="px-3 py-2 text-center">
                          {row.kademliaHops !== null ? (
                            <span className={`px-1.5 py-0.5 rounded ${hopBadgeClass(row.kademliaHops)}`}>
                              {row.kademliaHops}
                            </span>
                          ) : (
                            <span className="text-slate-700">—</span>
                          )}
                        </td>

                        {/* Kademlia latency */}
                        <td className="px-3 py-2 text-center text-slate-500 tabular-nums">
                          {row.kademliaLatency !== null ? `${row.kademliaLatency}` : <span className="text-slate-700">—</span>}
                        </td>

                        {/* Winner */}
                        <td className="px-3 py-2 text-center">
                          {hasBoth ? (
                            <span
                              className={`font-medium ${
                                w === 'Chord'
                                  ? 'text-purple-400'
                                  : w === 'Kademlia'
                                  ? 'text-blue-400'
                                  : 'text-slate-400'
                              }`}
                            >
                              {w}
                            </span>
                          ) : (
                            <span className="text-slate-700 text-xs italic">needs both</span>
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}

          {/* Legend */}
          <div className="px-4 py-2 border-t border-slate-800 flex items-center gap-4 text-xs text-slate-600">
            <span>
              <span className="inline-block w-2 h-2 rounded-full bg-green-600 mr-1" />
              {'\u22643'} hops
            </span>
            <span>
              <span className="inline-block w-2 h-2 rounded-full bg-yellow-600 mr-1" />
              4-6 hops
            </span>
            <span>
              <span className="inline-block w-2 h-2 rounded-full bg-red-600 mr-1" />
              {'>'}6 hops
            </span>
            <span className="ml-auto italic">Winner = fewer hops</span>
          </div>
        </div>
      )}
    </div>
  );
}

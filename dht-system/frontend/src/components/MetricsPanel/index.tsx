import { useDHTStore } from '@/store/dhtStore';
import type { DHTStore } from '@/store/dhtStore';
import {
  LineChart,
  Line,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  Cell,
} from 'recharts';
import { useEffect, useState } from 'react';

interface HopDataPoint {
  time: string;
  hops: number;
}

interface HopBucket {
  label: string;
  count: number;
}

interface QuorumStats {
  writeCount: number;
  readCount: number;
  avgLagMs: number | null;
}

// Build histogram buckets from an array of hop counts
function buildHopHistogram(hopCounts: number[]): HopBucket[] {
  const buckets: HopBucket[] = [
    { label: '1', count: 0 },
    { label: '2', count: 0 },
    { label: '3', count: 0 },
    { label: '4', count: 0 },
    { label: '5', count: 0 },
    { label: '6', count: 0 },
    { label: '7', count: 0 },
    { label: '8+', count: 0 },
  ];

  for (const h of hopCounts) {
    if (h <= 0) continue;
    if (h >= 8) {
      buckets[7].count += 1;
    } else {
      buckets[h - 1].count += 1;
    }
  }

  return buckets;
}

export default function MetricsPanel() {
  const nodes = useDHTStore((s: DHTStore) => s.nodes);
  const keys = useDHTStore((s: DHTStore) => s.keys);
  const events = useDHTStore((s: DHTStore) => s.events);

  const [hopHistory, setHopHistory] = useState<HopDataPoint[]>([]);
  const [hopHistogram, setHopHistogram] = useState<HopBucket[]>(buildHopHistogram([]));
  const [stabilizeRate, setStabilizeRate] = useState<number>(0);
  const [quorumStats, setQuorumStats] = useState<QuorumStats>({ writeCount: 0, readCount: 0, avgLagMs: null });

  useEffect(() => {
    const now = Date.now();
    const tenSecondsAgo = now - 10_000;

    // --- Hop history (line chart, recent 20) ---
    const lookupEvents = events
      .filter(e => e.type === 'lookup_complete')
      .slice(0, 20)
      .map((e) => {
        const p = e.payload as { totalHops?: number };
        return {
          time: new Date(e.timestamp).toLocaleTimeString(),
          hops: p.totalHops ?? 0,
        };
      })
      .reverse();
    setHopHistory(lookupEvents);

    // --- Hop histogram (all lookup_complete events in store) ---
    const allHopCounts = events
      .filter(e => e.type === 'lookup_complete')
      .map((e) => {
        const p = e.payload as { totalHops?: number };
        return p.totalHops ?? 0;
      });
    setHopHistogram(buildHopHistogram(allHopCounts));

    // --- Stabilization rate: count stabilize events in last 10s ---
    const recentStabilize = events.filter(
      e => e.type === 'stabilize' && new Date(e.timestamp).getTime() >= tenSecondsAgo
    );
    setStabilizeRate(parseFloat((recentStabilize.length / 10).toFixed(1)));

    // --- Quorum event counts + replication lag ---
    const recentWrite = events.filter(
      e => e.type === 'write_quorum' && new Date(e.timestamp).getTime() >= tenSecondsAgo
    );
    const recentRead = events.filter(
      e => e.type === 'read_quorum' && new Date(e.timestamp).getTime() >= tenSecondsAgo
    );

    // Compute avg replication lag from replication_lag events in last 60s
    const lagEvents = events.filter(
      e => e.type === 'replication_lag' && new Date(e.timestamp).getTime() >= now - 60_000
    );
    let avgLagMs: number | null = null;
    if (lagEvents.length > 0) {
      const totalLag = lagEvents.reduce((sum, e) => {
        const p = e.payload as { lagMs?: number };
        return sum + (p.lagMs ?? 0);
      }, 0);
      avgLagMs = Math.round(totalLag / lagEvents.length);
    }

    setQuorumStats({ writeCount: recentWrite.length, readCount: recentRead.length, avgLagMs });
  }, [events]);

  const nodeCount = nodes.size;
  const keyCount = keys.size;
  const logN = nodeCount > 0 ? Math.ceil(Math.log2(nodeCount)) : 0;

  const totalLookups = events.filter(e => e.type === 'lookup_complete').length;

  return (
    <div className="p-6 max-w-4xl mx-auto space-y-6">
      <h2 className="text-slate-200 font-semibold">Performance Metrics</h2>

      {/* Top stat cards */}
      <div className="grid grid-cols-4 gap-4">
        <div className="bg-slate-900 rounded border border-slate-800 p-4 text-center">
          <p className="text-2xl font-bold text-slate-200">{nodeCount}</p>
          <p className="text-xs text-slate-500">Nodes</p>
        </div>
        <div className="bg-slate-900 rounded border border-slate-800 p-4 text-center">
          <p className="text-2xl font-bold text-slate-200">{keyCount}</p>
          <p className="text-xs text-slate-500">Keys</p>
        </div>
        <div className="bg-slate-900 rounded border border-slate-800 p-4 text-center">
          <p className="text-2xl font-bold text-slate-200">O(log {nodeCount})</p>
          <p className="text-xs text-slate-500">Expected Hops</p>
        </div>
        <div className="bg-slate-900 rounded border border-slate-800 p-4 text-center">
          <p className="text-2xl font-bold text-slate-200">{logN}</p>
          <p className="text-xs text-slate-500">&#8968;log&#8322;(N)&#8969;</p>
        </div>
      </div>

      {/* Secondary metric cards: stabilize rate + replication lag */}
      <div className="grid grid-cols-2 gap-4">
        {/* Stabilization cycles/s gauge card */}
        <div className="bg-slate-900 rounded border border-slate-800 p-4">
          <p className="text-xs text-slate-500 uppercase tracking-wider mb-2">Stabilize Rate</p>
          <div className="flex items-end gap-2">
            <span className="text-3xl font-bold text-emerald-400">{stabilizeRate}</span>
            <span className="text-sm text-slate-400 pb-1">cycles / s</span>
          </div>
          <p className="text-xs text-slate-500 mt-2">
            Counted <span className="text-slate-300">{Math.round(stabilizeRate * 10)}</span> stabilize
            events in the last 10 seconds
          </p>
          {/* Visual bar representing rate (capped at 20 cycles/s) */}
          <div className="mt-3 bg-slate-800 rounded-full h-2 overflow-hidden">
            <div
              className="h-full bg-emerald-500 rounded-full transition-all duration-500"
              style={{ width: `${Math.min(100, (stabilizeRate / 20) * 100)}%` }}
            />
          </div>
          <div className="flex justify-between text-xs text-slate-600 mt-1">
            <span>0</span>
            <span>20/s</span>
          </div>
        </div>

        {/* Replication lag / quorum activity card */}
        <div className="bg-slate-900 rounded border border-slate-800 p-4">
          <p className="text-xs text-slate-500 uppercase tracking-wider mb-2">Replication Activity</p>
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-xs text-slate-400">Write Quorum Events</span>
              <span className="text-sm font-semibold text-blue-400">{quorumStats.writeCount}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-xs text-slate-400">Read Quorum Events</span>
              <span className="text-sm font-semibold text-purple-400">{quorumStats.readCount}</span>
            </div>
            <div className="border-t border-slate-800 pt-2">
              <p className="text-xs text-slate-500">Avg Replication Lag (60s)</p>
              {quorumStats.avgLagMs !== null ? (
                <p className="text-sm font-semibold text-amber-400 font-mono mt-0.5">
                  {quorumStats.avgLagMs} ms
                </p>
              ) : (
                <p className="text-xs text-slate-600 font-mono mt-0.5">
                  — no writes yet
                </p>
              )}
            </div>
          </div>
          <p className="text-xs text-slate-600 mt-2">Counts from last 10 seconds</p>
        </div>
      </div>

      {/* Hop count line chart (recent lookups over time) */}
      <div className="bg-slate-900 rounded border border-slate-800 p-4">
        <p className="text-sm text-slate-300 mb-3">
          Lookup Hop Count &mdash; Recent
          {totalLookups > 0 && (
            <span className="text-slate-500 text-xs ml-2">({totalLookups} total)</span>
          )}
        </p>
        {hopHistory.length > 0 ? (
          <ResponsiveContainer width="100%" height={160}>
            <LineChart data={hopHistory}>
              <XAxis dataKey="time" tick={{ fill: '#64748b', fontSize: 10 }} />
              <YAxis tick={{ fill: '#64748b', fontSize: 10 }} allowDecimals={false} />
              <Tooltip
                contentStyle={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0' }}
              />
              <Line type="monotone" dataKey="hops" stroke="#3b82f6" strokeWidth={2} dot={false} />
            </LineChart>
          </ResponsiveContainer>
        ) : (
          <p className="text-slate-500 text-sm text-center py-8">
            Perform lookups to see hop count data
          </p>
        )}
      </div>

      {/* Hop count histogram */}
      <div className="bg-slate-900 rounded border border-slate-800 p-4">
        <p className="text-sm text-slate-300 mb-1">Hop Count Distribution</p>
        <p className="text-xs text-slate-500 mb-3">
          Number of lookups per hop-count bucket (across all tracked events)
        </p>
        {totalLookups > 0 ? (
          <ResponsiveContainer width="100%" height={180}>
            <BarChart data={hopHistogram} barCategoryGap="20%">
              <XAxis
                dataKey="label"
                tick={{ fill: '#64748b', fontSize: 11 }}
                label={{ value: 'Hops', position: 'insideBottom', offset: -2, fill: '#64748b', fontSize: 11 }}
              />
              <YAxis
                tick={{ fill: '#64748b', fontSize: 10 }}
                allowDecimals={false}
                label={{ value: 'Count', angle: -90, position: 'insideLeft', fill: '#64748b', fontSize: 11 }}
              />
              <Tooltip
                contentStyle={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0' }}
                formatter={(value) => [value ?? 0, 'Lookups']}
              />
              <Bar dataKey="count" radius={[3, 3, 0, 0]}>
                {hopHistogram.map((_entry, index) => {
                  // Color gradient: green for low hops, amber for mid, red for high
                  const colors = [
                    '#22c55e', // 1 hop
                    '#4ade80', // 2 hops
                    '#86efac', // 3 hops
                    '#fbbf24', // 4 hops
                    '#f59e0b', // 5 hops
                    '#f97316', // 6 hops
                    '#ef4444', // 7 hops
                    '#dc2626', // 8+ hops
                  ];
                  return <Cell key={`cell-${index}`} fill={colors[index] ?? '#3b82f6'} />;
                })}
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        ) : (
          <p className="text-slate-500 text-sm text-center py-8">
            Perform lookups to see hop distribution
          </p>
        )}
      </div>

      {/* Node key distribution */}
      <div className="bg-slate-900 rounded border border-slate-800 p-4">
        <p className="text-sm text-slate-300 mb-3">Key Distribution</p>
        {nodes.size > 0 ? (
          <div className="space-y-2">
            {Array.from(nodes.values()).map((n) => (
              <div key={n.id} className="flex items-center gap-3 text-xs">
                <span className="font-mono text-slate-500 w-20">{n.id.slice(0, 8)}</span>
                <div className="flex-1 bg-slate-800 rounded-full h-2 overflow-hidden">
                  <div
                    className="h-full bg-blue-600 rounded-full transition-all"
                    style={{
                      width:
                        keyCount > 0
                          ? `${Math.min(100, (n.keyCount / keyCount) * 100 * nodeCount)}%`
                          : '0%',
                    }}
                  />
                </div>
                <span className="text-slate-400 w-8 text-right">{n.keyCount}</span>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-slate-500 text-sm text-center py-4">No nodes active</p>
        )}
      </div>
    </div>
  );
}

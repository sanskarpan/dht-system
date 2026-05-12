import { useParams, Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { useState, useEffect, useRef, useMemo } from 'react';
import * as api from '@/api/client';
import type { NodeSnapshot, KBucketInfo, ContactInfo, DHTEvent } from '@/types/dht';
import { useDHTStore } from '@/store/dhtStore';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function relativeTime(iso: string): string {
  const diffMs = Date.now() - new Date(iso).getTime();
  const diffSec = Math.floor(diffMs / 1000);
  if (diffSec < 5) return 'just now';
  if (diffSec < 60) return `${diffSec}s ago`;
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHr = Math.floor(diffMin / 60);
  return `${diffHr}h ago`;
}

const EVENT_BADGE: Record<string, string> = {
  node_join:        'bg-green-900 text-green-300',
  node_leave:       'bg-orange-900 text-orange-300',
  node_crash:       'bg-red-900 text-red-300',
  stabilize:        'bg-sky-900 text-sky-300',
  fix_fingers:      'bg-indigo-900 text-indigo-300',
  notify:           'bg-violet-900 text-violet-300',
  key_migrate:      'bg-amber-900 text-amber-300',
  lookup_start:     'bg-teal-900 text-teal-300',
  lookup_hop:       'bg-teal-900 text-teal-300',
  lookup_complete:  'bg-teal-900 text-teal-300',
  write_quorum:     'bg-yellow-900 text-yellow-300',
  read_quorum:      'bg-yellow-900 text-yellow-300',
  vector_clock:     'bg-purple-900 text-purple-300',
  conflict_detected:'bg-red-900 text-red-300',
  gossip_sync:      'bg-cyan-900 text-cyan-300',
  bucket_update:    'bg-blue-900 text-blue-300',
  republish:        'bg-slate-700 text-slate-300',
  ring_state:       'bg-slate-700 text-slate-300',
  scenario_step:    'bg-pink-900 text-pink-300',
};

function eventBadgeClass(type: string): string {
  return EVENT_BADGE[type] ?? 'bg-slate-700 text-slate-300';
}

/** Extract a short human-readable description from an event payload. */
function describeEvent(event: DHTEvent): string {
  const p = event.payload as Record<string, unknown> | null;
  if (!p) return '';
  switch (event.type) {
    case 'node_join':
      return `joined at ${p.addr ?? ''}`;
    case 'node_leave':
    case 'node_crash':
      return `node ${String(p.nodeId ?? '').slice(0, 8)}`;
    case 'stabilize':
      return `succ=${String(p.successor ?? '').slice(0, 8) || '?'}`;
    case 'fix_fingers':
      return `finger[${p.index ?? '?'}]=${String(p.nodeId ?? '').slice(0, 8) || '?'}`;
    case 'notify':
      return `pred=${String(p.predecessor ?? '').slice(0, 8) || '?'}`;
    case 'key_migrate':
      return `${p.keyCount ?? '?'} keys`;
    case 'lookup_start':
      return `key ${String(p.key ?? '').slice(0, 12)}`;
    case 'lookup_hop':
      return `${String(p.fromNode ?? '').slice(0, 8)} -> ${String(p.toNode ?? '').slice(0, 8)} via ${p.mechanism ?? ''}`;
    case 'lookup_complete':
      return `${String(p.key ?? '').slice(0, 12)} in ${p.totalHops ?? '?'} hops`;
    case 'write_quorum':
      return `key ${String(p.key ?? '').slice(0, 12)}, quorum ${p.acksReceived ?? '?'}/${p.n ?? '?'}`;
    case 'read_quorum':
      return `key ${String(p.key ?? '').slice(0, 12)}, quorum ${p.responsesReceived ?? '?'}/${p.n ?? '?'}`;
    case 'gossip_sync':
      return `${String(p.fromNode ?? '').slice(0, 8)} -> ${String(p.toNode ?? '').slice(0, 8)}, ${p.keysReconciled ?? 0} keys`;
    case 'bucket_update':
      return `bucket[${p.bucketIndex ?? '?'}] contact=${String(p.contactId ?? '').slice(0, 8)}`;
    case 'republish':
      return `key ${String(p.key ?? '').slice(0, 12)}`;
    case 'conflict_detected':
      return `key ${String(p.key ?? '').slice(0, 12)}`;
    case 'scenario_step':
      return String(p.message ?? '');
    default:
      return '';
  }
}

/** Return true if this event involves the given nodeId. */
function eventInvolvesNode(event: DHTEvent, nodeId: string): boolean {
  const p = event.payload as Record<string, unknown> | null;
  if (!p) return false;
  return (
    p.nodeId === nodeId ||
    p.fromNode === nodeId ||
    p.toNode === nodeId
  );
}

// ---------------------------------------------------------------------------
// K-Bucket section sub-components
// ---------------------------------------------------------------------------

interface BucketRowProps {
  bucket: KBucketInfo;
}

function BucketRow({ bucket }: BucketRowProps) {
  const [expanded, setExpanded] = useState(false);
  return (
    <div className="border-t border-slate-800 first:border-t-0">
      <button
        onClick={() => setExpanded((v) => !v)}
        className="w-full flex items-center gap-3 py-1.5 px-2 hover:bg-slate-800 transition-colors text-left"
      >
        <span className="text-slate-500 font-mono text-xs w-8 shrink-0">{bucket.index}</span>
        <span className="bg-blue-900 text-blue-300 text-xs font-medium px-1.5 py-0.5 rounded">
          {bucket.contacts.length}
        </span>
        <span className="text-slate-600 text-xs ml-auto">{expanded ? '▲' : '▼'}</span>
      </button>
      {expanded && (
        <div className="pl-10 pb-2 space-y-1">
          {bucket.contacts.map((c: ContactInfo) => (
            <div key={c.id} className="flex items-center gap-3 text-xs font-mono">
              <span className="text-slate-300">{c.id.slice(0, 8)}</span>
              <span className="text-slate-500">{c.addr}</span>
              <span className="text-slate-600 ml-auto">{relativeTime(c.lastSeen)}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

interface KBucketSectionProps {
  kBuckets: KBucketInfo[];
}

function KBucketSection({ kBuckets }: KBucketSectionProps) {
  const [sectionOpen, setSectionOpen] = useState(true);
  const [showAll, setShowAll] = useState(false);

  const nonEmpty = useMemo(
    () => kBuckets.filter((b) => b.contacts.length > 0),
    [kBuckets]
  );

  const visible = showAll ? nonEmpty : nonEmpty.slice(0, 20);

  return (
    <div className="bg-slate-900 rounded border border-slate-800 mb-4">
      <button
        onClick={() => setSectionOpen((v) => !v)}
        className="w-full flex items-center justify-between px-4 py-3 hover:bg-slate-800 transition-colors rounded"
      >
        <span className="text-xs font-medium text-slate-300">
          K-Buckets
          <span className="ml-2 text-slate-500 font-normal">
            ({nonEmpty.length} non-empty of {kBuckets.length})
          </span>
        </span>
        <span className="text-slate-600 text-xs">{sectionOpen ? '▲' : '▼'}</span>
      </button>

      {sectionOpen && (
        <div className="border-t border-slate-800">
          {nonEmpty.length === 0 ? (
            <p className="text-slate-600 text-xs px-4 py-3">No populated buckets.</p>
          ) : (
            <>
              <div>
                {visible.map((bucket) => (
                  <BucketRow key={bucket.index} bucket={bucket} />
                ))}
              </div>
              {nonEmpty.length > 20 && (
                <div className="px-4 py-2 border-t border-slate-800">
                  <button
                    onClick={() => setShowAll((v) => !v)}
                    className="text-xs text-blue-400 hover:text-blue-300 transition-colors"
                  >
                    {showAll
                      ? 'Show fewer'
                      : `Show all ${nonEmpty.length} buckets`}
                  </button>
                </div>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Event Log section
// ---------------------------------------------------------------------------

interface EventLogSectionProps {
  nodeId: string;
}

function EventLogSection({ nodeId }: EventLogSectionProps) {
  const events = useDHTStore((s) => s.events);
  const bottomRef = useRef<HTMLDivElement>(null);

  // events are stored newest-first; filter then reverse so oldest is at top
  const filtered = useMemo(() => {
    const matching = events.filter((e) => eventInvolvesNode(e, nodeId));
    return matching.slice(0, 20).reverse();
  }, [events, nodeId]);

  // Auto-scroll to bottom when new events arrive
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [filtered]);

  return (
    <div className="bg-slate-900 rounded border border-slate-800 mb-4">
      <div className="flex items-center justify-between px-4 py-3 border-b border-slate-800">
        <span className="text-xs font-medium text-slate-300">
          Event Log
          <span className="ml-2 text-slate-500 font-normal">(last 20 for this node)</span>
        </span>
        <span className="text-xs text-slate-600">{filtered.length} events</span>
      </div>

      <div className="max-h-72 overflow-y-auto">
        {filtered.length === 0 ? (
          <p className="text-slate-600 text-xs px-4 py-3">No events yet for this node.</p>
        ) : (
          <div className="divide-y divide-slate-800">
            {filtered.map((event, idx) => (
              <div key={idx} className="flex items-start gap-3 px-4 py-2 hover:bg-slate-800 transition-colors">
                <span className="text-slate-600 font-mono text-xs w-16 shrink-0 pt-0.5">
                  {relativeTime(event.timestamp)}
                </span>
                <span className={`text-xs font-medium px-1.5 py-0.5 rounded shrink-0 ${eventBadgeClass(event.type)}`}>
                  {event.type.replace(/_/g, ' ')}
                </span>
                <span className="text-slate-400 font-mono text-xs break-all">
                  {describeEvent(event)}
                </span>
              </div>
            ))}
          </div>
        )}
        <div ref={bottomRef} />
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export default function NodeInspector() {
  const { id } = useParams<{ id: string }>();

  const { data: node, isLoading, error } = useQuery<NodeSnapshot>({
    queryKey: ['node', id],
    queryFn: () => api.getNode(id!),
    refetchInterval: 2000,
    enabled: !!id,
  });

  if (isLoading) return <div className="p-6 text-slate-400">Loading...</div>;
  if (error || !node) return <div className="p-6 text-red-400">Failed to load node {id}</div>;

  return (
    <div className="p-6 max-w-4xl mx-auto">
      <div className="flex items-center gap-4 mb-6">
        <Link to="/" className="text-slate-400 hover:text-slate-200 text-sm">← Ring</Link>
        <h1 className="font-mono text-slate-200 text-sm">{node.id}</h1>
        <span className={`px-2 py-0.5 rounded text-xs font-medium ${
          node.status === 'healthy' ? 'bg-green-900 text-green-300' : 'bg-yellow-900 text-yellow-300'
        }`}>{node.status}</span>
      </div>

      <div className="grid grid-cols-2 gap-4 mb-6">
        <div className="bg-slate-900 rounded p-4 border border-slate-800">
          <p className="text-xs text-slate-500 mb-1">Address</p>
          <p className="font-mono text-slate-200 text-sm">{node.addr}</p>
        </div>
        <div className="bg-slate-900 rounded p-4 border border-slate-800">
          <p className="text-xs text-slate-500 mb-1">Protocol</p>
          <p className="font-mono text-slate-200 text-sm">{node.protocol}</p>
        </div>
        <div className="bg-slate-900 rounded p-4 border border-slate-800">
          <p className="text-xs text-slate-500 mb-1">Successor</p>
          {node.successor ? (
            <Link to={`/nodes/${node.successor}`} className="font-mono text-blue-400 hover:text-blue-300 text-sm">
              {node.successor.slice(0, 12)}...
            </Link>
          ) : <p className="text-slate-500 text-sm">None</p>}
        </div>
        <div className="bg-slate-900 rounded p-4 border border-slate-800">
          <p className="text-xs text-slate-500 mb-1">Predecessor</p>
          {node.predecessor ? (
            <Link to={`/nodes/${node.predecessor}`} className="font-mono text-blue-400 hover:text-blue-300 text-sm">
              {node.predecessor.slice(0, 12)}...
            </Link>
          ) : <p className="text-slate-500 text-sm">None</p>}
        </div>
      </div>

      <div className="bg-slate-900 rounded border border-slate-800 p-4 mb-4">
        <p className="text-xs text-slate-500 mb-3">Keys stored: {node.keyCount}</p>
        {node.protocol === 'chord' && node.fingerTable && (
          <div>
            <p className="text-xs text-slate-500 mb-2">
              Finger Table
              <span className="ml-2 text-slate-600 font-normal">
                ({node.fingerTable.filter(f => f.nodeId).length} populated of {node.fingerTable.length})
              </span>
            </p>
            <div className="overflow-x-auto max-h-64 overflow-y-auto rounded border border-slate-800">
              <table className="w-full text-xs font-mono">
                <thead className="sticky top-0 bg-slate-900 z-10">
                  <tr className="text-slate-500 border-b border-slate-800">
                    <th className="text-left py-1 pr-4 pl-2">i</th>
                    <th className="text-left py-1 pr-4">start</th>
                    <th className="text-left py-1">node</th>
                  </tr>
                </thead>
                <tbody>
                  {node.fingerTable.map((f) => (
                    <tr key={f.index} className="border-t border-slate-800/60 hover:bg-slate-800/30">
                      <td className="py-1 pr-4 pl-2 text-slate-400">{f.index}</td>
                      <td className="py-1 pr-4 text-slate-500">{f.start.slice(0, 10)}…</td>
                      <td className="py-1">
                        {f.nodeId ? (
                          <span className="text-sky-400">{f.nodeId.slice(0, 10)}…</span>
                        ) : (
                          <span className="text-slate-700">—</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </div>

      {node.protocol === 'kademlia' && node.kBuckets && (
        <KBucketSection kBuckets={node.kBuckets} />
      )}

      {id && <EventLogSection nodeId={id} />}
    </div>
  );
}

import { useState, useEffect, useRef, useMemo } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useDHTStore } from '@/store/dhtStore';
import type { DHTStore } from '@/store/dhtStore';
import * as api from '@/api/client';
import type { LookupTrace } from '@/types/dht';
import { toast } from '@/components/toastStore';
import NetworkConfigPanel from '@/components/RingVisualizer/NetworkConfigPanel';

export default function Sidebar() {
  const [keyInput, setKeyInput] = useState('');
  const [valueInput, setValueInput] = useState('');
  const [lookupKey, setLookupKey] = useState('');
  const [lookupResult, setLookupResult] = useState<string | null>(null);
  const [netConfigOpen, setNetConfigOpen] = useState(false);
  const qc = useQueryClient();

  const protocol = useDHTStore((s: DHTStore) => s.protocol);
  const selectedNode = useDHTStore((s: DHTStore) => s.selectedNode);
  const storeConfig = useDHTStore((s: DHTStore) => s.config);
  const nodeMap = useDHTStore((s: DHTStore) => s.nodes);
  const nodes = useMemo(
    () => Array.from(nodeMap.values()).sort((a, b) => a.id.localeCompare(b.id)),
    [nodeMap],
  );

  const configMut = useMutation({
    mutationFn: (cfg: Parameters<typeof api.updateNetworkConfig>[0]) =>
      api.updateNetworkConfig(cfg),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['network'] }),
  });

  // Debounce helper so rapid slider drags only fire one request per 300 ms
  const debounceTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => () => {
    if (debounceTimer.current) clearTimeout(debounceTimer.current);
  }, []);

  const sendConfig = (cfg: Parameters<typeof api.updateNetworkConfig>[0]) => {
    if (debounceTimer.current) clearTimeout(debounceTimer.current);
    debounceTimer.current = setTimeout(() => configMut.mutate(cfg), 300);
  };

  const configSignature = storeConfig
    ? [
        storeConfig.protocol,
        storeConfig.writeQuorum,
        storeConfig.readQuorum,
        storeConfig.replicationN,
        storeConfig.stabilizeIntervalMs,
        storeConfig.simDelayMs,
        storeConfig.simLossRate,
      ].join(':')
    : 'config-default';

  const spawnMut = useMutation({
    mutationFn: () => api.spawnNode(),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['network'] });
      toast.success('Node added to network');
    },
    onError: (err: unknown) => {
      toast.error('Failed to add node: ' + (err instanceof Error ? err.message : String(err)));
    },
  });

  const killMut = useMutation({
    mutationFn: (id: string) => api.killNode(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['network'] });
      toast.success('Node gracefully left');
    },
    onError: (err: unknown) => {
      toast.error('Kill failed: ' + (err instanceof Error ? err.message : String(err)));
    },
  });

  const crashMut = useMutation({
    mutationFn: (id: string) => api.crashNode(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['network'] });
      toast.error('Node crashed');
    },
    onError: (err: unknown) => {
      toast.error('Crash failed: ' + (err instanceof Error ? err.message : String(err)));
    },
  });

  const partitionMut = useMutation({
    mutationFn: (groups: string[][]) => api.setNetworkPartition(groups),
    onSuccess: () => {
      toast.success('Network partition applied');
    },
    onError: (err: unknown) => {
      toast.error('Partition failed: ' + (err instanceof Error ? err.message : String(err)));
    },
  });

  const healPartitionMut = useMutation({
    mutationFn: () => api.healNetworkPartition(),
    onSuccess: () => {
      toast.success('Network partition healed');
    },
    onError: (err: unknown) => {
      toast.error('Heal failed: ' + (err instanceof Error ? err.message : String(err)));
    },
  });

  const putMut = useMutation({
    mutationFn: () => api.putKey(keyInput, valueInput),
    onSuccess: () => {
      setKeyInput('');
      setValueInput('');
      qc.invalidateQueries({ queryKey: ['network'] });
      toast.success('Key inserted successfully');
    },
    onError: (err: unknown) => {
      toast.error('Insert failed: ' + (err instanceof Error ? err.message : String(err)));
    },
  });

  const handleLookup = async () => {
    try {
      const trace: LookupTrace = await api.traceLookup(lookupKey);
      setLookupResult(`${trace.totalHops} hops → ${trace.targetNode.slice(0, 8)}...`);
    } catch {
      setLookupResult('Lookup failed');
    }
  };

  const switchProtocol = async (p: string) => {
    await api.updateNetworkConfig({ protocol: p as 'chord' | 'kademlia' });
  };

  const splitNetwork = () => {
    if (nodes.length < 2) {
      toast.error('Need at least two nodes to partition the network');
      return;
    }
    const mid = Math.ceil(nodes.length / 2);
    partitionMut.mutate([
      nodes.slice(0, mid).map((node) => node.id),
      nodes.slice(mid).map((node) => node.id),
    ]);
  };

  return (
    <div className="w-full lg:w-64 shrink-0 border-b lg:border-b-0 lg:border-r border-slate-800 bg-slate-950 flex flex-col gap-4 p-4 overflow-y-auto max-h-[45vh] lg:max-h-none">
      <div>
        <p className="text-xs text-slate-500 uppercase tracking-wider mb-2">Protocol</p>
        <div className="flex gap-2">
          {(['chord', 'kademlia'] as const).map((p) => (
            <button
              key={p}
              onClick={() => switchProtocol(p)}
              className={`flex-1 py-1 rounded text-xs font-medium transition-colors ${
                protocol === p
                  ? 'bg-blue-600 text-white'
                  : 'bg-slate-800 text-slate-400 hover:bg-slate-700'
              }`}
            >
              {p.charAt(0).toUpperCase() + p.slice(1)}
            </button>
          ))}
        </div>
      </div>

      <div>
        <p className="text-xs text-slate-500 uppercase tracking-wider mb-2">Network</p>
        <button
          onClick={() => spawnMut.mutate()}
          disabled={spawnMut.isPending}
          className="w-full py-1.5 bg-green-700 hover:bg-green-600 text-white text-xs rounded font-medium mb-2 disabled:opacity-50"
        >
          {spawnMut.isPending ? '...' : '+ Add Node'}
        </button>
        {selectedNode && (
          <div className="flex gap-2">
            <button
              onClick={() => killMut.mutate(selectedNode)}
              disabled={killMut.isPending}
              className="flex-1 py-1 bg-yellow-700 hover:bg-yellow-600 text-white text-xs rounded disabled:opacity-50"
            >
              {killMut.isPending ? '...' : 'Kill'}
            </button>
            <button
              onClick={() => crashMut.mutate(selectedNode)}
              disabled={crashMut.isPending}
              className="flex-1 py-1 bg-red-700 hover:bg-red-600 text-white text-xs rounded disabled:opacity-50"
            >
              {crashMut.isPending ? '...' : 'Crash'}
            </button>
          </div>
        )}
        <div className="flex gap-2 mt-2">
          <button
            onClick={splitNetwork}
            disabled={partitionMut.isPending || nodes.length < 2}
            className="flex-1 py-1 bg-orange-700 hover:bg-orange-600 text-white text-xs rounded disabled:opacity-50"
          >
            {partitionMut.isPending ? '...' : 'Split 50/50'}
          </button>
          <button
            onClick={() => healPartitionMut.mutate()}
            disabled={healPartitionMut.isPending}
            className="flex-1 py-1 bg-sky-700 hover:bg-sky-600 text-white text-xs rounded disabled:opacity-50"
          >
            {healPartitionMut.isPending ? '...' : 'Heal'}
          </button>
        </div>
      </div>

      <div>
        <p className="text-xs text-slate-500 uppercase tracking-wider mb-2">Insert Key</p>
        <input
          value={keyInput}
          onChange={(e) => setKeyInput(e.target.value)}
          placeholder="Key"
          className="w-full bg-slate-800 border border-slate-700 text-slate-200 text-xs rounded px-2 py-1.5 mb-1.5 outline-none focus:border-blue-500"
        />
        <input
          value={valueInput}
          onChange={(e) => setValueInput(e.target.value)}
          placeholder="Value"
          className="w-full bg-slate-800 border border-slate-700 text-slate-200 text-xs rounded px-2 py-1.5 mb-1.5 outline-none focus:border-blue-500"
        />
        <button
          onClick={() => putMut.mutate()}
          disabled={putMut.isPending || !keyInput}
          className="w-full py-1.5 bg-blue-700 hover:bg-blue-600 text-white text-xs rounded font-medium disabled:opacity-50"
        >
          {putMut.isPending ? '...' : 'Insert'}
        </button>
      </div>

      <div>
        <p className="text-xs text-slate-500 uppercase tracking-wider mb-2">Lookup</p>
        <input
          value={lookupKey}
          onChange={(e) => setLookupKey(e.target.value)}
          placeholder="Key to look up"
          className="w-full bg-slate-800 border border-slate-700 text-slate-200 text-xs rounded px-2 py-1.5 mb-1.5 outline-none focus:border-blue-500"
        />
        <button
          onClick={handleLookup}
          disabled={!lookupKey}
          className="w-full py-1.5 bg-purple-700 hover:bg-purple-600 text-white text-xs rounded font-medium disabled:opacity-50 mb-1.5"
        >
          Trace Lookup
        </button>
        {lookupResult && (
          <p className="text-xs text-green-400 font-mono">{lookupResult}</p>
        )}
      </div>

      {/* ── Network Config ── */}
      <div className="border border-slate-800 rounded">
        <button
          onClick={() => setNetConfigOpen((o) => !o)}
          className="w-full flex items-center justify-between px-3 py-2 text-xs text-slate-400 uppercase tracking-wider hover:bg-slate-800/60 transition-colors rounded"
        >
          <span>Network Config</span>
          <span className="text-slate-500 text-base leading-none">
            {netConfigOpen ? '−' : '+'}
          </span>
        </button>

        {netConfigOpen && storeConfig && (
          <div className="px-3 pb-3 pt-1">
            <NetworkConfigPanel
              key={configSignature}
              config={storeConfig}
              onChange={sendConfig}
            />
            {configMut.isError && (
              <p className="text-xs text-red-400 mt-1">Failed to update config</p>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

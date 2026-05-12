import type {
  NetworkState,
  Config,
  NodeSnapshot,
  KeySnapshot,
  FaultState,
  LookupTrace,
  ValueEntry,
} from '@/types/dht';

const BASE = '/api/v1';

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { 'Content-Type': 'application/json', ...init?.headers },
    ...init,
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`${res.status} ${res.statusText}: ${text}`);
  }
  return res.json() as Promise<T>;
}

// Network
export const getNetworkState = () => request<NetworkState>('/network/state');
export const updateNetworkConfig = (cfg: Partial<Config>) =>
  request<Config>('/network/config', { method: 'PUT', body: JSON.stringify(cfg) });
export const startNetwork = (cfg: Partial<Config> & { nodeCount?: number }) =>
  request<NetworkState>('/network/start', { method: 'POST', body: JSON.stringify(cfg) });
export const resetNetwork = () =>
  request<{ success: boolean; message: string }>('/network/reset', { method: 'POST' });
export const setNetworkPartition = (groups: string[][]) =>
  request<{ success: boolean; groups: string[][] }>('/network/partition', {
    method: 'POST',
    body: JSON.stringify({ groups }),
  });
export const healNetworkPartition = () =>
  request<{ success: boolean }>('/network/heal', { method: 'POST' });
export const getNetworkFaults = () =>
  request<FaultState>('/network/faults');
export const setLinkLatency = (from: string, to: string, latencyMs: number) =>
  request<{ success: boolean; from: string; to: string; latencyMs: number }>('/network/links', {
    method: 'PUT',
    body: JSON.stringify({ from, to, latencyMs }),
  });
export const clearLinkLatencies = () =>
  request<{ success: boolean }>('/network/links', { method: 'DELETE' });

// Nodes
export const spawnNode = (addr?: string) =>
  request<NodeSnapshot>('/nodes', { method: 'POST', body: JSON.stringify({ addr }) });
export const killNode = (nodeId: string) =>
  request<void>(`/nodes/${nodeId}`, { method: 'DELETE' });
export const crashNode = (nodeId: string) =>
  request<void>(`/nodes/${nodeId}/crash`, { method: 'POST' });
export const getNode = (nodeId: string) =>
  request<NodeSnapshot>(`/nodes/${nodeId}`);

// KV
export const putKey = (key: string, value: string) =>
  request<{ success: boolean }>('/kv', { method: 'POST', body: JSON.stringify({ key, value }) });
export const getKey = (key: string) =>
  request<KeySnapshot>(`/kv/${encodeURIComponent(key)}`);
export const deleteKey = (key: string) =>
  request<{ success: boolean }>(`/kv/${encodeURIComponent(key)}`, { method: 'DELETE' });
export const getKeyReplicas = (key: string) =>
  request<{ replicas: Array<{ nodeId: string; entry: ValueEntry }> }>(
    `/kv/${encodeURIComponent(key)}/replicas`
  );

// Lookup
export const traceLookup = (key: string) =>
  request<LookupTrace>('/lookup', { method: 'POST', body: JSON.stringify({ key }) });

// Scenarios
export const runScenario = (name: string, params?: { nodeCount?: number; durationSeconds?: number; keyCount?: number }) =>
  request<{ started: boolean }>(`/scenarios/${name}/run`, { method: 'POST', body: JSON.stringify(params ?? {}) });
export const listScenarios = () =>
  request<{ scenarios: Array<{ name: string; description: string }> }>('/scenarios');

// Metrics
export const getMetricsJson = () =>
  request<Record<string, unknown>>('/metrics/json');

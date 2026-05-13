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
const ERROR_PREVIEW_LIMIT = 240;

export class ApiRequestError extends Error {
  readonly status: number;

  readonly statusText: string;

  readonly contentType: string;

  readonly summary: string;

  constructor(status: number, statusText: string, contentType: string, summary: string) {
    super(`${status} ${statusText}: ${summary}`);
    this.name = 'ApiRequestError';
    this.status = status;
    this.statusText = statusText;
    this.contentType = contentType;
    this.summary = summary;
  }
}

function compactWhitespace(input: string): string {
  return input.replace(/\s+/g, ' ').trim();
}

function preview(input: string): string {
  const normalized = compactWhitespace(input);
  if (!normalized) {
    return 'empty response body';
  }
  if (normalized.length <= ERROR_PREVIEW_LIMIT) {
    return normalized;
  }
  return `${normalized.slice(0, ERROR_PREVIEW_LIMIT - 1).trimEnd()}…`;
}

function extractJsonMessage(value: unknown): string | undefined {
  if (typeof value === 'string') {
    const trimmed = value.trim();
    return trimmed || undefined;
  }

  if (!value || typeof value !== 'object') {
    return undefined;
  }

  const record = value as Record<string, unknown>;
  for (const key of ['error', 'message', 'details', 'title']) {
    const candidate = record[key];
    if (typeof candidate === 'string') {
      const trimmed = candidate.trim();
      if (trimmed) {
        return trimmed;
      }
    }
  }

  const nestedError = record.error;
  if (nestedError && typeof nestedError === 'object') {
    const nestedMessage = extractJsonMessage(nestedError);
    if (nestedMessage) {
      return nestedMessage;
    }
  }

  const nestedMessages = record.errors;
  if (Array.isArray(nestedMessages)) {
    const joined = nestedMessages
      .filter((entry): entry is string => typeof entry === 'string')
      .map((entry) => entry.trim())
      .filter(Boolean)
      .slice(0, 3)
      .join('; ');
    if (joined) {
      return joined;
    }
  }

  return undefined;
}

function stripHtml(input: string): string {
  return input.replace(/<[^>]*>/g, ' ');
}

function extractHtmlTitle(input: string): string | undefined {
  const match = input.match(/<title[^>]*>(.*?)<\/title>/is);
  if (!match) {
    return undefined;
  }
  const title = compactWhitespace(match[1]);
  return title || undefined;
}

export function summarizeErrorResponse(contentType: string, bodyText: string): string {
  const normalized = compactWhitespace(bodyText);
  if (!normalized) {
    return 'empty response body';
  }

  const lowerContentType = contentType.toLowerCase();
  const looksLikeJson = normalized.startsWith('{') || normalized.startsWith('[');
  if (lowerContentType.includes('application/json') || looksLikeJson) {
    try {
      const parsed = JSON.parse(bodyText);
      const message = extractJsonMessage(parsed);
      if (message) {
        return preview(message);
      }
      return preview(JSON.stringify(parsed));
    } catch {
      // Fall through to plain-text / HTML summarization.
    }
  }

  if (lowerContentType.includes('text/html') || /<!doctype html|<html/i.test(bodyText)) {
    const title = extractHtmlTitle(bodyText);
    if (title) {
      return `HTML response: ${preview(title)}`;
    }
    return `HTML response: ${preview(stripHtml(bodyText))}`;
  }

  return preview(bodyText);
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { 'Content-Type': 'application/json', ...init?.headers },
    ...init,
  });
  if (!res.ok) {
    const text = await res.text();
    const contentType = res.headers.get('content-type') ?? '';
    const summary = summarizeErrorResponse(contentType, text);
    throw new ApiRequestError(res.status, res.statusText, contentType, summary);
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

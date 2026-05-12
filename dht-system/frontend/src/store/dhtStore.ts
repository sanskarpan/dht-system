import { create } from 'zustand';
import type { NodeSnapshot, KeySnapshot, Config, DHTEvent, LookupTrace, Protocol } from '@/types/dht';

interface DHTStore {
  // State
  nodes: Map<string, NodeSnapshot>;
  keys: Map<string, KeySnapshot>;
  config: Config | null;
  events: DHTEvent[];
  selectedNode: string | null;
  activeLookup: LookupTrace | null;
  connected: boolean;
  protocol: Protocol;

  // Actions
  setNetworkState: (state: { nodes: NodeSnapshot[]; keys: KeySnapshot[]; config: Config }) => void;
  applyEvent: (event: DHTEvent) => void;
  setSelectedNode: (id: string | null) => void;
  setActiveLookup: (trace: LookupTrace | null) => void;
  setConnected: (connected: boolean) => void;
}

export const useDHTStore = create<DHTStore>((set) => ({
  nodes: new Map(),
  keys: new Map(),
  config: null,
  events: [],
  selectedNode: null,
  activeLookup: null,
  connected: false,
  protocol: 'chord',

  setNetworkState: ({ nodes, keys, config }) => {
    const nodeMap = new Map(nodes.map((n) => [n.id, n]));
    const keyMap = new Map(keys.map((k) => [k.id, k]));
    set({ nodes: nodeMap, keys: keyMap, config, protocol: config.protocol });
  },

  applyEvent: (event) => {
    set((state) => {
      const events = [event, ...state.events].slice(0, 200);
      const nodes = new Map(state.nodes);

      switch (event.type) {
        case 'node_join': {
          const p = event.payload as { nodeId: string; addr: string; successor?: string; predecessor?: string };
          if (p.nodeId) {
            const existing = nodes.get(p.nodeId);
            nodes.set(p.nodeId, {
              id: p.nodeId,
              addr: p.addr,
              status: 'stabilizing',
              keyCount: 0,
              protocol: state.protocol,
              ...existing,
              successor: p.successor,
              predecessor: p.predecessor,
            });
          }
          break;
        }
        case 'node_leave':
        case 'node_crash': {
          const p = event.payload as { nodeId: string };
          nodes.delete(p.nodeId);
          break;
        }
        case 'stabilize': {
          const p = event.payload as { nodeId: string; successor?: string; predecessor?: string };
          if (p.nodeId && nodes.has(p.nodeId)) {
            const n = nodes.get(p.nodeId)!;
            nodes.set(p.nodeId, { ...n, status: 'healthy', successor: p.successor, predecessor: p.predecessor });
          }
          break;
        }
        case 'ring_state': {
          // Full ring snapshot replaces current state
          const p = event.payload as {
            nodes: NodeSnapshot[];
            keys: KeySnapshot[];
            config?: Config;
            protocol?: Protocol;
          };
          return {
            events,
            nodes: new Map(p.nodes.map((n) => [n.id, n])),
            keys: new Map(p.keys?.map((k) => [k.id, k]) ?? []),
            config: p.config ?? state.config,
            protocol: p.config?.protocol ?? p.protocol ?? state.protocol,
          };
        }
        case 'lookup_complete': {
          // Handled by lookup hooks
          break;
        }
      }

      return { events, nodes };
    });
  },

  setSelectedNode: (id) => set({ selectedNode: id }),
  setActiveLookup: (trace) => set({ activeLookup: trace }),
  setConnected: (connected) => set({ connected }),
}));

// Convenience selectors
export const selectNodeList = (s: DHTStore) => Array.from(s.nodes.values());
export const selectKeyList = (s: DHTStore) => Array.from(s.keys.values());

export type { DHTStore };

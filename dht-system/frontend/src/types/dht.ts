// Core DHT types matching Go backend models

export type NodeStatus = 'healthy' | 'stabilizing' | 'unreachable' | 'crashed';
export type Protocol = 'chord' | 'kademlia';
export type ClockOrder = 'before' | 'after' | 'concurrent' | 'equal';

export interface VectorClock {
  entries: Record<string, number>;
}

export interface ValueEntry {
  key: string;          // hex-encoded 20-byte key ID
  value: string;        // base64-encoded value
  clock: Record<string, number>;  // vector clock: nodeId -> counter (flat map from backend)
  timestamp: string;    // ISO 8601
  ttlNs: number;
  nodeId: string;
}

export interface FingerEntry {
  index: number;
  start: string;   // hex
  nodeId: string;  // hex
  addr: string;
}

export interface KBucketInfo {
  index: number;
  contacts: ContactInfo[];
  capacity: number;
}

export interface ContactInfo {
  id: string;    // hex
  addr: string;
  lastSeen: string;  // ISO 8601
}

export interface NodeSnapshot {
  id: string;          // hex
  addr: string;
  successor?: string;  // hex
  predecessor?: string; // hex
  successorList?: string[]; // hex list
  fingerTable?: FingerEntry[];
  kBuckets?: KBucketInfo[];
  keyCount: number;
  status: NodeStatus;
  protocol: Protocol;
}

export interface KeySnapshot {
  id: string;       // hex - hash of key
  key: string;      // original key string
  value: string;    // base64
  replicas: string[]; // node hex IDs that hold this key
  replicaCount: number;
}

export interface Config {
  protocol: Protocol;
  m: number;
  virtualNodes: number;
  replicationN: number;
  writeQuorum: number;
  readQuorum: number;
  successorListSize: number;
  kBucketSize: number;
  alpha: number;
  stabilizeIntervalMs: number;
  fixFingersIntervalMs: number;
  gossipIntervalMs: number;
  republishIntervalMs: number;
  simDelayMs: number;
  simLossRate: number;
}

export interface NetworkState {
  protocol: Protocol;
  nodes: NodeSnapshot[];
  keys: KeySnapshot[];
  config: Config;
}

export interface FaultState {
  partitions: string[][];
  links: Array<{
    from: string;
    to: string;
    latencyMs: number;
  }>;
}

export interface LookupTrace {
  key: string;
  keyHash: string;
  targetNode: string;
  hops: HopEvent[];
  totalHops: number;
  latencyMs: number;
}

// Event types from WebSocket
export type EventType =
  | 'node_join'
  | 'node_leave'
  | 'node_crash'
  | 'stabilize'
  | 'fix_fingers'
  | 'notify'
  | 'key_migrate'
  | 'lookup_start'
  | 'lookup_hop'
  | 'lookup_complete'
  | 'write_quorum'
  | 'read_quorum'
  | 'vector_clock'
  | 'conflict_detected'
  | 'gossip_sync'
  | 'bucket_update'
  | 'republish'
  | 'ring_state'
  | 'replication_lag'
  | 'scenario_step';

export interface DHTEvent {
  type: EventType;
  timestamp: string;
  payload: unknown;
}

export interface HopEvent {
  fromNode: string;
  toNode: string;
  mechanism: string;  // e.g., "finger[7]", "successor", "k-bucket[3]"
  hopIndex: number;
}

// Specific event payloads
export interface NodeJoinPayload {
  nodeId: string;
  addr: string;
  successor?: string;
  predecessor?: string;
}

export interface LookupHopPayload extends HopEvent {}

export interface LookupCompletePayload {
  key: string;
  keyHash: string;
  targetNode: string;
  totalHops: number;
  latencyMs: number;
}

export interface GossipSyncPayload {
  fromNode: string;
  toNode: string;
  keysReconciled: number;
  divergentBefore: number;
  divergentAfter: number;
}

export interface ConflictPayload {
  key: string;
  versions: ValueEntry[];
  resolved: ValueEntry;
  strategy: 'lww' | 'vector_clock';
}

export interface ScenarioStepPayload {
  scenario: string;
  step: number;
  total: number;
  message: string;
  data: unknown;
}

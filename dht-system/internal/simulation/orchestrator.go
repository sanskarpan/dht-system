// Package simulation provides the Orchestrator, which manages a cluster of
// simulated DHT nodes within a single process.
//
// The Orchestrator wires together:
//   - An InProcessTransport (with optional latency/loss injection)
//   - A set of Chord or Kademlia nodes (selected by Config.Protocol)
//   - A QuorumManager for replication-aware reads and writes
//   - An EventBus for real-time observability
//
// It exposes a high-level API (SpawnNode, KillNode, Insert, Read, Lookup, …)
// used directly by the REST gateway and by integration tests.
package simulation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sanskarpan/dht-system/dht-system/internal/chord"
	"github.com/sanskarpan/dht-system/dht-system/internal/consistent"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/gossip"
	"github.com/sanskarpan/dht-system/dht-system/internal/kademlia"
	"github.com/sanskarpan/dht-system/dht-system/internal/replication"
	"github.com/sanskarpan/dht-system/dht-system/internal/store"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
	"go.uber.org/zap"
)

// Config holds orchestrator configuration.
type Config struct {
	Protocol           string // "chord" | "kademlia"
	M                  int    // hash space bits, default 160
	VirtualNodes       int    // vnodes per physical, default 3
	ReplicationN       int    // default 3
	WriteQuorum        int    // default 2
	ReadQuorum         int    // default 2
	SuccessorListSize  int
	KBucketSize        int
	Alpha              int
	StabilizeInterval  time.Duration
	FixFingersInterval time.Duration
	GossipInterval     time.Duration
	RepublishInterval  time.Duration
	SimDelay           time.Duration
	SimLossRate        float64
	InitialNodes       int
}

func DefaultConfig() *Config {
	return &Config{
		Protocol:           "chord",
		M:                  160,
		VirtualNodes:       3,
		ReplicationN:       3,
		WriteQuorum:        2,
		ReadQuorum:         2,
		SuccessorListSize:  8,
		KBucketSize:        20,
		Alpha:              3,
		StabilizeInterval:  500 * time.Millisecond,
		FixFingersInterval: 1 * time.Second,
		GossipInterval:     2 * time.Second,
		RepublishInterval:  24 * time.Second,
		SimDelay:           0,
		SimLossRate:        0,
		InitialNodes:       6,
	}
}

// Node is the common interface satisfied by both ChordNode and KademliaNode.
type Node interface {
	Ref() transport.NodeRef
}

// NodeState is a snapshot of a node for the REST API.
type NodeState struct {
	ID          string         `json:"id"`
	Addr        string         `json:"addr"`
	Protocol    string         `json:"protocol"`
	Status      string         `json:"status"`
	Successor   *string        `json:"successor,omitempty"`
	Predecessor *string        `json:"predecessor,omitempty"`
	SuccList    []string       `json:"successorList,omitempty"`
	FingerTable []FingerEntry  `json:"fingerTable,omitempty"`
	KBuckets    []KBucketState `json:"kBuckets,omitempty"`
	KeyCount    int            `json:"keyCount"`
}

// FingerEntry is the snapshot of one finger table entry.
type FingerEntry struct {
	Index  int    `json:"index"`
	Start  string `json:"start"`
	NodeID string `json:"nodeId,omitempty"`
	Addr   string `json:"addr,omitempty"`
}

// KBucketState is the snapshot of one k-bucket.
type KBucketState struct {
	Index    int            `json:"index"`
	Contacts []ContactState `json:"contacts"`
	Capacity int            `json:"capacity"`
}

type ContactState struct {
	ID       string `json:"id"`
	Addr     string `json:"addr"`
	LastSeen string `json:"lastSeen"`
}

// KeyState is a snapshot of a stored key.
type KeyState struct {
	ID           string   `json:"id"`
	Key          string   `json:"key"`
	Value        string   `json:"value"`
	Replicas     []string `json:"replicas"`
	ReplicaCount int      `json:"replicaCount"`
}

// NetworkState is the full ring snapshot for the REST API.
type NetworkState struct {
	Protocol string      `json:"protocol"`
	Nodes    []NodeState `json:"nodes"`
	Keys     []KeyState  `json:"keys"`
	Config   *Config     `json:"config"`
}

// LookupTrace records a traced key lookup.
type LookupTrace struct {
	Key        string               `json:"key"`
	KeyHash    string               `json:"keyHash"`
	TargetNode string               `json:"targetNode"`
	Hops       []transport.HopEvent `json:"hops"`
	TotalHops  int                  `json:"totalHops"`
	LatencyMs  int64                `json:"latencyMs"`
}

// Orchestrator manages all DHT nodes, shared transport, and event bus.
type Orchestrator struct {
	mu          sync.RWMutex
	cfg         *Config
	bus         *events.EventBus
	transport   *transport.InProcessTransport
	nodes       map[string]Node // addr -> Node
	chordNodes  map[string]*chord.ChordNode
	kadNodes    map[string]*kademlia.KademliaNode
	kadGossip   map[string]*gossip.AntiEntropy
	nodeAddrs   []string // ordered for successor resolution
	quorum      *replication.QuorumManager
	portCounter int
	logger      *zap.Logger
	keyStrings  map[[20]byte]string // original key string by hash
}

func cloneConfig(cfg *Config) *Config {
	if cfg == nil {
		return nil
	}
	cp := *cfg
	return &cp
}

// NewOrchestrator creates a new Orchestrator.
func NewOrchestrator(cfg *Config, bus *events.EventBus) *Orchestrator {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	logger, _ := zap.NewProduction()

	tr := transport.NewInProcessTransport(cfg.SimDelay, cfg.SimLossRate)
	o := &Orchestrator{
		cfg:         cfg,
		bus:         bus,
		transport:   tr,
		nodes:       make(map[string]Node),
		chordNodes:  make(map[string]*chord.ChordNode),
		kadNodes:    make(map[string]*kademlia.KademliaNode),
		kadGossip:   make(map[string]*gossip.AntiEntropy),
		portCounter: 7000,
		logger:      logger,
		keyStrings:  make(map[[20]byte]string),
	}

	o.quorum = replication.NewQuorumManager(
		&replication.Config{N: cfg.ReplicationN, W: cfg.WriteQuorum, R: cfg.ReadQuorum},
		tr,
		o.findReplicas,
		bus,
	)

	return o
}

// SpawnNode creates a new node and joins it to the network.
func (o *Orchestrator) SpawnNode(addr string) (Node, error) {
	if addr == "" {
		o.mu.Lock()
		addr = fmt.Sprintf("127.0.0.1:%d", o.portCounter)
		o.portCounter++
		o.mu.Unlock()
	}

	o.mu.RLock()
	_, exists := o.nodes[addr]
	firstNode := len(o.nodes) == 0
	o.mu.RUnlock()

	if exists {
		return nil, fmt.Errorf("orchestrator: node %s already exists", addr)
	}

	switch o.cfg.Protocol {
	case "chord":
		return o.spawnChordNode(addr, firstNode)
	case "kademlia":
		return o.spawnKadNode(addr, firstNode)
	default:
		return nil, fmt.Errorf("orchestrator: unknown protocol %q", o.cfg.Protocol)
	}
}

func (o *Orchestrator) spawnChordNode(addr string, firstNode bool) (*chord.ChordNode, error) {
	cfg := &chord.Config{
		StabilizeInterval:  o.cfg.StabilizeInterval,
		FixFingersInterval: o.cfg.FixFingersInterval,
		CheckPredInterval:  o.cfg.StabilizeInterval,
		CheckSuccInterval:  o.cfg.StabilizeInterval,
		SuccessorListSize:  o.cfg.SuccessorListSize,
		MaxHops:            3 * 160,
	}
	node := chord.NewChordNode(addr, cfg, o.transport, o.bus, o.logger)
	o.transport.Register(addr, node)

	if firstNode {
		node.CreateRing()
	} else {
		// Join via a random existing node
		bootstrap := o.getAnyChordBootstrap()
		if bootstrap == nil {
			node.CreateRing()
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := node.Join(ctx, *bootstrap); err != nil {
				o.transport.Deregister(addr)
				return nil, fmt.Errorf("chord join failed: %w", err)
			}
		}
	}

	node.Start()

	o.mu.Lock()
	o.nodes[addr] = node
	o.chordNodes[addr] = node
	o.nodeAddrs = append(o.nodeAddrs, addr)
	o.mu.Unlock()

	return node, nil
}

func (o *Orchestrator) spawnKadNode(addr string, firstNode bool) (*kademlia.KademliaNode, error) {
	cfg := &kademlia.Config{
		K:                     o.cfg.KBucketSize,
		Alpha:                 o.cfg.Alpha,
		RepublishInterval:     o.cfg.RepublishInterval,
		ExpireTTL:             o.cfg.RepublishInterval + 1*time.Second,
		BucketRefreshInterval: 60 * time.Second,
		PingTimeout:           300 * time.Millisecond,
	}
	node := kademlia.NewKademliaNode(addr, cfg, o.transport, o.bus, o.logger)
	o.transport.Register(addr, node)

	if firstNode {
		node.CreateNetwork()
	} else {
		bootstrap := o.getAnyKadBootstrap()
		if bootstrap == nil {
			node.CreateNetwork()
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := node.Join(ctx, *bootstrap); err != nil {
				o.transport.Deregister(addr)
				return nil, fmt.Errorf("kademlia join failed: %w", err)
			}
		}
	}

	ae := gossip.New(
		addr,
		node.KVStore,
		o.transport,
		o.bus,
		func() []transport.NodeRef {
			o.mu.RLock()
			defer o.mu.RUnlock()

			peers := make([]transport.NodeRef, 0, len(o.kadNodes))
			for peerAddr, peer := range o.kadNodes {
				if peerAddr == addr {
					continue
				}
				peers = append(peers, peer.Ref())
			}
			return peers
		},
		&gossip.Config{
			Interval:      o.cfg.GossipInterval,
			PeersPerRound: 2,
		},
	)
	node.SetAntiEntropy(ae)

	node.Start()

	o.mu.Lock()
	o.nodes[addr] = node
	o.kadNodes[addr] = node
	o.kadGossip[addr] = ae
	o.nodeAddrs = append(o.nodeAddrs, addr)
	o.mu.Unlock()

	return node, nil
}

// KillNode gracefully removes a node.
func (o *Orchestrator) KillNode(addr string) error {
	o.mu.Lock()
	node, ok := o.nodes[addr]
	if !ok {
		o.mu.Unlock()
		return fmt.Errorf("orchestrator: node %s not found", addr)
	}
	delete(o.nodes, addr)
	delete(o.chordNodes, addr)
	delete(o.kadNodes, addr)
	delete(o.kadGossip, addr)
	o.removeAddr(addr)
	o.mu.Unlock()

	o.transport.Deregister(addr)

	if cn, ok := node.(*chord.ChordNode); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return cn.Leave(ctx)
	}
	if kn, ok := node.(*kademlia.KademliaNode); ok {
		kn.Stop()
	}
	return nil
}

// CrashNode abruptly stops a node (no cleanup).
func (o *Orchestrator) CrashNode(addr string) error {
	o.mu.Lock()
	node, ok := o.nodes[addr]
	if !ok {
		o.mu.Unlock()
		return fmt.Errorf("orchestrator: node %s not found", addr)
	}
	delete(o.nodes, addr)
	delete(o.chordNodes, addr)
	delete(o.kadNodes, addr)
	delete(o.kadGossip, addr)
	o.removeAddr(addr)
	o.mu.Unlock()

	o.transport.Deregister(addr)

	if cn, ok := node.(*chord.ChordNode); ok {
		cn.SimulateCrash()
	} else if kn, ok := node.(*kademlia.KademliaNode); ok {
		kn.Stop()
		o.bus.Publish(events.MakeEvent(events.EventNodeCrash, events.NodeCrashPayload{
			NodeID: consistent.IDToHex([20]byte(kn.ID)),
		}))
	}
	return nil
}

// Insert performs a quorum write with a 30-second deadline.
func (o *Orchestrator) Insert(key, value string) error {
	keyID := consistent.KeyID(key)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := o.quorum.Write(ctx, keyID, []byte(value), "gateway"); err != nil {
		return err
	}
	// Record original key string so GetNetworkState can display it.
	o.mu.Lock()
	o.keyStrings[keyID] = key
	o.mu.Unlock()
	return nil
}

// Lookup traces a key lookup through the DHT.
// A 30-second deadline ensures the call never blocks indefinitely, even
// under load or race-detector slowdown.
func (o *Orchestrator) Lookup(key string) (*LookupTrace, error) {
	keyID := consistent.KeyID(key)
	start := time.Now()

	o.mu.RLock()
	var firstNode Node
	for _, n := range o.nodes {
		firstNode = n
		break
	}
	o.mu.RUnlock()

	if firstNode == nil {
		return nil, errors.New("orchestrator: no nodes in network")
	}

	firstRef := firstNode.Ref()
	o.bus.Publish(events.MakeEvent(events.EventLookupStart, events.LookupStartPayload{
		Key:      key,
		KeyHash:  consistent.IDToHex(keyID),
		FromNode: consistent.IDToHex(firstRef.ID),
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var hops []transport.HopEvent
	var targetNode transport.NodeRef

	if cn, ok := firstNode.(*chord.ChordNode); ok {
		succ, h, err := cn.FindSuccessor(ctx, keyID)
		if err != nil {
			return nil, err
		}
		hops = h
		targetNode = succ
	} else if kn, ok := firstNode.(*kademlia.KademliaNode); ok {
		contacts, h, err := kn.IterativeFindNode(ctx, kademlia.NodeID(keyID))
		if err != nil {
			return nil, err
		}
		hops = h
		if len(contacts) > 0 {
			targetNode = transport.NodeRef{ID: [20]byte(contacts[0].ID), Addr: contacts[0].Addr}
		}
	}

	trace := &LookupTrace{
		Key:        key,
		KeyHash:    consistent.IDToHex(keyID),
		TargetNode: consistent.IDToHex(targetNode.ID),
		Hops:       hops,
		TotalHops:  len(hops),
		LatencyMs:  time.Since(start).Milliseconds(),
	}

	o.bus.Publish(events.MakeEvent(events.EventLookupComplete, events.LookupCompletePayload{
		Key:        trace.Key,
		KeyHash:    trace.KeyHash,
		TargetNode: trace.TargetNode,
		TotalHops:  trace.TotalHops,
		LatencyMs:  trace.LatencyMs,
	}))

	return trace, nil
}

// GetNetworkState returns a full snapshot of the network for the REST API.
func (o *Orchestrator) GetNetworkState() *NetworkState {
	o.mu.RLock()
	defer o.mu.RUnlock()

	nodes := make([]NodeState, 0, len(o.chordNodes)+len(o.kadNodes))

	for addr, cn := range o.chordNodes {
		state := chordNodeState(addr, cn)
		nodes = append(nodes, state)
	}
	for addr, kn := range o.kadNodes {
		state := kadNodeState(addr, kn)
		nodes = append(nodes, state)
	}

	// Collect all keys (from chord and kademlia nodes)
	keySet := make(map[string]*KeyState)
	for _, cn := range o.chordNodes {
		nodeIDHex := consistent.IDToHex(cn.ID)
		for _, entry := range cn.Store.All() {
			keyHex := consistent.IDToHex(entry.Key)
			if ks, ok := keySet[keyHex]; ok {
				ks.Replicas = append(ks.Replicas, nodeIDHex)
				ks.ReplicaCount++
			} else {
				origKey := o.keyStrings[entry.Key]
				if origKey == "" {
					origKey = keyHex // fallback to hex if original not known
				}
				keySet[keyHex] = &KeyState{
					ID:           keyHex,
					Key:          origKey,
					Value:        string(entry.Value),
					Replicas:     []string{nodeIDHex},
					ReplicaCount: 1,
				}
			}
		}
	}
	for _, kn := range o.kadNodes {
		nodeIDHex := consistent.IDToHex([20]byte(kn.ID))
		for _, entry := range kn.KVStore.All() {
			keyHex := consistent.IDToHex(entry.Key)
			if ks, ok := keySet[keyHex]; ok {
				ks.Replicas = append(ks.Replicas, nodeIDHex)
				ks.ReplicaCount++
			} else {
				origKey := o.keyStrings[entry.Key]
				if origKey == "" {
					origKey = keyHex
				}
				keySet[keyHex] = &KeyState{
					ID:           keyHex,
					Key:          origKey,
					Value:        string(entry.Value),
					Replicas:     []string{nodeIDHex},
					ReplicaCount: 1,
				}
			}
		}
	}

	keys := make([]KeyState, 0, len(keySet))
	for _, ks := range keySet {
		keys = append(keys, *ks)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].ID < keys[j].ID
	})

	return &NetworkState{
		Protocol: o.cfg.Protocol,
		Nodes:    nodes,
		Keys:     keys,
		Config:   o.cfg,
	}
}

// findReplicas returns the N nodes responsible for a key (used by QuorumManager).
func (o *Orchestrator) findReplicas(key [20]byte) []transport.NodeRef {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if o.cfg.Protocol == "chord" {
		return o.findChordReplicas(key)
	}
	return o.findKadReplicas(key)
}

func (o *Orchestrator) findChordReplicas(key [20]byte) []transport.NodeRef {
	if len(o.chordNodes) == 0 {
		return nil
	}

	// Build a sorted slice of all registered chord node refs (by ring ID).
	// This is deterministic regardless of map iteration order, so Write and Read
	// always agree on which nodes hold replicas for a given key.
	refs := make([]transport.NodeRef, 0, len(o.chordNodes))
	for _, cn := range o.chordNodes {
		refs = append(refs, cn.Ref())
	}
	sort.Slice(refs, func(i, j int) bool {
		return consistent.Compare(refs[i].ID, refs[j].ID) < 0
	})

	// Find the first node whose ID >= key (the key's successor in the ring).
	n := len(refs)
	idx := sort.Search(n, func(i int) bool {
		return consistent.Compare(refs[i].ID, key) >= 0
	})
	// Wrap around: if no node has ID >= key, the successor is refs[0].
	idx = idx % n

	// Collect N consecutive nodes starting from idx (with ring wrap-around).
	want := o.cfg.ReplicationN
	if want > n {
		want = n
	}
	result := make([]transport.NodeRef, 0, want)
	for i := 0; i < n && len(result) < want; i++ {
		result = append(result, refs[(idx+i)%n])
	}
	return result
}

func (o *Orchestrator) findKadReplicas(key [20]byte) []transport.NodeRef {
	if len(o.kadNodes) == 0 {
		return nil
	}

	refs := make([]transport.NodeRef, 0, len(o.kadNodes))
	for _, kn := range o.kadNodes {
		refs = append(refs, kn.Ref())
	}
	target := kademlia.NodeID(key)
	sort.Slice(refs, func(i, j int) bool {
		left := kademlia.XORDistance(kademlia.NodeID(refs[i].ID), target)
		right := kademlia.XORDistance(kademlia.NodeID(refs[j].ID), target)
		return left.Cmp(right) < 0
	})

	n := o.cfg.ReplicationN
	if n > len(refs) {
		n = len(refs)
	}
	return append([]transport.NodeRef(nil), refs[:n]...)
}

func (o *Orchestrator) getAnyChordBootstrap() *transport.NodeRef {
	for _, cn := range o.chordNodes {
		ref := cn.Ref()
		return &ref
	}
	return nil
}

func (o *Orchestrator) getAnyKadBootstrap() *kademlia.Contact {
	for _, kn := range o.kadNodes {
		c := kademlia.Contact{ID: kn.ID, Addr: kn.Addr}
		return &c
	}
	return nil
}

func (o *Orchestrator) removeAddr(addr string) {
	for i, a := range o.nodeAddrs {
		if a == addr {
			o.nodeAddrs = append(o.nodeAddrs[:i], o.nodeAddrs[i+1:]...)
			return
		}
	}
}

// OrchestratorConfig returns the current config.
func (o *Orchestrator) OrchestratorConfig() *Config {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return cloneConfig(o.cfg)
}

// Read performs a quorum read and returns the value.
func (o *Orchestrator) Read(key string) ([]byte, error) {
	keyID := consistent.KeyID(key)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	entry, err := o.quorum.Read(ctx, keyID)
	if err != nil {
		return nil, err
	}
	return entry.Value, nil
}

// SetPartition splits the network into groups; cross-group RPCs fail.
func (o *Orchestrator) SetPartition(groups [][]string) {
	o.transport.SetPartitions(groups)
}

// HealPartition removes all network partitions.
func (o *Orchestrator) HealPartition() {
	o.transport.ClearPartitions()
}

func (o *Orchestrator) PartitionGroups() [][]string {
	return o.transport.Partitions()
}

func (o *Orchestrator) SetLinkLatency(from, to string, d time.Duration) {
	o.transport.SetLinkLatency(from, to, d)
}

func (o *Orchestrator) ClearLinkLatencies() {
	o.transport.ClearLinkLatencies()
}

func (o *Orchestrator) LinkLatencies() []transport.LinkLatency {
	return o.transport.LinkLatencies()
}

// UpdateConfig replaces the config (some fields take effect immediately).
func (o *Orchestrator) UpdateConfig(cfg *Config) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.cfg = cloneConfig(cfg)
	o.quorum.Config = &replication.Config{
		N: cfg.ReplicationN,
		W: cfg.WriteQuorum,
		R: cfg.ReadQuorum,
	}
	o.transport.SetSimulation(cfg.SimDelay, cfg.SimLossRate)
}

// Reset stops all nodes and clears all state, returning the network to an empty state.
func (o *Orchestrator) Reset() {
	o.mu.Lock()
	defer o.mu.Unlock()

	for _, cn := range o.chordNodes {
		cn.Stop()
	}
	for _, kn := range o.kadNodes {
		kn.Stop()
	}
	for addr := range o.nodes {
		o.transport.Deregister(addr)
	}
	o.nodes = make(map[string]Node)
	o.chordNodes = make(map[string]*chord.ChordNode)
	o.kadNodes = make(map[string]*kademlia.KademliaNode)
	o.kadGossip = make(map[string]*gossip.AntiEntropy)
	o.nodeAddrs = nil
	o.keyStrings = make(map[[20]byte]string)
	o.portCounter = 7000
	o.transport.ClearPartitions()
	o.transport.ClearLinkLatencies()
}

// DeleteKey removes a key from all replica nodes' stores.
func (o *Orchestrator) DeleteKey(key string) error {
	keyID := consistent.KeyID(key)
	o.mu.Lock()
	defer o.mu.Unlock()

	deleted := false
	for _, cn := range o.chordNodes {
		if cn.Store.Delete(keyID) {
			deleted = true
		}
	}
	for _, kn := range o.kadNodes {
		if kn.KVStore.Delete(keyID) {
			deleted = true
		}
	}
	delete(o.keyStrings, keyID)
	if !deleted {
		return fmt.Errorf("orchestrator: key %s not found", consistent.IDToHex(keyID))
	}
	return nil
}

// ReplicaEntry is a single replica: which node holds it and the full value entry.
type ReplicaEntry struct {
	NodeID string
	Entry  *store.ValueEntry
}

// GetKeyReplicas returns the full ValueEntry for each replica of a key.
func (o *Orchestrator) GetKeyReplicas(key string) []ReplicaEntry {
	keyID := consistent.KeyID(key)
	o.mu.RLock()
	defer o.mu.RUnlock()

	var results []ReplicaEntry
	for _, cn := range o.chordNodes {
		if entry, ok := cn.Store.Get(keyID); ok {
			results = append(results, ReplicaEntry{
				NodeID: consistent.IDToHex(cn.ID),
				Entry:  entry,
			})
		}
	}
	for _, kn := range o.kadNodes {
		if entry, ok := kn.KVStore.Get(keyID); ok {
			results = append(results, ReplicaEntry{
				NodeID: consistent.IDToHex([20]byte(kn.ID)),
				Entry:  entry,
			})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].NodeID < results[j].NodeID
	})
	return results
}

// Transport returns the shared in-process transport.
func (o *Orchestrator) Transport() *transport.InProcessTransport {
	return o.transport
}

// Bus returns the event bus.
func (o *Orchestrator) Bus() *events.EventBus {
	return o.bus
}

// NodeCount returns current node count.
func (o *Orchestrator) NodeCount() int {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return len(o.nodes)
}

// Protocol returns the current DHT protocol name ("chord" or "kademlia").
func (o *Orchestrator) Protocol() string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.cfg.Protocol
}

// chordNodeState builds a NodeState snapshot for a Chord node.
func chordNodeState(addr string, cn *chord.ChordNode) NodeState {
	state := NodeState{
		ID:       consistent.IDToHex(cn.ID),
		Addr:     addr,
		Protocol: "chord",
		Status:   "healthy",
		KeyCount: cn.Store.Size(),
	}
	succ := cn.Successor()
	if succ != nil {
		s := consistent.IDToHex(succ.ID)
		state.Successor = &s
	}
	pred := cn.Predecessor()
	if pred != nil {
		p := consistent.IDToHex(pred.ID)
		state.Predecessor = &p
	}
	for _, s := range cn.SuccessorList() {
		if s != nil {
			state.SuccList = append(state.SuccList, consistent.IDToHex(s.ID))
		}
	}
	// Finger table snapshot
	for i := 0; i < 160; i++ {
		f := cn.GetFinger(i)
		fe := FingerEntry{Index: i, Start: consistent.IDToHex(consistent.Add(cn.ID, consistent.PowerOfTwo(i)))}
		if f != nil {
			fe.NodeID = consistent.IDToHex(f.ID)
			fe.Addr = f.Addr
		}
		state.FingerTable = append(state.FingerTable, fe)
	}
	return state
}

// kadNodeState builds a NodeState snapshot for a Kademlia node.
func kadNodeState(addr string, kn *kademlia.KademliaNode) NodeState {
	state := NodeState{
		ID:       consistent.IDToHex([20]byte(kn.ID)),
		Addr:     addr,
		Protocol: "kademlia",
		Status:   "healthy",
		KeyCount: kn.KVStore.Size(),
	}
	for i := range kn.RoutingTable.Buckets {
		contacts := kn.RoutingTable.Buckets[i].Entries()
		cs := make([]ContactState, len(contacts))
		for j, c := range contacts {
			cs[j] = ContactState{
				ID:       consistent.IDToHex([20]byte(c.ID)),
				Addr:     c.Addr,
				LastSeen: c.LastSeen.Format(time.RFC3339),
			}
		}
		state.KBuckets = append(state.KBuckets, KBucketState{
			Index:    i,
			Contacts: cs,
			Capacity: kn.Config.K,
		})
	}
	return state
}

// ensure store import is used
var _ = store.NewKVStore

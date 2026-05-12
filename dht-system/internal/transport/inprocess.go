package transport

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/sanskarpan/dht-system/dht-system/internal/store"
)

// Sentinel errors for simulated failures.
var (
	ErrNodeNotFound  = errors.New("transport: node not registered")
	ErrSimulatedLoss = errors.New("transport: simulated packet loss")
	ErrPartitioned   = errors.New("transport: network partition")
)

// InProcessTransport dispatches RPCs by looking up node handlers in a registry.
// All RPCs are direct function calls (no serialization).
type InProcessTransport struct {
	mu            sync.RWMutex
	registry      map[string]NodeHandler              // addr -> handler
	delay         time.Duration                       // global per-call simulated latency
	lossRate      float64                             // 0.0-1.0 probability of simulated loss
	partitions    [][]string                          // groups of addrs; cross-group RPCs fail
	latencyMatrix map[string]map[string]time.Duration // from -> to -> per-link delay
}

type LinkLatency struct {
	From  string
	To    string
	Delay time.Duration
}

// NewInProcessTransport creates a new transport with optional delay/loss.
func NewInProcessTransport(delay time.Duration, lossRate float64) *InProcessTransport {
	return &InProcessTransport{
		registry:      make(map[string]NodeHandler),
		delay:         delay,
		lossRate:      lossRate,
		latencyMatrix: make(map[string]map[string]time.Duration),
	}
}

// SetSimulation updates the transport-wide delay and loss simulation.
func (t *InProcessTransport) SetSimulation(delay time.Duration, lossRate float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.delay = delay
	t.lossRate = lossRate
}

// SetLinkLatency sets a simulated per-link latency between two node addresses.
// This models geographic distance in the latency topology simulation.
// Call with d=0 to remove an existing link latency.
func (t *InProcessTransport) SetLinkLatency(from, to string, d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.latencyMatrix[from]; !ok {
		t.latencyMatrix[from] = make(map[string]time.Duration)
	}
	if d == 0 {
		delete(t.latencyMatrix[from], to)
	} else {
		t.latencyMatrix[from][to] = d
	}
}

// GetLinkLatency returns the configured per-link latency from → to (0 if not set).
func (t *InProcessTransport) GetLinkLatency(from, to string) time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if row, ok := t.latencyMatrix[from]; ok {
		return row[to]
	}
	return 0
}

// Register adds a node handler to the registry.
func (t *InProcessTransport) Register(addr string, handler NodeHandler) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.registry[addr] = handler
}

// Deregister removes a node handler from the registry.
func (t *InProcessTransport) Deregister(addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.registry, addr)
}

// SetPartitions defines network partition groups.
// Nodes in different groups cannot communicate.
func (t *InProcessTransport) SetPartitions(groups [][]string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.partitions = groups
}

// ClearPartitions removes all network partitions.
func (t *InProcessTransport) ClearPartitions() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.partitions = nil
}

func (t *InProcessTransport) Partitions() [][]string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.partitions) == 0 {
		return nil
	}

	result := make([][]string, len(t.partitions))
	for i, group := range t.partitions {
		result[i] = append([]string(nil), group...)
	}
	return result
}

func (t *InProcessTransport) ClearLinkLatencies() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.latencyMatrix = make(map[string]map[string]time.Duration)
}

func (t *InProcessTransport) LinkLatencies() []LinkLatency {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var links []LinkLatency
	for from, row := range t.latencyMatrix {
		for to, delay := range row {
			if delay <= 0 {
				continue
			}
			links = append(links, LinkLatency{
				From:  from,
				To:    to,
				Delay: delay,
			})
		}
	}
	return links
}

// lookup finds the handler for addr, applying delay and loss simulation.
func (t *InProcessTransport) lookup(addr string) (NodeHandler, error) {
	if t.delay > 0 {
		time.Sleep(t.delay)
	}
	if t.lossRate > 0 && rand.Float64() < t.lossRate {
		return nil, ErrSimulatedLoss
	}
	t.mu.RLock()
	h, ok := t.registry[addr]
	t.mu.RUnlock()
	if !ok {
		return nil, ErrNodeNotFound
	}
	return h, nil
}

// lookupFrom is like lookup but also checks partition rules and per-link latency.
func (t *InProcessTransport) lookupFrom(fromAddr, toAddr string) (NodeHandler, error) {
	t.mu.RLock()
	partitions := t.partitions
	var linkDelay time.Duration
	if row, ok := t.latencyMatrix[fromAddr]; ok {
		linkDelay = row[toAddr]
	}
	t.mu.RUnlock()

	if len(partitions) > 0 && fromAddr != "" {
		fromGroup := -1
		toGroup := -1
		for gi, group := range partitions {
			for _, addr := range group {
				if addr == fromAddr {
					fromGroup = gi
				}
				if addr == toAddr {
					toGroup = gi
				}
			}
		}
		// If both are in partition groups and in different groups -> blocked
		if fromGroup >= 0 && toGroup >= 0 && fromGroup != toGroup {
			return nil, ErrPartitioned
		}
	}

	// Apply per-link latency (additive on top of global delay).
	if linkDelay > 0 {
		time.Sleep(linkDelay)
	}

	return t.lookup(toAddr)
}

func (t *InProcessTransport) lookupForContext(ctx context.Context, toAddr string) (NodeHandler, error) {
	if sender, ok := senderFromContext(ctx); ok {
		return t.lookupFrom(sender.Addr, toAddr)
	}
	return t.lookup(toAddr)
}

// --- Chord RPCs ---

func (t *InProcessTransport) FindSuccessor(ctx context.Context, target NodeRef, id [20]byte) (NodeRef, error) {
	h, err := t.lookupForContext(ctx, target.Addr)
	if err != nil {
		return NodeRef{}, err
	}
	return h.HandleFindSuccessor(ctx, id)
}

func (t *InProcessTransport) GetPredecessor(ctx context.Context, target NodeRef) (*NodeRef, error) {
	h, err := t.lookupForContext(ctx, target.Addr)
	if err != nil {
		return nil, err
	}
	return h.HandleGetPredecessor(ctx)
}

func (t *InProcessTransport) Notify(ctx context.Context, target NodeRef, sender NodeRef) error {
	h, err := t.lookupFrom(sender.Addr, target.Addr)
	if err != nil {
		return err
	}
	return h.HandleNotify(ctx, sender)
}

func (t *InProcessTransport) GetSuccessorList(ctx context.Context, target NodeRef) ([]NodeRef, error) {
	h, err := t.lookupForContext(ctx, target.Addr)
	if err != nil {
		return nil, err
	}
	return h.HandleGetSuccessorList(ctx)
}

func (t *InProcessTransport) TransferKeys(ctx context.Context, target NodeRef, entries []*store.ValueEntry) error {
	h, err := t.lookupForContext(ctx, target.Addr)
	if err != nil {
		return err
	}
	return h.HandleTransferKeys(ctx, entries)
}

func (t *InProcessTransport) UpdateFingerTable(ctx context.Context, target NodeRef, s NodeRef, i int) error {
	h, err := t.lookupForContext(ctx, target.Addr)
	if err != nil {
		return err
	}
	return h.HandleUpdateFingerTable(ctx, s, i)
}

// --- Kademlia RPCs ---

func (t *InProcessTransport) KPing(ctx context.Context, sender NodeRef, target NodeRef) error {
	h, err := t.lookupFrom(sender.Addr, target.Addr)
	if err != nil {
		return err
	}
	return h.HandleKPing(ctx, sender)
}

func (t *InProcessTransport) KStore(ctx context.Context, sender NodeRef, target NodeRef, entry *store.ValueEntry) error {
	h, err := t.lookupFrom(sender.Addr, target.Addr)
	if err != nil {
		return err
	}
	return h.HandleKStore(ctx, sender, entry)
}

func (t *InProcessTransport) FindNode(ctx context.Context, sender NodeRef, target NodeRef, id [20]byte) ([]NodeRef, error) {
	h, err := t.lookupFrom(sender.Addr, target.Addr)
	if err != nil {
		return nil, err
	}
	return h.HandleFindNode(ctx, sender, id)
}

func (t *InProcessTransport) FindValue(ctx context.Context, sender NodeRef, target NodeRef, key [20]byte) (*FindValueResult, error) {
	h, err := t.lookupFrom(sender.Addr, target.Addr)
	if err != nil {
		return nil, err
	}
	return h.HandleFindValue(ctx, sender, key)
}

// --- Common RPCs ---

func (t *InProcessTransport) Ping(ctx context.Context, target NodeRef) error {
	h, err := t.lookupForContext(ctx, target.Addr)
	if err != nil {
		return err
	}
	return h.HandlePing(ctx)
}

func (t *InProcessTransport) GetEntry(ctx context.Context, target NodeRef, key [20]byte) (*store.ValueEntry, error) {
	h, err := t.lookupForContext(ctx, target.Addr)
	if err != nil {
		return nil, err
	}
	return h.HandleGetEntry(ctx, key)
}

func (t *InProcessTransport) PutEntry(ctx context.Context, target NodeRef, key [20]byte, entry *store.ValueEntry) error {
	h, err := t.lookupForContext(ctx, target.Addr)
	if err != nil {
		return err
	}
	return h.HandlePutEntry(ctx, key, entry)
}

func (t *InProcessTransport) GetAllEntries(ctx context.Context, target NodeRef) ([]*store.ValueEntry, error) {
	h, err := t.lookupForContext(ctx, target.Addr)
	if err != nil {
		return nil, err
	}
	return h.HandleGetAllEntries(ctx)
}

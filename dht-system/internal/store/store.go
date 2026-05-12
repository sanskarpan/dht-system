// Package store provides the in-memory key-value store with TTL eviction and vector clock support.
package store

import (
	"context"
	"math/big"
	"sync"
	"time"
)

// ValueEntry is a stored key-value pair with vector clock for conflict resolution.
type ValueEntry struct {
	Key       [20]byte
	Value     []byte
	Clock     map[string]uint64 // VectorClock: nodeID -> counter
	Timestamp time.Time
	TTL       time.Duration // 0 means no expiry
	NodeID    string        // hex ID of last writer
}

// Clone returns a deep copy of the ValueEntry.
func (e *ValueEntry) Clone() *ValueEntry {
	if e == nil {
		return nil
	}
	c := &ValueEntry{
		Key:       e.Key,
		Timestamp: e.Timestamp,
		TTL:       e.TTL,
		NodeID:    e.NodeID,
	}
	if e.Value != nil {
		c.Value = make([]byte, len(e.Value))
		copy(c.Value, e.Value)
	}
	if e.Clock != nil {
		c.Clock = make(map[string]uint64, len(e.Clock))
		for k, v := range e.Clock {
			c.Clock[k] = v
		}
	}
	return c
}

// IsExpired returns true if the entry has a TTL and it has elapsed.
func (e *ValueEntry) IsExpired() bool {
	if e.TTL == 0 {
		return false
	}
	return time.Since(e.Timestamp) > e.TTL
}

// KVStore is a thread-safe in-memory key-value store.
type KVStore struct {
	mu      sync.RWMutex
	entries map[[20]byte]*ValueEntry
}

// NewKVStore creates a new empty KVStore.
func NewKVStore() *KVStore {
	return &KVStore{
		entries: make(map[[20]byte]*ValueEntry),
	}
}

// Put stores an entry. If an entry with the same key exists, it is replaced.
func (s *KVStore) Put(entry *ValueEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[entry.Key] = entry.Clone()
}

// Get retrieves an entry by key. Returns (nil, false) if not found or expired.
func (s *KVStore) Get(key [20]byte) (*ValueEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[key]
	if !ok {
		return nil, false
	}
	if e.IsExpired() {
		return nil, false
	}
	return e.Clone(), true
}

// Delete removes a key. Returns true if the key existed.
func (s *KVStore) Delete(key [20]byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.entries[key]
	if ok {
		delete(s.entries, key)
	}
	return ok
}

// GetInRange returns all entries whose keys fall in the half-open interval (start, end]
// on the hash ring. Uses ring-aware comparison (wraps around 2^160).
// This is used during key migration on node join/leave.
func (s *KVStore) GetInRange(start, end [20]byte) []*ValueEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*ValueEntry
	for key, entry := range s.entries {
		if entry.IsExpired() {
			continue
		}
		if isInRangeRightClosed(key, start, end) {
			result = append(result, entry.Clone())
		}
	}
	return result
}

// DeleteInRange removes all entries in (start, end] and returns them.
func (s *KVStore) DeleteInRange(start, end [20]byte) []*ValueEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	var result []*ValueEntry
	for key, entry := range s.entries {
		if isInRangeRightClosed(key, start, end) {
			result = append(result, entry.Clone())
			delete(s.entries, key)
		}
	}
	return result
}

// All returns a snapshot of all non-expired entries.
func (s *KVStore) All() []*ValueEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*ValueEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		if !entry.IsExpired() {
			result = append(result, entry.Clone())
		}
	}
	return result
}

// Size returns the number of entries (including potentially expired ones).
func (s *KVStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// StartTTLEviction runs a background goroutine that evicts expired entries
// every evictInterval. Stops when ctx is cancelled.
func (s *KVStore) StartTTLEviction(ctx context.Context, evictInterval time.Duration) {
	go func() {
		ticker := time.NewTicker(evictInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.evictExpired()
			}
		}
	}()
}

func (s *KVStore) evictExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, entry := range s.entries {
		if entry.IsExpired() {
			delete(s.entries, key)
		}
	}
}

// isInRangeRightClosed checks key ∈ (start, end] on the ring (handles wrap-around).
func isInRangeRightClosed(key, start, end [20]byte) bool {
	if key == end {
		return true
	}
	// If start == end (and key != end), the interval is effectively empty.
	if start == end {
		return false
	}
	keyBig := toBigInt(key)
	startBig := toBigInt(start)
	endBig := toBigInt(end)

	if startBig.Cmp(endBig) < 0 {
		// Normal interval: start < end, key must satisfy start < key < end.
		return keyBig.Cmp(startBig) > 0 && keyBig.Cmp(endBig) < 0
	}
	// Wrap-around interval: key > start OR key < end.
	return keyBig.Cmp(startBig) > 0 || keyBig.Cmp(endBig) < 0
}

// toBigInt converts a [20]byte (big-endian) to a *big.Int.
func toBigInt(b [20]byte) *big.Int {
	return new(big.Int).SetBytes(b[:])
}

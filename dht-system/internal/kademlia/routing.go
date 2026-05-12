package kademlia

import (
	"math/big"
	"sync"
	"time"
)

// NodeID is a 20-byte Kademlia node/key identifier.
type NodeID [20]byte

// XORDistance returns the XOR distance between two NodeIDs as a *big.Int.
func XORDistance(a, b NodeID) *big.Int {
	result := make([]byte, 20)
	for i := range a {
		result[i] = a[i] ^ b[i]
	}
	return new(big.Int).SetBytes(result)
}

// BucketIndex computes which k-bucket a contact belongs to, from the perspective of self.
// Returns 159 - clz(self XOR contact), where clz = count of leading zeros.
// Returns -1 if contact == self (do not add self to routing table).
func BucketIndex(self, contact NodeID) int {
	xor := XORDistance(self, contact)
	if xor.Sign() == 0 {
		return -1 // same node
	}
	// BitLen gives the position of the highest set bit (1-indexed)
	// clz = 160 - BitLen(xor)
	// bucket = 159 - clz = 159 - (160 - BitLen) = BitLen - 1
	return xor.BitLen() - 1
}

// Contact represents a known Kademlia peer.
type Contact struct {
	ID       NodeID
	Addr     string
	LastSeen time.Time
}

// KBucket is a list of up to k contacts ordered least-recently-seen first.
type KBucket struct {
	mu       sync.Mutex
	entries  []Contact
	capacity int // k
}

// UpdateContact inserts or moves a contact in the bucket.
// If bucket is full, pings the oldest entry using the provided ping function.
// If ping fails, evicts oldest and adds new; if ping succeeds, keeps oldest (prefer live old nodes).
func (b *KBucket) UpdateContact(c Contact, ping func(Contact) error) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Check if already present
	for i, existing := range b.entries {
		if existing.ID == c.ID {
			// Move to tail (most-recently-seen)
			b.entries = append(b.entries[:i], b.entries[i+1:]...)
			c.LastSeen = time.Now()
			b.entries = append(b.entries, c)
			return true
		}
	}

	// Not present
	if len(b.entries) < b.capacity {
		c.LastSeen = time.Now()
		b.entries = append(b.entries, c)
		return true
	}

	// Bucket full: ping oldest (entries[0])
	oldest := b.entries[0]
	if ping != nil && ping(oldest) == nil {
		// Oldest is alive: move to tail, discard new contact
		b.entries = append(b.entries[1:], oldest)
	} else {
		// Oldest is dead: evict, add new
		b.entries = append(b.entries[1:], c)
		return true
	}
	return false
}

// Entries returns a copy of all contacts in this bucket (least-recently-seen first).
func (b *KBucket) Entries() []Contact {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := make([]Contact, len(b.entries))
	copy(cp, b.entries)
	return cp
}

// Len returns the number of contacts in the bucket.
func (b *KBucket) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.entries)
}

// RoutingTable holds 160 k-buckets indexed by XOR distance.
type RoutingTable struct {
	Self    NodeID
	Buckets [160]KBucket
	K       int // bucket capacity, default 20
}

// NewRoutingTable creates a routing table for self with bucket capacity k.
func NewRoutingTable(self NodeID, k int) *RoutingTable {
	rt := &RoutingTable{Self: self, K: k}
	for i := range rt.Buckets {
		rt.Buckets[i].capacity = k
	}
	return rt
}

// UpdateContact adds or updates a contact in the appropriate k-bucket.
// The ping function is called when a bucket is full.
func (rt *RoutingTable) UpdateContact(c Contact, ping func(Contact) error) (int, bool) {
	idx := BucketIndex(rt.Self, c.ID)
	if idx < 0 {
		return -1, false // don't add self
	}
	return idx, rt.Buckets[idx].UpdateContact(c, ping)
}

// KClosest returns the k contacts closest to target (sorted by XOR distance ascending).
func (rt *RoutingTable) KClosest(target NodeID, k int) []Contact {
	// Collect all contacts from all buckets
	var all []Contact
	for i := range rt.Buckets {
		all = append(all, rt.Buckets[i].Entries()...)
	}

	// Sort by XOR distance to target
	sortByDistance(all, target)

	if k > len(all) {
		k = len(all)
	}
	return all[:k]
}

// AllContacts returns all known contacts across all buckets.
func (rt *RoutingTable) AllContacts() []Contact {
	var all []Contact
	for i := range rt.Buckets {
		all = append(all, rt.Buckets[i].Entries()...)
	}
	return all
}

// SplitBucket splits the bucket at index into two halves by re-evaluating BucketIndex.
// Contacts whose BucketIndex equals splitIdx stay; those with a different index are
// re-inserted into their correct bucket. This is used when the own-ID bucket overflows.
func (rt *RoutingTable) SplitBucket(splitIdx int) {
	if splitIdx < 0 || splitIdx >= 160 {
		return
	}
	entries := rt.Buckets[splitIdx].Entries()
	rt.Buckets[splitIdx].mu.Lock()
	rt.Buckets[splitIdx].entries = nil
	rt.Buckets[splitIdx].mu.Unlock()

	for _, c := range entries {
		newIdx := BucketIndex(rt.Self, c.ID)
		if newIdx >= 0 && newIdx < 160 {
			rt.Buckets[newIdx].UpdateContact(c, nil)
		}
	}
}

// sortByDistance sorts contacts by XOR distance to target (ascending).
func sortByDistance(contacts []Contact, target NodeID) {
	// Simple insertion sort (typically small N)
	for i := 1; i < len(contacts); i++ {
		key := contacts[i]
		keyDist := XORDistance(key.ID, target)
		j := i - 1
		for j >= 0 && XORDistance(contacts[j].ID, target).Cmp(keyDist) > 0 {
			contacts[j+1] = contacts[j]
			j--
		}
		contacts[j+1] = key
	}
}

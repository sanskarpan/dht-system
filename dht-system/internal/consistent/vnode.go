// Package consistent - this file contains VNodeRing, a consistent-hashing virtual node ring
// implementation. VNodeRing is used in tests and benchmarks only; the simulation uses
// the Chord/Kademlia protocols directly for ring management.
package consistent

import (
	"fmt"
	"sort"
	"sync"
)

// VNodeID derives a virtual node ID for a physical node at a given index.
func VNodeID(addr string, index int) [20]byte {
	return SHA1(fmt.Sprintf("%s:%d", addr, index))
}

// VNodeEntry represents one virtual node on the ring.
type VNodeEntry struct {
	VID      [20]byte // virtual node ID
	PhysAddr string   // physical node address
	Index    int      // vnode index (0..V-1)
}

// VNodeRing manages virtual node positions for multiple physical nodes.
// It maintains a sorted ring of virtual node IDs → physical nodes.
type VNodeRing struct {
	mu      sync.RWMutex
	entries []VNodeEntry // sorted by VID
	vcount  int          // virtual nodes per physical node
}

// NewVNodeRing creates a new virtual node ring with the given vnodes-per-node count.
func NewVNodeRing(vnodesPerNode int) *VNodeRing {
	return &VNodeRing{vcount: vnodesPerNode}
}

// AddNode registers a physical node and all its virtual nodes on the ring.
func (r *VNodeRing) AddNode(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := 0; i < r.vcount; i++ {
		vid := VNodeID(addr, i)
		r.entries = append(r.entries, VNodeEntry{VID: vid, PhysAddr: addr, Index: i})
	}
	// Re-sort after insertion
	sort.Slice(r.entries, func(i, j int) bool {
		return Compare(r.entries[i].VID, r.entries[j].VID) < 0
	})
}

// RemoveNode deregisters all virtual nodes for a physical address.
func (r *VNodeRing) RemoveNode(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	filtered := r.entries[:0]
	for _, e := range r.entries {
		if e.PhysAddr != addr {
			filtered = append(filtered, e)
		}
	}
	r.entries = filtered
}

// FindOwner returns the physical node address responsible for the given key ID.
// It finds the first virtual node clockwise from keyID (i.e., the successor).
func (r *VNodeRing) FindOwner(keyID [20]byte) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.entries) == 0 {
		return "", false
	}

	// Binary search: find first entry where entry.VID >= keyID
	idx := sort.Search(len(r.entries), func(i int) bool {
		return Compare(r.entries[i].VID, keyID) >= 0
	})

	// Wrap around if past the end
	if idx == len(r.entries) {
		idx = 0
	}
	return r.entries[idx].PhysAddr, true
}

// LoadBalance returns the number of virtual node slots per physical node.
// Used for load balance analysis.
func (r *VNodeRing) LoadBalance() map[string]int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	counts := make(map[string]int)
	for _, e := range r.entries {
		counts[e.PhysAddr]++
	}
	return counts
}

// VNodeCount returns the total number of virtual nodes on the ring.
func (r *VNodeRing) VNodeCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}

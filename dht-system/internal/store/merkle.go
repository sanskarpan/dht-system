package store

import (
	"crypto/sha1"
	"sort"
	"sync"
)

// MerkleTree is a binary Merkle tree over a sorted set of [20]byte key-value pairs.
// Each leaf holds SHA-1(key || value). Internal nodes hold SHA-1(left || right).
// It is safe for concurrent use.
type MerkleTree struct {
	mu   sync.RWMutex
	data map[[20]byte][]byte // key → raw value bytes
	root [20]byte
	// flat sorted keys (rebuilt on mutation)
	keys [][20]byte
}

// NewMerkleTree creates an empty Merkle tree.
func NewMerkleTree() *MerkleTree {
	return &MerkleTree{data: make(map[[20]byte][]byte)}
}

// Update inserts or replaces the value for the given key and recomputes the root.
func (m *MerkleTree) Update(key [20]byte, value []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.data[key]; !exists {
		m.keys = append(m.keys, key)
		sort.Slice(m.keys, func(i, j int) bool {
			return compareIDs(m.keys[i], m.keys[j]) < 0
		})
	}
	cp := make([]byte, len(value))
	copy(cp, value)
	m.data[key] = cp
	m.root = m.buildRoot()
}

// Delete removes a key from the tree and recomputes the root.
func (m *MerkleTree) Delete(key [20]byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.data[key]; !ok {
		return
	}
	delete(m.data, key)
	filtered := m.keys[:0]
	for _, k := range m.keys {
		if k != key {
			filtered = append(filtered, k)
		}
	}
	m.keys = filtered
	m.root = m.buildRoot()
}

// Root returns the current Merkle root hash.
func (m *MerkleTree) Root() [20]byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.root
}

// Diff returns the leaf keys that differ between m and other.
// When both trees have identical key universes, uses O(log N) binary descent.
// Falls back to O(N) linear scan for trees with different key sets.
// Keys present in one but not the other are always returned as differing.
func (m *MerkleTree) Diff(other *MerkleTree) [][20]byte {
	m.mu.RLock()
	other.mu.RLock()
	defer m.mu.RUnlock()
	defer other.mu.RUnlock()

	if m.root == other.root {
		return nil
	}

	// If both trees have identical key universes, use O(log N) tree descent.
	if len(m.keys) == len(other.keys) && m.keysEqual(other) {
		levelsA := buildTreeLevels(m.keys, m.data)
		levelsB := buildTreeLevels(other.keys, other.data)
		if levelsA == nil || levelsB == nil {
			return nil
		}
		var diffIndices []int
		descendDiff(levelsA, levelsB, len(levelsA)-1, 0, &diffIndices, len(m.keys))
		result := make([][20]byte, 0, len(diffIndices))
		for _, idx := range diffIndices {
			result = append(result, m.keys[idx])
		}
		return result
	}

	// Different key universes: fall back to O(N) linear scan.
	keySet := make(map[[20]byte]struct{})
	for _, k := range m.keys {
		keySet[k] = struct{}{}
	}
	for _, k := range other.keys {
		keySet[k] = struct{}{}
	}

	var diff [][20]byte
	for k := range keySet {
		vA := m.data[k]
		vB := other.data[k]
		if leafHash(k, vA) != leafHash(k, vB) {
			diff = append(diff, k)
		}
	}
	sort.Slice(diff, func(i, j int) bool {
		return compareIDs(diff[i], diff[j]) < 0
	})
	return diff
}

// keysEqual returns true if m.keys and other.keys are identical (caller holds both read locks).
func (m *MerkleTree) keysEqual(other *MerkleTree) bool {
	for i, k := range m.keys {
		if k != other.keys[i] {
			return false
		}
	}
	return true
}

// buildTreeLevels computes all Merkle tree levels bottom-up from sorted keys and data.
// levels[0] = leaf hashes, levels[top] has a single root hash.
// Caller must hold the relevant mu.
func buildTreeLevels(keys [][20]byte, data map[[20]byte][]byte) [][][20]byte {
	if len(keys) == 0 {
		return nil
	}
	leaves := make([][20]byte, len(keys))
	for i, k := range keys {
		leaves[i] = leafHash(k, data[k])
	}
	levels := [][][20]byte{append([][20]byte{}, leaves...)}
	current := leaves
	for len(current) > 1 {
		if len(current)%2 != 0 {
			current = append(current, current[len(current)-1])
		}
		parents := make([][20]byte, len(current)/2)
		for i := 0; i < len(current); i += 2 {
			parents[i/2] = internalHash(current[i], current[i+1])
		}
		levels = append(levels, append([][20]byte{}, parents...))
		current = parents
	}
	return levels
}

// descendDiff recursively descends Merkle tree levels and collects differing leaf indices.
// levelIdx is the level to check (0=leaves, top=root). nodeIdx is the index within that level.
func descendDiff(levelsA, levelsB [][][20]byte, levelIdx, nodeIdx int, result *[]int, maxLeaves int) {
	if levelIdx < 0 || nodeIdx >= len(levelsA[levelIdx]) || nodeIdx >= len(levelsB[levelIdx]) {
		return
	}
	if levelsA[levelIdx][nodeIdx] == levelsB[levelIdx][nodeIdx] {
		return // subtree is identical
	}
	if levelIdx == 0 {
		// Leaf node — report it if within actual (non-padded) range.
		if nodeIdx < maxLeaves {
			*result = append(*result, nodeIdx)
		}
		return
	}
	// Descend into left and right children.
	descendDiff(levelsA, levelsB, levelIdx-1, nodeIdx*2, result, maxLeaves)
	descendDiff(levelsA, levelsB, levelIdx-1, nodeIdx*2+1, result, maxLeaves)
}

// Len returns the number of keys in the tree.
func (m *MerkleTree) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.keys)
}

// buildRoot recomputes the Merkle root from the current data.
// Caller must hold m.mu (write).
func (m *MerkleTree) buildRoot() [20]byte {
	if len(m.keys) == 0 {
		return [20]byte{}
	}
	leaves := make([][20]byte, len(m.keys))
	for i, k := range m.keys {
		leaves[i] = leafHash(k, m.data[k])
	}
	return merkleBuild(leaves)
}

// merkleBuild recursively builds the tree over a slice of hashes.
func merkleBuild(hashes [][20]byte) [20]byte {
	if len(hashes) == 1 {
		return hashes[0]
	}
	// Pad to even length by duplicating last element
	if len(hashes)%2 != 0 {
		hashes = append(hashes, hashes[len(hashes)-1])
	}
	parents := make([][20]byte, len(hashes)/2)
	for i := 0; i < len(hashes); i += 2 {
		parents[i/2] = internalHash(hashes[i], hashes[i+1])
	}
	return merkleBuild(parents)
}

func leafHash(key [20]byte, value []byte) [20]byte {
	h := sha1.New()
	h.Write(key[:])
	h.Write(value)
	var out [20]byte
	copy(out[:], h.Sum(nil))
	return out
}

func internalHash(left, right [20]byte) [20]byte {
	h := sha1.New()
	h.Write(left[:])
	h.Write(right[:])
	var out [20]byte
	copy(out[:], h.Sum(nil))
	return out
}

func compareIDs(a, b [20]byte) int {
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

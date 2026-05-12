// Package store — CRDT types for conflict-free replicated data.
// GCounter: grow-only counter, merge = pairwise max.
// ORSet: observed-remove set with unique tags per element.
//
// NOTE: GCounter and ORSet are fully implemented but are NOT integrated into
// the main replication or store layer. They are reserved as a future extension
// for use cases requiring strict CRDT semantics (e.g., distributed counters,
// presence tracking). The primary store uses vector-clock-based LWW conflict
// resolution instead.
package store

import "sort"

// ---------------------------------------------------------------------------
// GCounter — grow-only counter
//
// A GCounter is represented as a map from nodeID to that node's local
// increment count. The global value is the sum of all per-node counts.
// Merging two GCounters takes the pairwise maximum for every nodeID, which
// is safe because counts only ever grow.
// ---------------------------------------------------------------------------

// GCounter maps a nodeID to its monotonically increasing count.
type GCounter map[string]uint64

// NewGCounter returns an empty, initialised GCounter.
func NewGCounter() GCounter {
	return make(GCounter)
}

// Increment atomically increments the counter for the given nodeID.
func (g GCounter) Increment(nodeID string) {
	g[nodeID]++
}

// Value returns the total count across all nodes (sum of per-node counts).
func (g GCounter) Value() uint64 {
	var total uint64
	for _, v := range g {
		total += v
	}
	return total
}

// Merge returns a new GCounter that is the pairwise maximum of g and other.
// Neither g nor other is modified.
func (g GCounter) Merge(other GCounter) GCounter {
	merged := g.Clone()
	for nodeID, count := range other {
		if existing, ok := merged[nodeID]; !ok || count > existing {
			merged[nodeID] = count
		}
	}
	return merged
}

// Clone returns a deep copy of the GCounter.
func (g GCounter) Clone() GCounter {
	c := make(GCounter, len(g))
	for k, v := range g {
		c[k] = v
	}
	return c
}

// ---------------------------------------------------------------------------
// ORSet — observed-remove set
//
// An ORSet (Observed-Remove Set) tracks elements with unique per-add tags.
// When an element is added a unique tag is associated with it. Removing an
// element moves all currently known tags into the RemoveSet. An element is
// considered present when it has at least one tag in AddSet that is NOT also
// in RemoveSet. This gives add-wins semantics on concurrent operations: a
// concurrent add with a fresh tag beats a concurrent remove that only knew
// about older tags.
// ---------------------------------------------------------------------------

// ORSet is an observed-remove CRDT set.
type ORSet struct {
	// AddSet[element] = set of unique tags that were used to add element.
	AddSet map[string]map[string]bool
	// RemoveSet[element] = set of tags that were observed at remove time.
	RemoveSet map[string]map[string]bool
}

// NewORSet returns a new, empty ORSet.
func NewORSet() *ORSet {
	return &ORSet{
		AddSet:    make(map[string]map[string]bool),
		RemoveSet: make(map[string]map[string]bool),
	}
}

// Add associates tag with element in AddSet, recording the addition.
// Each call should use a unique tag (e.g. a UUID) to distinguish
// concurrent additions from one another.
func (s *ORSet) Add(element, tag string) {
	if s.AddSet[element] == nil {
		s.AddSet[element] = make(map[string]bool)
	}
	s.AddSet[element][tag] = true
}

// Remove copies all currently known add-tags for element into RemoveSet,
// effectively tombstoning them. Tags added concurrently with a higher
// logical timestamp (i.e. not yet observed) are unaffected, so a
// concurrent Add with a new tag survives the Remove.
func (s *ORSet) Remove(element string) {
	tags, ok := s.AddSet[element]
	if !ok {
		return
	}
	if s.RemoveSet[element] == nil {
		s.RemoveSet[element] = make(map[string]bool)
	}
	for tag := range tags {
		s.RemoveSet[element][tag] = true
	}
}

// Contains returns true when element has at least one add-tag that has not
// been tombstoned in RemoveSet.
func (s *ORSet) Contains(element string) bool {
	addTags, ok := s.AddSet[element]
	if !ok {
		return false
	}
	removeTags := s.RemoveSet[element] // may be nil
	for tag := range addTags {
		if !removeTags[tag] {
			return true // at least one live tag
		}
	}
	return false
}

// Elements returns a sorted slice of all elements currently present in the
// set (i.e. elements for which Contains returns true).
func (s *ORSet) Elements() []string {
	var elems []string
	for element := range s.AddSet {
		if s.Contains(element) {
			elems = append(elems, element)
		}
	}
	sort.Strings(elems)
	return elems
}

// Merge returns a new ORSet that is the join of s and other.
// The join takes the union of both AddSets and the union of both RemoveSets.
// Neither s nor other is modified.
func (s *ORSet) Merge(other *ORSet) *ORSet {
	result := NewORSet()

	// Union of AddSets.
	for elem, tags := range s.AddSet {
		result.AddSet[elem] = make(map[string]bool, len(tags))
		for tag := range tags {
			result.AddSet[elem][tag] = true
		}
	}
	for elem, tags := range other.AddSet {
		if result.AddSet[elem] == nil {
			result.AddSet[elem] = make(map[string]bool)
		}
		for tag := range tags {
			result.AddSet[elem][tag] = true
		}
	}

	// Union of RemoveSets.
	for elem, tags := range s.RemoveSet {
		result.RemoveSet[elem] = make(map[string]bool, len(tags))
		for tag := range tags {
			result.RemoveSet[elem][tag] = true
		}
	}
	for elem, tags := range other.RemoveSet {
		if result.RemoveSet[elem] == nil {
			result.RemoveSet[elem] = make(map[string]bool)
		}
		for tag := range tags {
			result.RemoveSet[elem][tag] = true
		}
	}

	return result
}

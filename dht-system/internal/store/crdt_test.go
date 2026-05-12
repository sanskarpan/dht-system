package store

import (
	"reflect"
	"testing"
)

// ==========================================================================
// GCounter tests
// ==========================================================================

// TestGCounterNewIsEmpty verifies that a freshly created GCounter has value 0.
func TestGCounterNewIsEmpty(t *testing.T) {
	g := NewGCounter()
	if got := g.Value(); got != 0 {
		t.Fatalf("expected Value() == 0 for new GCounter, got %d", got)
	}
}

// TestGCounterIncrementSingleNode checks that incrementing from one node
// accumulates correctly.
func TestGCounterIncrementSingleNode(t *testing.T) {
	g := NewGCounter()
	g.Increment("node-A")
	g.Increment("node-A")
	g.Increment("node-A")

	if got := g.Value(); got != 3 {
		t.Fatalf("expected Value() == 3, got %d", got)
	}
}

// TestGCounterIncrementMultipleNodes verifies that counts from different nodes
// sum correctly.
func TestGCounterIncrementMultipleNodes(t *testing.T) {
	g := NewGCounter()
	g.Increment("node-A") // A=1
	g.Increment("node-A") // A=2
	g.Increment("node-B") // B=1
	g.Increment("node-C") // C=1

	if got := g.Value(); got != 4 {
		t.Fatalf("expected Value() == 4, got %d", got)
	}
}

// TestGCounterMergePreservesPairwiseMax checks that Merge takes the pairwise
// maximum per nodeID.
func TestGCounterMergePreservesPairwiseMax(t *testing.T) {
	// Simulate two replicas that incremented independently.
	replica1 := NewGCounter()
	replica1.Increment("node-A") // A=1
	replica1.Increment("node-A") // A=2
	replica1.Increment("node-B") // B=1

	replica2 := NewGCounter()
	replica2.Increment("node-A") // A=1  (replica2 has fewer A increments)
	replica2.Increment("node-B") // B=1
	replica2.Increment("node-B") // B=2
	replica2.Increment("node-C") // C=1

	merged := replica1.Merge(replica2)

	// Pairwise max: A=max(2,1)=2, B=max(1,2)=2, C=max(0,1)=1  =>  total=5
	if got := merged.Value(); got != 5 {
		t.Fatalf("expected merged Value() == 5, got %d", got)
	}
	if merged["node-A"] != 2 {
		t.Errorf("expected node-A == 2, got %d", merged["node-A"])
	}
	if merged["node-B"] != 2 {
		t.Errorf("expected node-B == 2, got %d", merged["node-B"])
	}
	if merged["node-C"] != 1 {
		t.Errorf("expected node-C == 1, got %d", merged["node-C"])
	}
}

// TestGCounterMergeDoesNotMutateInputs verifies that Merge is non-destructive.
func TestGCounterMergeDoesNotMutateInputs(t *testing.T) {
	g1 := NewGCounter()
	g1.Increment("node-A")

	g2 := NewGCounter()
	g2.Increment("node-B")

	_ = g1.Merge(g2)

	// g1 and g2 should be unchanged.
	if _, ok := g1["node-B"]; ok {
		t.Error("Merge mutated g1 by adding node-B")
	}
	if _, ok := g2["node-A"]; ok {
		t.Error("Merge mutated g2 by adding node-A")
	}
}

// TestGCounterMergeIsCommutative checks that order of operands does not matter.
func TestGCounterMergeIsCommutative(t *testing.T) {
	g1 := NewGCounter()
	g1.Increment("node-A")
	g1.Increment("node-A")

	g2 := NewGCounter()
	g2.Increment("node-B")

	left := g1.Merge(g2)
	right := g2.Merge(g1)

	if !reflect.DeepEqual(map[string]uint64(left), map[string]uint64(right)) {
		t.Errorf("Merge not commutative: %v vs %v", left, right)
	}
}

// TestGCounterMergeIsIdempotent checks that merging with itself is a no-op.
func TestGCounterMergeIsIdempotent(t *testing.T) {
	g := NewGCounter()
	g.Increment("node-A")
	g.Increment("node-B")

	merged := g.Merge(g)
	if !reflect.DeepEqual(map[string]uint64(g), map[string]uint64(merged)) {
		t.Errorf("Merge not idempotent: original %v, merged %v", g, merged)
	}
}

// TestGCounterCloneIsDeepCopy checks that Clone produces an independent copy.
func TestGCounterCloneIsDeepCopy(t *testing.T) {
	g := NewGCounter()
	g.Increment("node-A")
	g.Increment("node-A")

	clone := g.Clone()
	clone.Increment("node-A") // mutate clone only

	if g["node-A"] != 2 {
		t.Errorf("Clone mutation leaked back to original: node-A = %d, want 2", g["node-A"])
	}
	if clone["node-A"] != 3 {
		t.Errorf("Clone increment did not apply: node-A = %d, want 3", clone["node-A"])
	}
}

// ==========================================================================
// ORSet tests
// ==========================================================================

// TestORSetNewIsEmpty verifies a freshly created ORSet contains no elements.
func TestORSetNewIsEmpty(t *testing.T) {
	s := NewORSet()
	if elems := s.Elements(); len(elems) != 0 {
		t.Fatalf("expected no elements, got %v", elems)
	}
}

// TestORSetAddContains checks that adding an element makes Contains return true.
func TestORSetAddContains(t *testing.T) {
	s := NewORSet()
	s.Add("apple", "tag-1")

	if !s.Contains("apple") {
		t.Error("expected Contains(\"apple\") == true after Add")
	}
	if s.Contains("banana") {
		t.Error("expected Contains(\"banana\") == false for unseen element")
	}
}

// TestORSetAddMultipleElements checks that each added element is tracked
// independently.
func TestORSetAddMultipleElements(t *testing.T) {
	s := NewORSet()
	s.Add("apple", "t1")
	s.Add("banana", "t2")
	s.Add("cherry", "t3")

	elems := s.Elements()
	if len(elems) != 3 {
		t.Fatalf("expected 3 elements, got %v", elems)
	}
	// Elements() must be sorted.
	want := []string{"apple", "banana", "cherry"}
	if !reflect.DeepEqual(elems, want) {
		t.Errorf("Elements() = %v, want %v", elems, want)
	}
}

// TestORSetRemoveMakesContainsFalse checks that after Remove the element
// is no longer present.
func TestORSetRemoveMakesContainsFalse(t *testing.T) {
	s := NewORSet()
	s.Add("apple", "tag-1")
	s.Remove("apple")

	if s.Contains("apple") {
		t.Error("expected Contains(\"apple\") == false after Remove")
	}
	if elems := s.Elements(); len(elems) != 0 {
		t.Errorf("expected empty Elements() after Remove, got %v", elems)
	}
}

// TestORSetRemoveNonExistentIsNoop checks that removing an element that was
// never added does not panic and leaves the set unchanged.
func TestORSetRemoveNonExistentIsNoop(t *testing.T) {
	s := NewORSet()
	s.Remove("ghost") // must not panic
	if s.Contains("ghost") {
		t.Error("ghost should not appear after removing a non-existent element")
	}
}

// TestORSetReAddAfterRemove checks that re-adding an element with a new tag
// after it has been removed makes it present again.
func TestORSetReAddAfterRemove(t *testing.T) {
	s := NewORSet()
	s.Add("apple", "tag-1")
	s.Remove("apple")
	s.Add("apple", "tag-2") // fresh tag, not tombstoned

	if !s.Contains("apple") {
		t.Error("expected Contains(\"apple\") == true after re-add with fresh tag")
	}
}

// TestORSetElementsSorted verifies that Elements() returns a lexicographically
// sorted slice.
func TestORSetElementsSorted(t *testing.T) {
	s := NewORSet()
	s.Add("zebra", "t1")
	s.Add("mango", "t2")
	s.Add("apple", "t3")

	elems := s.Elements()
	want := []string{"apple", "mango", "zebra"}
	if !reflect.DeepEqual(elems, want) {
		t.Errorf("Elements() = %v, want %v", elems, want)
	}
}

// TestORSetMergeUnionOfElements checks that merging two disjoint ORSets
// produces the union of their elements.
func TestORSetMergeUnionOfElements(t *testing.T) {
	s1 := NewORSet()
	s1.Add("apple", "t1")

	s2 := NewORSet()
	s2.Add("banana", "t2")

	merged := s1.Merge(s2)

	if !merged.Contains("apple") {
		t.Error("merged set should contain \"apple\"")
	}
	if !merged.Contains("banana") {
		t.Error("merged set should contain \"banana\"")
	}
}

// TestORSetMergeReconcilesConcurrentAddAndRemove tests the core ORSet
// semantic: if one replica removes an element while another concurrently
// adds it with the SAME tag set, the remove wins (all tags are tombstoned).
func TestORSetMergeReconcilesConcurrentAddAndRemove(t *testing.T) {
	// Both replicas start from the same state: "apple" added with "tag-1".
	base := NewORSet()
	base.Add("apple", "tag-1")

	// Replica A removes "apple" (tombstones "tag-1").
	replicaA := NewORSet()
	replicaA.Add("apple", "tag-1")
	replicaA.Remove("apple")

	// Replica B does nothing — it only knows about the original add.
	replicaB := NewORSet()
	replicaB.Add("apple", "tag-1")

	// Merge A's view into B's view.
	merged := replicaB.Merge(replicaA)

	// "apple" should NOT be present: tag-1 appears in both AddSet and
	// RemoveSet, so no live tag survives.
	if merged.Contains("apple") {
		t.Error("expected \"apple\" to be absent after merging remove (same-tag scenario)")
	}
}

// TestORSetMergeAddWinsWithDifferentTag is the canonical add-wins test:
// if a node adds an element with a NEW tag concurrently with another node
// removing it (using only the OLD tag), the element survives after merge.
func TestORSetMergeAddWinsWithDifferentTag(t *testing.T) {
	// Replica A: knows "apple" with tag-1, then removes it (tombstones tag-1).
	replicaA := NewORSet()
	replicaA.Add("apple", "tag-1")
	replicaA.Remove("apple") // tombstones tag-1

	// Replica B: also had tag-1 originally, but concurrently adds tag-2
	// (replica B does NOT yet know about A's remove).
	replicaB := NewORSet()
	replicaB.Add("apple", "tag-1")
	replicaB.Add("apple", "tag-2") // fresh concurrent add

	// After merge:
	// AddSet["apple"]    = {tag-1, tag-2}  (union)
	// RemoveSet["apple"] = {tag-1}         (only from A)
	// tag-2 is live  =>  Contains("apple") must be true.
	merged := replicaA.Merge(replicaB)
	if !merged.Contains("apple") {
		t.Error("add-wins: \"apple\" should be present because tag-2 was not tombstoned")
	}
}

// TestORSetMergeDoesNotMutateInputs verifies that Merge is non-destructive.
func TestORSetMergeDoesNotMutateInputs(t *testing.T) {
	s1 := NewORSet()
	s1.Add("apple", "t1")

	s2 := NewORSet()
	s2.Add("banana", "t2")
	s2.Remove("banana")

	_ = s1.Merge(s2)

	// s1 must not have learned about banana.
	if s1.Contains("banana") {
		t.Error("Merge mutated s1 by adding banana")
	}
	// s2 must not have learned about apple.
	if s2.Contains("apple") {
		t.Error("Merge mutated s2 by adding apple")
	}
}

// TestORSetMergeIsIdempotent verifies that merging a set with itself is a
// no-op (a required CRDT lattice property).
func TestORSetMergeIsIdempotent(t *testing.T) {
	s := NewORSet()
	s.Add("apple", "t1")
	s.Remove("apple")
	s.Add("apple", "t2")

	merged := s.Merge(s)

	if !reflect.DeepEqual(merged.AddSet, s.AddSet) {
		t.Errorf("Merge not idempotent: AddSets differ\n  original: %v\n  merged:   %v",
			s.AddSet, merged.AddSet)
	}
	if !reflect.DeepEqual(merged.RemoveSet, s.RemoveSet) {
		t.Errorf("Merge not idempotent: RemoveSets differ\n  original: %v\n  merged:   %v",
			s.RemoveSet, merged.RemoveSet)
	}
}

// TestORSetMergeIsCommutative verifies that s1.Merge(s2) == s2.Merge(s1).
func TestORSetMergeIsCommutative(t *testing.T) {
	s1 := NewORSet()
	s1.Add("apple", "t1")
	s1.Remove("apple")

	s2 := NewORSet()
	s2.Add("apple", "t2")
	s2.Add("banana", "t3")

	left := s1.Merge(s2)
	right := s2.Merge(s1)

	if !reflect.DeepEqual(left.AddSet, right.AddSet) {
		t.Errorf("Merge not commutative: AddSets differ\n  left:  %v\n  right: %v",
			left.AddSet, right.AddSet)
	}
	if !reflect.DeepEqual(left.RemoveSet, right.RemoveSet) {
		t.Errorf("Merge not commutative: RemoveSets differ\n  left:  %v\n  right: %v",
			left.RemoveSet, right.RemoveSet)
	}
}

// TestORSetMultipleTagsSameElement verifies that an element added multiple
// times (e.g. by different nodes) with distinct tags is present as long as
// at least one tag is live.
func TestORSetMultipleTagsSameElement(t *testing.T) {
	s := NewORSet()
	s.Add("apple", "tag-A")
	s.Add("apple", "tag-B")
	s.Add("apple", "tag-C")

	// Remove tombstones all three current tags.
	s.Remove("apple")

	if s.Contains("apple") {
		t.Error("apple should not be present after removing all tags")
	}

	// Adding with a new tag revives it.
	s.Add("apple", "tag-D")
	if !s.Contains("apple") {
		t.Error("apple should be present after re-add with tag-D")
	}
}

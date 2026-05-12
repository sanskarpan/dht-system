package replication

import (
	"reflect"
	"testing"
)

// ---- Compare ----

func TestCompare(t *testing.T) {
	tests := []struct {
		name string
		a    VectorClock
		b    VectorClock
		want ClockOrder
	}{
		// Basic single-key comparisons
		{
			name: "{A:1} vs {A:2} → Before",
			a:    VectorClock{"A": 1},
			b:    VectorClock{"A": 2},
			want: Before,
		},
		{
			name: "{A:2} vs {A:1} → After",
			a:    VectorClock{"A": 2},
			b:    VectorClock{"A": 1},
			want: After,
		},
		{
			name: "{A:1} vs {A:1} → Equal",
			a:    VectorClock{"A": 1},
			b:    VectorClock{"A": 1},
			want: Equal,
		},

		// Concurrent: each clock has a key the other is behind in
		{
			name: "{A:1,B:0} vs {A:0,B:1} → Concurrent",
			a:    VectorClock{"A": 1, "B": 0},
			b:    VectorClock{"A": 0, "B": 1},
			want: Concurrent,
		},
		{
			name: "{A:2,B:1} vs {A:1,B:2} → Concurrent",
			a:    VectorClock{"A": 2, "B": 1},
			b:    VectorClock{"A": 1, "B": 2},
			want: Concurrent,
		},

		// nil / empty clocks
		{
			name: "nil vs nil → Equal",
			a:    nil,
			b:    nil,
			want: Equal,
		},
		{
			name: "nil vs {A:1} → Before",
			a:    nil,
			b:    VectorClock{"A": 1},
			want: Before,
		},
		{
			name: "{A:1} vs nil → After",
			a:    VectorClock{"A": 1},
			b:    nil,
			want: After,
		},
		{
			name: "empty vs empty → Equal",
			a:    VectorClock{},
			b:    VectorClock{},
			want: Equal,
		},
		{
			name: "empty vs {A:1} → Before",
			a:    VectorClock{},
			b:    VectorClock{"A": 1},
			want: Before,
		},

		// Multi-key dominance
		{
			name: "{A:3,B:2} vs {A:1,B:1} → After",
			a:    VectorClock{"A": 3, "B": 2},
			b:    VectorClock{"A": 1, "B": 1},
			want: After,
		},
		{
			name: "{A:1,B:1} vs {A:3,B:2} → Before",
			a:    VectorClock{"A": 1, "B": 1},
			b:    VectorClock{"A": 3, "B": 2},
			want: Before,
		},
		{
			name: "{A:2,B:2} vs {A:2,B:2} → Equal",
			a:    VectorClock{"A": 2, "B": 2},
			b:    VectorClock{"A": 2, "B": 2},
			want: Equal,
		},

		// Asymmetric key presence
		{
			name: "{A:1} vs {A:1,B:1} → Before (b has extra key)",
			a:    VectorClock{"A": 1},
			b:    VectorClock{"A": 1, "B": 1},
			want: Before,
		},
		{
			name: "{A:1,B:1} vs {A:1} → After (a has extra key)",
			a:    VectorClock{"A": 1, "B": 1},
			b:    VectorClock{"A": 1},
			want: After,
		},
		{
			name: "{A:1} vs {B:1} → Concurrent (disjoint keys)",
			a:    VectorClock{"A": 1},
			b:    VectorClock{"B": 1},
			want: Concurrent,
		},

		// Zero values should be treated as absent
		{
			name: "{A:1,B:0} vs {A:1} → Equal (explicit zero == absent)",
			a:    VectorClock{"A": 1, "B": 0},
			b:    VectorClock{"A": 1},
			want: Equal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Compare(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("Compare(%v, %v) = %v, want %v", tt.a, tt.b, orderName(got), orderName(tt.want))
			}
		})
	}
}

// TestCompareSymmetry verifies that if Compare(a,b) == Before then Compare(b,a) == After and vice-versa.
func TestCompareSymmetry(t *testing.T) {
	pairs := []struct{ a, b VectorClock }{
		{VectorClock{"A": 1}, VectorClock{"A": 2}},
		{VectorClock{"A": 1, "B": 1}, VectorClock{"A": 2, "B": 2}},
		{nil, VectorClock{"A": 1}},
	}
	for _, p := range pairs {
		ab := Compare(p.a, p.b)
		ba := Compare(p.b, p.a)
		if ab == Before && ba != After {
			t.Errorf("symmetry broken: Compare(%v,%v)=Before but Compare(%v,%v)=%v", p.a, p.b, p.b, p.a, orderName(ba))
		}
		if ab == After && ba != Before {
			t.Errorf("symmetry broken: Compare(%v,%v)=After but Compare(%v,%v)=%v", p.a, p.b, p.b, p.a, orderName(ba))
		}
	}
}

// ---- Increment ----

func TestIncrementCreatesNewClock(t *testing.T) {
	orig := VectorClock{"A": 3, "B": 1}
	next := Increment(orig, "A")

	if next["A"] != 4 {
		t.Errorf("expected next[A]=4, got %d", next["A"])
	}
	if next["B"] != 1 {
		t.Errorf("expected next[B]=1, got %d", next["B"])
	}
	// Original must not be mutated.
	if orig["A"] != 3 {
		t.Errorf("Increment mutated the original clock: orig[A]=%d", orig["A"])
	}
}

func TestIncrementNewKey(t *testing.T) {
	orig := VectorClock{"A": 1}
	next := Increment(orig, "B")

	if next["B"] != 1 {
		t.Errorf("expected next[B]=1 for new node, got %d", next["B"])
	}
	if _, ok := orig["B"]; ok {
		t.Error("Increment added new key to original clock")
	}
}

func TestIncrementNilClock(t *testing.T) {
	next := Increment(nil, "X")
	if next["X"] != 1 {
		t.Errorf("expected 1 for first increment on nil clock, got %d", next["X"])
	}
}

func TestIncrementEmptyClock(t *testing.T) {
	next := Increment(VectorClock{}, "Z")
	if next["Z"] != 1 {
		t.Errorf("expected 1 for first increment on empty clock, got %d", next["Z"])
	}
}

func TestIncrementDoesNotShareState(t *testing.T) {
	c1 := VectorClock{"A": 1}
	c2 := Increment(c1, "A")
	c3 := Increment(c2, "A")

	// Each step must be independent.
	if c1["A"] != 1 || c2["A"] != 2 || c3["A"] != 3 {
		t.Errorf("chained increments failed: c1[A]=%d c2[A]=%d c3[A]=%d", c1["A"], c2["A"], c3["A"])
	}
}

// ---- Merge ----

func TestMergeComponentWiseMax(t *testing.T) {
	a := VectorClock{"A": 3, "B": 1, "C": 5}
	b := VectorClock{"A": 1, "B": 4, "D": 2}

	m := Merge(a, b)

	expected := VectorClock{"A": 3, "B": 4, "C": 5, "D": 2}
	if !reflect.DeepEqual(m, expected) {
		t.Errorf("Merge result mismatch:\n  got  %v\n  want %v", m, expected)
	}
}

func TestMergeDoesNotMutateInputs(t *testing.T) {
	a := VectorClock{"A": 1}
	b := VectorClock{"A": 5}

	aCopy := VectorClock{"A": 1}
	bCopy := VectorClock{"A": 5}

	_ = Merge(a, b)

	if !reflect.DeepEqual(a, aCopy) {
		t.Error("Merge mutated clock a")
	}
	if !reflect.DeepEqual(b, bCopy) {
		t.Error("Merge mutated clock b")
	}
}

func TestMergeWithNil(t *testing.T) {
	a := VectorClock{"A": 2}
	m := Merge(a, nil)
	if m["A"] != 2 {
		t.Errorf("Merge with nil changed value: got %d, want 2", m["A"])
	}
}

func TestMergeIdentical(t *testing.T) {
	a := VectorClock{"A": 3, "B": 7}
	m := Merge(a, a)
	if !reflect.DeepEqual(m, a) {
		t.Errorf("Merge of identical clocks should equal input: got %v", m)
	}
}

func TestMergeEmpty(t *testing.T) {
	m := Merge(VectorClock{}, VectorClock{})
	if len(m) != 0 {
		t.Errorf("Merge of two empty clocks should be empty, got %v", m)
	}
}

func TestMergeIsCommutative(t *testing.T) {
	a := VectorClock{"X": 4, "Y": 1}
	b := VectorClock{"X": 2, "Y": 6, "Z": 3}

	ab := Merge(a, b)
	ba := Merge(b, a)

	if !reflect.DeepEqual(ab, ba) {
		t.Errorf("Merge is not commutative:\n  Merge(a,b)=%v\n  Merge(b,a)=%v", ab, ba)
	}
}

// orderName returns a human-readable string for a ClockOrder value.
func orderName(o ClockOrder) string {
	switch o {
	case Before:
		return "Before"
	case After:
		return "After"
	case Concurrent:
		return "Concurrent"
	case Equal:
		return "Equal"
	default:
		return "Unknown"
	}
}

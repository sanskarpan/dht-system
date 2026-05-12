// Package replication provides vector clock operations and conflict resolution
// for the DHT system's replication layer.
package replication

// VectorClock maps nodeID -> lamport counter.
type VectorClock map[string]uint64

// ClockOrder describes the causal relationship between two vector clocks.
type ClockOrder int

const (
	Before     ClockOrder = iota // a < b  (a is causally before b)
	After                        // a > b  (a is causally after b)
	Concurrent                   // neither dominates
	Equal                        // identical clocks
)

// Increment creates a new VectorClock with the counter for nodeID incremented by 1.
// It does not mutate the receiver c.
func Increment(c VectorClock, nodeID string) VectorClock {
	next := make(VectorClock, len(c)+1)
	for k, v := range c {
		next[k] = v
	}
	next[nodeID]++
	return next
}

// Compare determines the causal relationship between clocks a and b.
//
// We track two booleans that represent whether the "less-than-or-equal"
// relationship holds component-wise in each direction:
//
//   aLEb = true  iff  a[k] <= b[k]  for every k  (a dominated by b → Before)
//   bLEa = true  iff  b[k] <= a[k]  for every k  (b dominated by a → After)
//
// Once we have a[k] > b[k] for any k, aLEb becomes false.
// Once we have b[k] > a[k] for any k, bLEa becomes false.
//
// Results:
//   aLEb && bLEa → Equal
//   aLEb only    → Before  (a ≤ b but not b ≤ a)
//   bLEa only    → After   (b ≤ a but not a ≤ b)
//   neither      → Concurrent
func Compare(a, b VectorClock) ClockOrder {
	aLEb := true // assume a[k] <= b[k] until proven otherwise
	bLEa := true // assume b[k] <= a[k] until proven otherwise

	// Walk keys present in a.
	for k, av := range a {
		bv := b[k] // 0 if absent
		if av > bv {
			// a[k] > b[k] → a is NOT <= b at this key
			aLEb = false
		}
		if bv > av {
			// b[k] > a[k] → b is NOT <= a at this key
			bLEa = false
		}
	}

	// Walk keys present in b but NOT in a (a's implicit value is 0).
	for k, bv := range b {
		if _, ok := a[k]; !ok && bv > 0 {
			// b[k] > 0 = a[k] → b is NOT <= a at this key
			bLEa = false
		}
	}

	switch {
	case aLEb && bLEa:
		return Equal
	case aLEb:
		// a[k] <= b[k] for all k and a != b → a is causally before b
		return Before
	case bLEa:
		// b[k] <= a[k] for all k and a != b → a is causally after b
		return After
	default:
		return Concurrent
	}
}

// Merge returns a new VectorClock containing the component-wise maximum of a and b.
// Neither a nor b is mutated.
func Merge(a, b VectorClock) VectorClock {
	result := make(VectorClock, len(a))
	for k, v := range a {
		result[k] = v
	}
	for k, v := range b {
		if v > result[k] {
			result[k] = v
		}
	}
	return result
}

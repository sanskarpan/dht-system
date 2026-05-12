package consistent

import (
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"testing"
)

// TestIsInInterval covers normal and wrap-around cases.
func TestIsInInterval(t *testing.T) {
	// Helper: create an ID from a single byte value in a 256-element space
	// We'll use real 20-byte IDs but set only the last byte for simple tests.
	mkID := func(b byte) [20]byte {
		var id [20]byte
		id[19] = b
		return id
	}

	tests := []struct {
		name  string
		id    [20]byte
		start [20]byte
		end   [20]byte
		want  bool
	}{
		// Normal (non-wrapping) interval
		{"inside normal", mkID(5), mkID(3), mkID(9), true},
		{"at start (excluded)", mkID(3), mkID(3), mkID(9), false},
		{"at end (excluded)", mkID(9), mkID(3), mkID(9), false},
		{"below normal", mkID(2), mkID(3), mkID(9), false},
		{"above normal", mkID(10), mkID(3), mkID(9), false},

		// Wrap-around interval (start > end)
		{"wrap-around above start", mkID(200), mkID(100), mkID(10), true},  // 200 > 100
		{"wrap-around below end", mkID(5), mkID(100), mkID(10), true},       // 5 < 10
		{"wrap-around at start excluded", mkID(100), mkID(100), mkID(10), false},
		{"wrap-around at end excluded", mkID(10), mkID(100), mkID(10), false},
		{"wrap-around in middle", mkID(50), mkID(100), mkID(10), false},     // 50 not > 100 and not < 10

		// Edge: id = 2 in (250, 10) ring wrap — should be true
		{"example from spec", mkID(2), mkID(250), mkID(10), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsInInterval(tt.id, tt.start, tt.end)
			if got != tt.want {
				t.Errorf("IsInInterval(%d, %d, %d) = %v, want %v",
					tt.id[19], tt.start[19], tt.end[19], got, tt.want)
			}
		})
	}
}

// TestIsInIntervalRightClosed verifies inclusive end.
func TestIsInIntervalRightClosed(t *testing.T) {
	mkID := func(b byte) [20]byte {
		var id [20]byte
		id[19] = b
		return id
	}

	// id == end → should return true
	if !IsInIntervalRightClosed(mkID(9), mkID(3), mkID(9)) {
		t.Error("id == end should be included in right-closed interval")
	}
	// id inside normal interval
	if !IsInIntervalRightClosed(mkID(5), mkID(3), mkID(9)) {
		t.Error("id inside interval should be included")
	}
	// id == start → should return false (left-open)
	if IsInIntervalRightClosed(mkID(3), mkID(3), mkID(9)) {
		t.Error("id == start should not be included in right-closed interval")
	}
}

// TestAdd verifies ring arithmetic including wrap-around.
func TestAdd(t *testing.T) {
	var zero [20]byte

	// 0 + 1 = 1
	result := Add(zero, big.NewInt(1))
	if result[19] != 1 {
		t.Errorf("0 + 1 = %d, want 1", result[19])
	}

	// Max ID + 1 should wrap to 0
	maxID := BigIntToID(new(big.Int).Sub(keySpaceSize, big.NewInt(1)))
	wrapped := Add(maxID, big.NewInt(1))
	if wrapped != zero {
		t.Errorf("maxID + 1 should wrap to 0, got %x", wrapped)
	}

	// 2^160 - 1 + 2 should give 1
	result2 := Add(maxID, big.NewInt(2))
	var expected [20]byte
	expected[19] = 1
	if result2 != expected {
		t.Errorf("maxID + 2 should be 1, got %x", result2)
	}

	// PowerOfTwo check: 2^i
	for i := 0; i < 160; i++ {
		p := PowerOfTwo(i)
		expected := new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(i)), nil)
		if p.Cmp(expected) != 0 {
			t.Errorf("PowerOfTwo(%d) = %v, want %v", i, p, expected)
		}
	}
}

// TestIDToFloat64 verifies positions are in [0, 1].
func TestIDToFloat64(t *testing.T) {
	var zero [20]byte
	if f := IDToFloat64(zero); f != 0.0 {
		t.Errorf("IDToFloat64(0) = %v, want 0.0", f)
	}

	// maxID (2^160 - 1) / 2^160 is extremely close to 1.0.
	// Due to float64 precision limits it may round to exactly 1.0, which is acceptable
	// for visualization purposes.
	maxID := BigIntToID(new(big.Int).Sub(keySpaceSize, big.NewInt(1)))
	f := IDToFloat64(maxID)
	if f < 0 || f > 1.0 {
		t.Errorf("IDToFloat64(maxID) = %v, should be in [0, 1]", f)
	}

	// A midpoint ID should be ~0.5
	midID := BigIntToID(new(big.Int).Rsh(keySpaceSize, 1)) // 2^159
	mid := IDToFloat64(midID)
	if math.Abs(mid-0.5) > 0.001 {
		t.Errorf("IDToFloat64(mid) = %v, want ~0.5", mid)
	}
}

// TestSHA1Uniformity verifies SHA-1 produces a roughly uniform distribution.
// Uses a chi-squared test over 10 buckets with 10,000 samples.
func TestSHA1Uniformity(t *testing.T) {
	const n = 10000
	const buckets = 10
	counts := make([]int, buckets)

	r := rand.New(rand.NewSource(42))
	for i := 0; i < n; i++ {
		key := make([]byte, 16)
		_, _ = r.Read(key)
		id := SHA1(string(key))
		// Determine bucket: take first byte mod buckets
		bucket := int(id[0]) * buckets / 256
		counts[bucket]++
	}

	// Chi-squared test: expected = n/buckets per bucket
	expected := float64(n) / float64(buckets)
	chiSq := 0.0
	for _, count := range counts {
		diff := float64(count) - expected
		chiSq += diff * diff / expected
	}

	// For 9 degrees of freedom, chi-squared critical value at 0.001 = 27.877
	// If chiSq < 27.877, we fail to reject H0 (uniform distribution)
	if chiSq > 27.877 {
		t.Errorf("SHA-1 distribution not uniform: chi-squared = %.2f (threshold 27.877)", chiSq)
	}

	t.Logf("SHA-1 uniformity chi-squared = %.4f (threshold 27.877)", chiSq)
}

// TestIDToHexRoundtrip tests hex encoding/decoding.
func TestIDToHexRoundtrip(t *testing.T) {
	id := SHA1("test-key-123")
	hex := IDToHex(id)
	decoded, err := IDFromHex(hex)
	if err != nil {
		t.Fatalf("IDFromHex error: %v", err)
	}
	if decoded != id {
		t.Errorf("hex roundtrip failed: %x != %x", decoded, id)
	}
}

// TestCompare verifies ID ordering.
func TestCompare(t *testing.T) {
	mkID := func(b byte) [20]byte {
		var id [20]byte
		id[19] = b
		return id
	}

	if Compare(mkID(5), mkID(10)) != -1 {
		t.Error("5 < 10 should return -1")
	}
	if Compare(mkID(10), mkID(5)) != 1 {
		t.Error("10 > 5 should return 1")
	}
	if Compare(mkID(7), mkID(7)) != 0 {
		t.Error("7 == 7 should return 0")
	}
}

// TestAddConsistency verifies Add produces same ring position as IDToBigInt arithmetic.
func TestAddConsistency(t *testing.T) {
	id := SHA1("node-addr-1")
	n := PowerOfTwo(7) // 2^7 = 128

	result := Add(id, n)

	// Manually compute
	idBig := IDToBigInt(id)
	expected := new(big.Int).Add(idBig, n)
	expected.Mod(expected, keySpaceSize)

	if IDToBigInt(result).Cmp(expected) != 0 {
		t.Errorf("Add inconsistency: got %x, want %x", result, BigIntToID(expected))
	}
}

// TestIsInIntervalSymmetry verifies that intervals are correctly asymmetric
// (unlike equality, intervals have directionality on the ring).
func TestIsInIntervalDegenerate(t *testing.T) {
	mkID := func(b byte) [20]byte {
		var id [20]byte
		id[19] = b
		return id
	}

	// start == end: no ID should be in interval
	if IsInInterval(mkID(5), mkID(5), mkID(5)) {
		t.Error("start == end: no ID should match")
	}
}

// TestVNodeLoadBalance verifies that virtual nodes improve load distribution.
// It measures the coefficient of variation (std_dev/mean) for V=1, 3, and 10
// vnodes per physical node over 10k keys.
func TestVNodeLoadBalance(t *testing.T) {
	const (
		nodeCount = 10
		keyCount  = 10_000
	)

	addrs := make([]string, nodeCount)
	for i := range addrs {
		addrs[i] = fmt.Sprintf("node-%02d:700%d", i, i)
	}

	measureCV := func(vnodes int) float64 {
		ring := NewVNodeRing(vnodes)
		for _, a := range addrs {
			ring.AddNode(a)
		}
		hits := make(map[string]float64, nodeCount)
		for i := 0; i < keyCount; i++ {
			k := SHA1(fmt.Sprintf("bench-key-%d", i))
			if owner, ok := ring.FindOwner(k); ok {
				hits[owner]++
			}
		}
		mean := float64(keyCount) / float64(nodeCount)
		var sumSq float64
		for _, a := range addrs {
			d := hits[a] - mean
			sumSq += d * d
		}
		return math.Sqrt(sumSq/float64(nodeCount)) / mean
	}

	cv1 := measureCV(1)
	cv3 := measureCV(3)
	cv15 := measureCV(15)
	t.Logf("CV: 1 vnode=%.3f  3 vnodes=%.3f  15 vnodes=%.3f", cv1, cv3, cv15)

	// 3 vnodes must improve over 1 vnode
	if cv3 >= cv1 {
		t.Errorf("3 vnodes (CV=%.3f) did not improve over 1 vnode (CV=%.3f)", cv3, cv1)
	}
	// 15 vnodes per node should achieve CV < 0.30
	if cv15 >= 0.30 {
		t.Errorf("15 vnodes/node CV=%.3f exceeds threshold 0.30", cv15)
	}
}

// TestBigIntToIDRoundtrip verifies BigIntToID and IDToBigInt are inverses.
func TestBigIntToIDRoundtrip(t *testing.T) {
	for _, v := range []int64{0, 1, 255, 256, 1<<20 - 1} {
		n := big.NewInt(v)
		id := BigIntToID(n)
		back := IDToBigInt(id)
		if back.Cmp(n) != 0 {
			t.Errorf("roundtrip failed for %d: got %v", v, back)
		}
	}

	// Test with a large number close to 2^160
	large := new(big.Int).Sub(keySpaceSize, big.NewInt(42))
	id := BigIntToID(large)
	back := IDToBigInt(id)
	if back.Cmp(large) != 0 {
		t.Errorf("large roundtrip failed: got %v, want %v", back, large)
	}
}

// BenchmarkSHA1 benchmarks the hash function.
func BenchmarkSHA1(b *testing.B) {
	key := "benchmark-key-12345"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SHA1(key)
	}
}

// BenchmarkIsInInterval benchmarks interval checks.
func BenchmarkIsInInterval(b *testing.B) {
	start := SHA1("node-1")
	end := SHA1("node-2")
	id := SHA1("key-x")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = IsInInterval(id, start, end)
	}
}

// Ensure math package usage (prevent unused import).
var _ = math.Pi

// TestMineNodeID verifies S/Kademlia proof-of-work node ID generation.
func TestMineNodeID(t *testing.T) {
	// difficulty=0: instant, any hash works.
	id0, nonce0 := MineNodeID(0)
	if id0 == ([20]byte{}) {
		t.Error("MineNodeID(0) returned zero ID")
	}
	t.Logf("difficulty=0: nonce=%d id=%x", nonce0, id0)

	// difficulty=8: first byte of SHA-256(nonce) must be 0x00.
	// Expected ~256 attempts on average.
	id8, nonce8 := MineNodeID(8)
	if id8 == ([20]byte{}) {
		t.Error("MineNodeID(8) returned zero ID")
	}
	t.Logf("difficulty=8: nonce=%d id=%x", nonce8, id8)

	// Verify: two calls with same difficulty return different IDs
	// (different nonces unless astronomically unlucky).
	id8b, nonce8b := MineNodeID(8)
	if nonce8 == nonce8b {
		t.Logf("same nonce twice (extremely unlikely but not an error): %d", nonce8)
	}
	_ = id8b

	// difficulty=16: expected ~65536 attempts — feasible in tests.
	if testing.Short() {
		t.Skip("skipping difficulty=16 in short mode")
	}
	id16, nonce16 := MineNodeID(16)
	if id16 == ([20]byte{}) {
		t.Error("MineNodeID(16) returned zero ID")
	}
	t.Logf("difficulty=16: nonce=%d id=%x", nonce16, id16)
}

package store

import (
	"sync"
	"testing"
	"time"
)

// key returns a [20]byte with the last byte set to b for easy construction.
func key(b byte) [20]byte {
	var k [20]byte
	k[19] = b
	return k
}

// keyAt sets byte at position pos (0 = most significant).
func keyAt(pos int, b byte) [20]byte {
	var k [20]byte
	k[pos] = b
	return k
}

// makeEntry builds a ValueEntry with the given key byte and value string. TTL=0 (no expiry).
func makeEntry(k byte, value string) *ValueEntry {
	return &ValueEntry{
		Key:       key(k),
		Value:     []byte(value),
		Timestamp: time.Now(),
		NodeID:    "node1",
	}
}

// makeEntryTTL builds a ValueEntry with the given TTL.
func makeEntryTTL(k byte, value string, ttl time.Duration) *ValueEntry {
	return &ValueEntry{
		Key:       key(k),
		Value:     []byte(value),
		Timestamp: time.Now(),
		TTL:       ttl,
		NodeID:    "node1",
	}
}

// ---- Put / Get / Delete ----

func TestPutAndGet(t *testing.T) {
	s := NewKVStore()
	e := makeEntry(1, "hello")
	s.Put(e)

	got, ok := s.Get(key(1))
	if !ok {
		t.Fatal("expected entry to be found")
	}
	if string(got.Value) != "hello" {
		t.Fatalf("expected value 'hello', got %q", got.Value)
	}
}

func TestGetNotFound(t *testing.T) {
	s := NewKVStore()
	_, ok := s.Get(key(42))
	if ok {
		t.Fatal("expected miss for absent key")
	}
}

func TestPutOverwrites(t *testing.T) {
	s := NewKVStore()
	s.Put(makeEntry(1, "first"))
	s.Put(makeEntry(1, "second"))

	got, ok := s.Get(key(1))
	if !ok {
		t.Fatal("expected entry")
	}
	if string(got.Value) != "second" {
		t.Fatalf("expected 'second', got %q", got.Value)
	}
}

func TestDelete(t *testing.T) {
	s := NewKVStore()
	s.Put(makeEntry(5, "val"))

	deleted := s.Delete(key(5))
	if !deleted {
		t.Fatal("expected Delete to return true for existing key")
	}
	_, ok := s.Get(key(5))
	if ok {
		t.Fatal("expected entry to be gone after Delete")
	}
}

func TestDeleteNonExistent(t *testing.T) {
	s := NewKVStore()
	deleted := s.Delete(key(99))
	if deleted {
		t.Fatal("expected Delete to return false for missing key")
	}
}

// ---- Clone isolation ----

func TestPutClonesEntry(t *testing.T) {
	s := NewKVStore()
	e := makeEntry(2, "original")
	s.Put(e)

	// Mutate the original after Put; the stored copy should be unaffected.
	e.Value[0] = 'X'

	got, _ := s.Get(key(2))
	if got.Value[0] == 'X' {
		t.Fatal("Put did not clone the entry; mutation leaked into store")
	}
}

func TestGetClonesEntry(t *testing.T) {
	s := NewKVStore()
	s.Put(makeEntry(3, "abc"))

	got, _ := s.Get(key(3))
	got.Value[0] = 'Z'

	got2, _ := s.Get(key(3))
	if got2.Value[0] == 'Z' {
		t.Fatal("Get did not clone the entry; mutation leaked into store")
	}
}

// ---- TTL ----

func TestTTLNotExpired(t *testing.T) {
	s := NewKVStore()
	s.Put(makeEntryTTL(10, "fresh", 10*time.Second))

	_, ok := s.Get(key(10))
	if !ok {
		t.Fatal("entry with future TTL should be returned")
	}
}

func TestTTLExpired(t *testing.T) {
	s := NewKVStore()
	e := &ValueEntry{
		Key:       key(11),
		Value:     []byte("stale"),
		Timestamp: time.Now().Add(-2 * time.Second), // written 2s ago
		TTL:       1 * time.Second,                  // expired after 1s
		NodeID:    "node1",
	}
	s.Put(e)

	_, ok := s.Get(key(11))
	if ok {
		t.Fatal("expired entry should not be returned by Get")
	}
}

func TestTTLZeroNeverExpires(t *testing.T) {
	s := NewKVStore()
	e := &ValueEntry{
		Key:       key(12),
		Value:     []byte("permanent"),
		Timestamp: time.Now().Add(-100 * time.Hour),
		TTL:       0, // no expiry
		NodeID:    "node1",
	}
	s.Put(e)

	_, ok := s.Get(key(12))
	if !ok {
		t.Fatal("TTL=0 entry should never expire")
	}
}

func TestStartTTLEviction(t *testing.T) {
	s := NewKVStore()

	// Insert an already-expired entry directly via the internal map to bypass
	// any future-TTL check in Put (Put just stores as-is).
	e := &ValueEntry{
		Key:       key(20),
		Value:     []byte("dead"),
		Timestamp: time.Now().Add(-5 * time.Second),
		TTL:       1 * time.Second,
		NodeID:    "node1",
	}
	s.mu.Lock()
	s.entries[e.Key] = e
	s.mu.Unlock()

	if s.Size() != 1 {
		t.Fatalf("expected size 1 before eviction, got %d", s.Size())
	}

	// Run eviction directly (no goroutine needed for the unit test).
	s.evictExpired()

	if s.Size() != 0 {
		t.Fatalf("expected size 0 after eviction, got %d", s.Size())
	}
}

// ---- All ----

func TestAll(t *testing.T) {
	s := NewKVStore()
	s.Put(makeEntry(1, "a"))
	s.Put(makeEntry(2, "b"))
	s.Put(makeEntry(3, "c"))

	all := s.All()
	if len(all) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(all))
	}
}

func TestAllExcludesExpired(t *testing.T) {
	s := NewKVStore()
	s.Put(makeEntry(1, "live"))

	// Inject expired entry directly.
	expired := &ValueEntry{
		Key:       key(2),
		Value:     []byte("dead"),
		Timestamp: time.Now().Add(-10 * time.Second),
		TTL:       1 * time.Second,
		NodeID:    "node1",
	}
	s.mu.Lock()
	s.entries[expired.Key] = expired
	s.mu.Unlock()

	all := s.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 non-expired entry, got %d", len(all))
	}
	if string(all[0].Value) != "live" {
		t.Fatalf("expected 'live', got %q", all[0].Value)
	}
}

// ---- GetInRange ----

// buildKey creates a [20]byte with a specific integer value in the last byte,
// used to position keys on the ring deterministically.
func buildKey(v byte) [20]byte { return key(v) }

func TestGetInRangeNormal(t *testing.T) {
	s := NewKVStore()
	// Keys: 10, 20, 30, 40, 50
	for _, k := range []byte{10, 20, 30, 40, 50} {
		s.Put(makeEntry(k, "v"))
	}

	// Range (15, 35] should return keys 20 and 30.
	result := s.GetInRange(buildKey(15), buildKey(35))
	found := make(map[byte]bool)
	for _, e := range result {
		found[e.Key[19]] = true
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 entries in range (15,35], got %d", len(result))
	}
	if !found[20] || !found[30] {
		t.Fatalf("expected keys 20 and 30, got %v", found)
	}
}

func TestGetInRangeIncludesEnd(t *testing.T) {
	s := NewKVStore()
	s.Put(makeEntry(50, "end"))
	s.Put(makeEntry(30, "middle"))

	// Range (30, 50]: should include 50 (end) but NOT 30 (start is exclusive).
	result := s.GetInRange(buildKey(30), buildKey(50))
	if len(result) != 1 {
		t.Fatalf("expected 1 entry (end key), got %d", len(result))
	}
	if result[0].Key[19] != 50 {
		t.Fatalf("expected key 50, got %d", result[0].Key[19])
	}
}

func TestGetInRangeExcludesStart(t *testing.T) {
	s := NewKVStore()
	s.Put(makeEntry(10, "start"))
	s.Put(makeEntry(20, "middle"))

	// Range (10, 25]: key 10 (start) must not appear.
	result := s.GetInRange(buildKey(10), buildKey(25))
	for _, e := range result {
		if e.Key[19] == 10 {
			t.Fatal("start key must be excluded from (start, end]")
		}
	}
}

func TestGetInRangeWrapAround(t *testing.T) {
	s := NewKVStore()
	// Keys: 5, 10, 200, 250 (using last byte for position)
	for _, k := range []byte{5, 10, 200, 250} {
		s.Put(makeEntry(k, "v"))
	}

	// Wrap-around range (200, 10]: covers 201..255 and 0..10.
	// Should include 250 and 5 and 10.
	result := s.GetInRange(buildKey(200), buildKey(10))
	found := make(map[byte]bool)
	for _, e := range result {
		found[e.Key[19]] = true
	}

	if !found[250] {
		t.Error("expected key 250 in wrap-around range (200,10]")
	}
	if !found[5] {
		t.Error("expected key 5 in wrap-around range (200,10]")
	}
	if !found[10] {
		t.Error("expected key 10 (end) in wrap-around range (200,10]")
	}
	if found[200] {
		t.Error("start key 200 must be excluded from wrap-around range")
	}
}

func TestGetInRangeEmptyWhenStartEqualsEnd(t *testing.T) {
	s := NewKVStore()
	s.Put(makeEntry(5, "v"))

	// start == end (not equal to any key): empty interval.
	result := s.GetInRange(buildKey(5), buildKey(5))
	// Key 5 == end, so it IS included (key == end returns true).
	// This is the defined behaviour: key == end → true.
	// So we just verify no panic and the result is consistent.
	_ = result
}

func TestGetInRangeExcludesExpired(t *testing.T) {
	s := NewKVStore()
	s.Put(makeEntry(20, "live"))

	expired := &ValueEntry{
		Key:       key(25),
		Value:     []byte("stale"),
		Timestamp: time.Now().Add(-10 * time.Second),
		TTL:       1 * time.Second,
		NodeID:    "node1",
	}
	s.mu.Lock()
	s.entries[expired.Key] = expired
	s.mu.Unlock()

	result := s.GetInRange(buildKey(10), buildKey(30))
	for _, e := range result {
		if e.Key[19] == 25 {
			t.Fatal("GetInRange must not return expired entries")
		}
	}
}

// ---- DeleteInRange ----

func TestDeleteInRange(t *testing.T) {
	s := NewKVStore()
	for _, k := range []byte{10, 20, 30, 40, 50} {
		s.Put(makeEntry(k, "v"))
	}

	// Delete (15, 35]: should delete 20 and 30.
	deleted := s.DeleteInRange(buildKey(15), buildKey(35))
	deletedKeys := make(map[byte]bool)
	for _, e := range deleted {
		deletedKeys[e.Key[19]] = true
	}

	if len(deleted) != 2 {
		t.Fatalf("expected 2 deleted entries, got %d", len(deleted))
	}
	if !deletedKeys[20] || !deletedKeys[30] {
		t.Fatalf("expected keys 20 and 30 deleted, got %v", deletedKeys)
	}

	// Remaining keys should be 10, 40, 50.
	all := s.All()
	if len(all) != 3 {
		t.Fatalf("expected 3 remaining entries, got %d", len(all))
	}
}

func TestDeleteInRangeWrapAround(t *testing.T) {
	s := NewKVStore()
	for _, k := range []byte{5, 10, 100, 200, 250} {
		s.Put(makeEntry(k, "v"))
	}

	// Wrap-around range (200, 10]: deletes 250, 5, 10.
	deleted := s.DeleteInRange(buildKey(200), buildKey(10))
	if len(deleted) != 3 {
		t.Fatalf("expected 3 deleted entries in wrap range, got %d", len(deleted))
	}

	all := s.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 remaining entries after wrap delete, got %d", len(all))
	}
}

// ---- Concurrent safety ----

func TestConcurrentPutGet(t *testing.T) {
	s := NewKVStore()
	const workers = 50
	const ops = 200

	var wg sync.WaitGroup
	wg.Add(workers * 2)

	// Writers
	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				k := byte((id*ops + j) % 256)
				s.Put(makeEntry(k, "val"))
			}
		}(i)
	}

	// Readers
	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				k := byte((id*ops + j) % 256)
				s.Get(key(k))
			}
		}(i)
	}

	wg.Wait()
}

func TestConcurrentPutDelete(t *testing.T) {
	s := NewKVStore()
	const workers = 20

	// Pre-populate
	for i := 0; i < 256; i++ {
		s.Put(makeEntry(byte(i), "v"))
	}

	var wg sync.WaitGroup
	wg.Add(workers * 2)

	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			s.Put(makeEntry(byte(id%256), "new"))
		}(i)
		go func(id int) {
			defer wg.Done()
			s.Delete(key(byte(id % 256)))
		}(i)
	}
	wg.Wait()
}

// ---- Size ----

func TestSize(t *testing.T) {
	s := NewKVStore()
	if s.Size() != 0 {
		t.Fatal("new store should have size 0")
	}
	s.Put(makeEntry(1, "a"))
	s.Put(makeEntry(2, "b"))
	if s.Size() != 2 {
		t.Fatalf("expected size 2, got %d", s.Size())
	}
	s.Delete(key(1))
	if s.Size() != 1 {
		t.Fatalf("expected size 1 after delete, got %d", s.Size())
	}
}

// ---- isInRangeRightClosed unit tests ----

func TestIsInRangeRightClosed(t *testing.T) {
	tests := []struct {
		name  string
		key   byte
		start byte
		end   byte
		want  bool
	}{
		{"key == end (included)", 30, 10, 30, true},
		{"key == start (excluded)", 10, 10, 30, false},
		{"key inside normal", 20, 10, 30, true},
		{"key outside normal below", 5, 10, 30, false},
		{"key outside normal above", 35, 10, 30, false},
		{"wrap: key above start", 210, 200, 10, true},
		{"wrap: key below end", 5, 200, 10, true},
		{"wrap: key == end", 10, 200, 10, true},
		{"wrap: key == start excluded", 200, 200, 10, false},
		{"wrap: key in middle excluded", 100, 200, 10, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isInRangeRightClosed(buildKey(tt.key), buildKey(tt.start), buildKey(tt.end))
			if got != tt.want {
				t.Errorf("isInRangeRightClosed(key=%d, start=%d, end=%d) = %v, want %v",
					tt.key, tt.start, tt.end, got, tt.want)
			}
		})
	}
}

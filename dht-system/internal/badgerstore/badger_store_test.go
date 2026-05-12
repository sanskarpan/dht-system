package badgerstore_test

import (
	"testing"
	"time"

	"github.com/sanskarpan/dht-system/dht-system/internal/badgerstore"
	"github.com/sanskarpan/dht-system/dht-system/internal/store"
)

// makeKey constructs a deterministic 20-byte key from a single seed byte.
func makeKey(seed byte) [20]byte {
	var k [20]byte
	k[19] = seed
	return k
}

// makeEntry builds a minimal ValueEntry suitable for round-trip tests.
func makeEntry(key [20]byte, value string) *store.ValueEntry {
	return &store.ValueEntry{
		Key:       key,
		Value:     []byte(value),
		Clock:     map[string]uint64{"node1": 1},
		Timestamp: time.Now(),
		TTL:       0, // no expiry
		NodeID:    "node1",
	}
}

// openStore is a helper that opens a BadgerStore at dir and fails the test on error.
func openStore(t *testing.T, dir string) *badgerstore.BadgerStore {
	t.Helper()
	s, err := badgerstore.New(dir)
	if err != nil {
		t.Fatalf("badgerstore.New(%q): %v", dir, err)
	}
	return s
}

// TestPutAndGet verifies that a stored entry can be retrieved with identical content.
func TestPutAndGet(t *testing.T) {
	dir := t.TempDir()
	s := openStore(t, dir)
	defer s.Close()

	key := makeKey(1)
	want := makeEntry(key, "hello-world")

	if err := s.Put(want); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, ok := s.Get(key)
	if !ok {
		t.Fatal("Get: expected entry to be found, got (nil, false)")
	}
	if string(got.Value) != string(want.Value) {
		t.Errorf("Get value: got %q, want %q", got.Value, want.Value)
	}
	if got.Key != want.Key {
		t.Errorf("Get key: got %v, want %v", got.Key, want.Key)
	}
	if got.NodeID != want.NodeID {
		t.Errorf("Get NodeID: got %q, want %q", got.NodeID, want.NodeID)
	}
}

// TestGetMissing verifies that querying an absent key returns (nil, false).
func TestGetMissing(t *testing.T) {
	dir := t.TempDir()
	s := openStore(t, dir)
	defer s.Close()

	_, ok := s.Get(makeKey(99))
	if ok {
		t.Fatal("Get: expected (nil, false) for missing key, but got ok=true")
	}
}

// TestGetExpired verifies that an expired entry is treated as absent.
func TestGetExpired(t *testing.T) {
	dir := t.TempDir()
	s := openStore(t, dir)
	defer s.Close()

	key := makeKey(2)
	entry := &store.ValueEntry{
		Key:       key,
		Value:     []byte("will-expire"),
		Timestamp: time.Now().Add(-2 * time.Second), // written 2 s ago
		TTL:       1 * time.Second,                  // 1 s TTL → already expired
		NodeID:    "node1",
	}

	if err := s.Put(entry); err != nil {
		t.Fatalf("Put: %v", err)
	}

	_, ok := s.Get(key)
	if ok {
		t.Fatal("Get: expected expired entry to be treated as missing, but got ok=true")
	}
}

// TestDelete verifies that a deleted key is no longer retrievable.
func TestDelete(t *testing.T) {
	dir := t.TempDir()
	s := openStore(t, dir)
	defer s.Close()

	key := makeKey(3)
	if err := s.Put(makeEntry(key, "to-be-deleted")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, ok := s.Get(key); !ok {
		t.Fatal("Get before Delete: entry not found")
	}

	if err := s.Delete(key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, ok := s.Get(key); ok {
		t.Fatal("Get after Delete: entry still present, expected (nil, false)")
	}
}

// TestDeleteNonExistent verifies that deleting a missing key is a no-op (no error).
func TestDeleteNonExistent(t *testing.T) {
	dir := t.TempDir()
	s := openStore(t, dir)
	defer s.Close()

	if err := s.Delete(makeKey(200)); err != nil {
		t.Fatalf("Delete of non-existent key: unexpected error: %v", err)
	}
}

// TestGetAll verifies that GetAll returns all non-deleted, non-expired entries.
func TestGetAll(t *testing.T) {
	dir := t.TempDir()
	s := openStore(t, dir)
	defer s.Close()

	// Insert 5 entries.
	for i := byte(0); i < 5; i++ {
		if err := s.Put(makeEntry(makeKey(i), "value")); err != nil {
			t.Fatalf("Put key %d: %v", i, err)
		}
	}

	// Delete one.
	if err := s.Delete(makeKey(2)); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	entries := s.GetAll()
	if len(entries) != 4 {
		t.Fatalf("GetAll: got %d entries, want 4", len(entries))
	}

	// Ensure the deleted key is absent from the result set.
	deletedKey := makeKey(2)
	for _, e := range entries {
		if e.Key == deletedKey {
			t.Errorf("GetAll: found deleted key %v in results", deletedKey)
		}
	}
}

// TestGetAllEmpty verifies that GetAll on an empty store returns an empty (non-nil) slice.
func TestGetAllEmpty(t *testing.T) {
	dir := t.TempDir()
	s := openStore(t, dir)
	defer s.Close()

	entries := s.GetAll()
	if entries == nil {
		t.Fatal("GetAll on empty store: got nil, want empty slice")
	}
	if len(entries) != 0 {
		t.Fatalf("GetAll on empty store: got %d entries, want 0", len(entries))
	}
}

// TestPersistenceAcrossReopen verifies that data written before Close is
// still available after reopening the store with the same path.
func TestPersistenceAcrossReopen(t *testing.T) {
	dir := t.TempDir()

	// --- Phase 1: write data and close. ---
	s1 := openStore(t, dir)
	keys := [][20]byte{makeKey(10), makeKey(11), makeKey(12)}
	for i, k := range keys {
		if err := s1.Put(makeEntry(k, "persistent-value-"+string(rune('A'+i)))); err != nil {
			t.Fatalf("Put key %d: %v", i, err)
		}
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close s1: %v", err)
	}

	// --- Phase 2: reopen and verify all entries survive. ---
	s2 := openStore(t, dir)
	defer s2.Close()

	for _, k := range keys {
		got, ok := s2.Get(k)
		if !ok {
			t.Errorf("Get after reopen: key %v not found", k)
			continue
		}
		if len(got.Value) == 0 {
			t.Errorf("Get after reopen: key %v has empty value", k)
		}
	}

	entries := s2.GetAll()
	if len(entries) != len(keys) {
		t.Fatalf("GetAll after reopen: got %d entries, want %d", len(entries), len(keys))
	}
}

// TestPutOverwrite verifies that Putting the same key twice replaces the value.
func TestPutOverwrite(t *testing.T) {
	dir := t.TempDir()
	s := openStore(t, dir)
	defer s.Close()

	key := makeKey(42)
	if err := s.Put(makeEntry(key, "original")); err != nil {
		t.Fatalf("Put original: %v", err)
	}
	if err := s.Put(makeEntry(key, "overwritten")); err != nil {
		t.Fatalf("Put overwrite: %v", err)
	}

	got, ok := s.Get(key)
	if !ok {
		t.Fatal("Get after overwrite: not found")
	}
	if string(got.Value) != "overwritten" {
		t.Errorf("Get after overwrite: got %q, want %q", got.Value, "overwritten")
	}

	// GetAll should still return only one entry for this key.
	all := s.GetAll()
	if len(all) != 1 {
		t.Errorf("GetAll after overwrite: got %d entries, want 1", len(all))
	}
}

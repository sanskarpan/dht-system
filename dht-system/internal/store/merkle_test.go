package store

import (
	"testing"
)

func mkey(b byte) [20]byte {
	var k [20]byte
	k[0] = b
	return k
}

func TestMerkleRootChangesOnInsert(t *testing.T) {
	m := NewMerkleTree()

	r0 := m.Root()
	m.Update(mkey(1), []byte("hello"))
	r1 := m.Root()
	if r0 == r1 {
		t.Error("root unchanged after first insert")
	}

	m.Update(mkey(2), []byte("world"))
	r2 := m.Root()
	if r1 == r2 {
		t.Error("root unchanged after second insert")
	}
}

func TestMerkleRootChangesOnUpdate(t *testing.T) {
	m := NewMerkleTree()
	m.Update(mkey(1), []byte("v1"))
	r1 := m.Root()

	m.Update(mkey(1), []byte("v2"))
	r2 := m.Root()
	if r1 == r2 {
		t.Error("root unchanged after value update")
	}
}

func TestMerkleRootChangesOnDelete(t *testing.T) {
	m := NewMerkleTree()
	m.Update(mkey(1), []byte("v1"))
	m.Update(mkey(2), []byte("v2"))
	r := m.Root()

	m.Delete(mkey(1))
	r2 := m.Root()
	if r == r2 {
		t.Error("root unchanged after delete")
	}

	// Deleting non-existent key is a no-op
	r3 := m.Root()
	m.Delete(mkey(99))
	if r3 != m.Root() {
		t.Error("root changed after deleting absent key")
	}
}

func TestMerkleSameContentsEqualRoots(t *testing.T) {
	a := NewMerkleTree()
	b := NewMerkleTree()

	pairs := [][2][]byte{
		{[]byte("k1"), []byte("val1")},
		{[]byte("k2"), []byte("val2")},
		{[]byte("k3"), []byte("val3")},
	}
	for _, p := range pairs {
		var k [20]byte
		copy(k[:], p[0])
		a.Update(k, p[1])
	}
	// Insert in reverse order into b
	for i := len(pairs) - 1; i >= 0; i-- {
		var k [20]byte
		copy(k[:], pairs[i][0])
		b.Update(k, pairs[i][1])
	}

	if a.Root() != b.Root() {
		t.Errorf("trees with same content have different roots: %x vs %x", a.Root(), b.Root())
	}
}

func TestMerkleDiffEmptyWhenEqual(t *testing.T) {
	a := NewMerkleTree()
	b := NewMerkleTree()
	a.Update(mkey(1), []byte("v"))
	b.Update(mkey(1), []byte("v"))

	diff := a.Diff(b)
	if len(diff) != 0 {
		t.Errorf("expected empty diff for equal trees, got %v", diff)
	}
}

func TestMerkleDiffDetectsDivergence(t *testing.T) {
	a := NewMerkleTree()
	b := NewMerkleTree()

	a.Update(mkey(1), []byte("same"))
	b.Update(mkey(1), []byte("same"))

	a.Update(mkey(2), []byte("version-a"))
	b.Update(mkey(2), []byte("version-b"))

	a.Update(mkey(3), []byte("only-in-a"))

	diff := a.Diff(b)
	has := func(k [20]byte) bool {
		for _, d := range diff {
			if d == k {
				return true
			}
		}
		return false
	}
	if !has(mkey(2)) {
		t.Error("diff missing mkey(2) which has different values")
	}
	if !has(mkey(3)) {
		t.Error("diff missing mkey(3) which only exists in a")
	}
	if has(mkey(1)) {
		t.Error("diff contains mkey(1) which has identical values")
	}
}

func TestMerkleLenTracksInsertDelete(t *testing.T) {
	m := NewMerkleTree()
	if m.Len() != 0 {
		t.Fatalf("expected empty tree, got %d", m.Len())
	}
	m.Update(mkey(1), []byte("v1"))
	m.Update(mkey(2), []byte("v2"))
	if m.Len() != 2 {
		t.Fatalf("expected 2 keys, got %d", m.Len())
	}
	m.Delete(mkey(1))
	if m.Len() != 1 {
		t.Fatalf("expected 1 key after delete, got %d", m.Len())
	}
}

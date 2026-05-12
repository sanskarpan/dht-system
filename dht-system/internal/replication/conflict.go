package replication

import (
	"sync"
	"time"

	"github.com/sanskarpan/dht-system/dht-system/internal/store"
)

// ConflictRecord describes a single detected (and resolved) write conflict.
type ConflictRecord struct {
	Key        [20]byte          // conflicting key
	Candidates []*store.ValueEntry // all concurrent versions
	Winner     *store.ValueEntry  // the LWW winner
	DetectedAt time.Time
}

// ConflictHistory is an in-memory ring buffer of the last N conflicts.
// It is safe for concurrent use.
type ConflictHistory struct {
	mu      sync.Mutex
	records []*ConflictRecord
	cap     int
}

// NewConflictHistory creates a conflict history with the given capacity.
func NewConflictHistory(cap int) *ConflictHistory {
	if cap <= 0 {
		cap = 100
	}
	return &ConflictHistory{cap: cap}
}

// Add appends a conflict record, evicting the oldest if at capacity.
func (h *ConflictHistory) Add(rec *ConflictRecord) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.records) >= h.cap {
		h.records = h.records[1:]
	}
	h.records = append(h.records, rec)
}

// All returns a snapshot of all stored conflict records (oldest first).
func (h *ConflictHistory) All() []*ConflictRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	cp := make([]*ConflictRecord, len(h.records))
	copy(cp, h.records)
	return cp
}

// Len returns the current number of stored records.
func (h *ConflictHistory) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.records)
}

// DefaultConflictHistory is a package-level history shared by quorum operations.
// Callers that want per-instance history should construct their own.
var DefaultConflictHistory = NewConflictHistory(100)

// Resolve selects the winning entry from a set of conflicting replicas.
//
// Resolution rules (applied in order):
//  1. If one entry causally dominates all others (its clock is After every
//     other entry's clock), return it.
//  2. If multiple entries are causally concurrent or equal, apply Last-Write-
//     Wins (LWW) using the entry's Timestamp field.
//
// Concurrent conflicts are logged to DefaultConflictHistory.
// Resolve never mutates any entry; it returns one of the input pointers.
func Resolve(entries []*store.ValueEntry) *store.ValueEntry {
	if len(entries) == 0 {
		return nil
	}
	if len(entries) == 1 {
		return entries[0]
	}

	hasConcurrent := false
	best := entries[0]
	for _, e := range entries[1:] {
		switch Compare(VectorClock(e.Clock), VectorClock(best.Clock)) {
		case After:
			best = e
		case Before:
			// keep best
		case Concurrent, Equal:
			hasConcurrent = true
			if e.Timestamp.After(best.Timestamp) {
				best = e
			}
		}
	}

	if hasConcurrent && len(entries) > 0 {
		DefaultConflictHistory.Add(&ConflictRecord{
			Key:        entries[0].Key,
			Candidates: entries,
			Winner:     best,
			DetectedAt: time.Now(),
		})
	}

	return best
}

// IsConcurrent returns true if any two entries in the slice have concurrent
// vector clocks (i.e. a genuine write conflict exists).
func IsConcurrent(entries []*store.ValueEntry) bool {
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if Compare(VectorClock(entries[i].Clock), VectorClock(entries[j].Clock)) == Concurrent {
				return true
			}
		}
	}
	return false
}

// Ensure the time package import is kept live (used indirectly by callers
// that rely on store.ValueEntry.Timestamp being a time.Time).
var _ = time.Now

// Package badgerstore provides a BadgerDB-backed persistent KV store for DHT nodes.
// It implements the same Get/Put/GetAll semantics as store.KVStore but
// survives process restarts. Values are JSON-encoded store.ValueEntry records.
package badgerstore

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/sanskarpan/dht-system/dht-system/internal/store"
)

// BadgerStore is a persistent key-value store backed by BadgerDB.
// Keys are stored as 40-character hex strings derived from the 20-byte node key.
// Values are JSON-marshalled store.ValueEntry records.
type BadgerStore struct {
	db *badger.DB
}

// New opens (or creates) a BadgerDB at the given filesystem path using default
// options. The caller must call Close when done to flush and release resources.
func New(path string) (*BadgerStore, error) {
	opts := badger.DefaultOptions(path)
	// Suppress BadgerDB's own logger output so it does not pollute test output.
	opts = opts.WithLogger(nil)

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("badgerstore.New: open %q: %w", path, err)
	}
	return &BadgerStore{db: db}, nil
}

// Close flushes all pending writes and closes the underlying BadgerDB.
func (s *BadgerStore) Close() error {
	return s.db.Close()
}

// keyToHex converts a 20-byte key to its 40-character hex representation.
func keyToHex(key [20]byte) []byte {
	h := hex.EncodeToString(key[:])
	return []byte(h)
}

// Put serialises entry as JSON and writes it under the hex-encoded key.
// Any previous value for that key is overwritten unconditionally.
func (s *BadgerStore) Put(entry *store.ValueEntry) error {
	if entry == nil {
		return fmt.Errorf("badgerstore.Put: nil entry")
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("badgerstore.Put: marshal entry: %w", err)
	}

	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set(keyToHex(entry.Key), data)
	})
}

// Get retrieves the entry stored under key. It returns (nil, false) when the
// key is absent or the entry has expired (TTL elapsed). A valid, unexpired
// entry is returned as a deep clone so callers may freely mutate it.
func (s *BadgerStore) Get(key [20]byte) (*store.ValueEntry, bool) {
	var entry store.ValueEntry

	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(keyToHex(key))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &entry)
		})
	})

	if err != nil {
		// badger.ErrKeyNotFound is the normal "not found" case; any other error
		// is also treated as a miss so callers always get a clean (nil, false).
		return nil, false
	}

	if entry.IsExpired() {
		return nil, false
	}

	return entry.Clone(), true
}

// GetAll scans the entire database and returns all non-expired entries.
// The returned slice may be empty but is never nil.
func (s *BadgerStore) GetAll() []*store.ValueEntry {
	var results []*store.ValueEntry

	_ = s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 64

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var entry store.ValueEntry
				if err := json.Unmarshal(val, &entry); err != nil {
					return err
				}
				if !entry.IsExpired() {
					results = append(results, entry.Clone())
				}
				return nil
			})
			if err != nil {
				// Skip corrupted or unreadable entries rather than aborting.
				continue
			}
		}
		return nil
	})

	if results == nil {
		results = []*store.ValueEntry{}
	}
	return results
}

// Delete removes the entry for key from the database.
// It is not an error to delete a key that does not exist.
func (s *BadgerStore) Delete(key [20]byte) error {
	return s.db.Update(func(txn *badger.Txn) error {
		err := txn.Delete(keyToHex(key))
		if err == badger.ErrKeyNotFound {
			return nil
		}
		return err
	})
}

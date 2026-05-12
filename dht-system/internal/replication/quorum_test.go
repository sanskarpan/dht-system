package replication

import (
	"context"
	"testing"

	"github.com/sanskarpan/dht-system/dht-system/internal/store"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
)

type quorumMockTransport struct {
	entries map[string]*store.ValueEntry
	writes  map[string]*store.ValueEntry
}

func (m *quorumMockTransport) FindSuccessor(context.Context, transport.NodeRef, [20]byte) (transport.NodeRef, error) {
	return transport.NodeRef{}, nil
}
func (m *quorumMockTransport) GetPredecessor(context.Context, transport.NodeRef) (*transport.NodeRef, error) {
	return nil, nil
}
func (m *quorumMockTransport) Notify(context.Context, transport.NodeRef, transport.NodeRef) error {
	return nil
}
func (m *quorumMockTransport) GetSuccessorList(context.Context, transport.NodeRef) ([]transport.NodeRef, error) {
	return nil, nil
}
func (m *quorumMockTransport) TransferKeys(context.Context, transport.NodeRef, []*store.ValueEntry) error {
	return nil
}
func (m *quorumMockTransport) UpdateFingerTable(context.Context, transport.NodeRef, transport.NodeRef, int) error {
	return nil
}
func (m *quorumMockTransport) KPing(context.Context, transport.NodeRef, transport.NodeRef) error {
	return nil
}
func (m *quorumMockTransport) KStore(context.Context, transport.NodeRef, transport.NodeRef, *store.ValueEntry) error {
	return nil
}
func (m *quorumMockTransport) FindNode(context.Context, transport.NodeRef, transport.NodeRef, [20]byte) ([]transport.NodeRef, error) {
	return nil, nil
}
func (m *quorumMockTransport) FindValue(context.Context, transport.NodeRef, transport.NodeRef, [20]byte) (*transport.FindValueResult, error) {
	return &transport.FindValueResult{}, nil
}
func (m *quorumMockTransport) Ping(context.Context, transport.NodeRef) error { return nil }
func (m *quorumMockTransport) GetEntry(_ context.Context, target transport.NodeRef, _ [20]byte) (*store.ValueEntry, error) {
	entry := m.entries[target.Addr]
	if entry == nil {
		return nil, context.DeadlineExceeded
	}
	return entry.Clone(), nil
}
func (m *quorumMockTransport) PutEntry(_ context.Context, target transport.NodeRef, _ [20]byte, entry *store.ValueEntry) error {
	if m.writes == nil {
		m.writes = make(map[string]*store.ValueEntry)
	}
	m.writes[target.Addr] = entry.Clone()
	return nil
}
func (m *quorumMockTransport) GetAllEntries(context.Context, transport.NodeRef) ([]*store.ValueEntry, error) {
	return nil, nil
}

func TestQuorumWriteMergesExistingReplicaClocks(t *testing.T) {
	key := [20]byte{9}
	replicas := []transport.NodeRef{{Addr: "r1"}, {Addr: "r2"}, {Addr: "r3"}}
	mt := &quorumMockTransport{
		entries: map[string]*store.ValueEntry{
			"r1": {Key: key, Clock: map[string]uint64{"node-a": 2}},
			"r2": {Key: key, Clock: map[string]uint64{"node-b": 3}},
		},
	}

	q := NewQuorumManager(&Config{N: 3, W: 2, R: 2}, mt, func([20]byte) []transport.NodeRef {
		return replicas
	}, nil)

	if err := q.Write(context.Background(), key, []byte("next"), "gateway"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	for _, replica := range replicas {
		written := mt.writes[replica.Addr]
		if written == nil {
			t.Fatalf("expected write for replica %s", replica.Addr)
		}
		if got, want := written.Clock["gateway"], uint64(1); got != want {
			t.Fatalf("gateway clock = %d, want %d", got, want)
		}
		if got, want := written.Clock["node-a"], uint64(2); got != want {
			t.Fatalf("node-a clock = %d, want %d", got, want)
		}
		if got, want := written.Clock["node-b"], uint64(3); got != want {
			t.Fatalf("node-b clock = %d, want %d", got, want)
		}
	}
}

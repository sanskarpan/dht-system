package transport

import (
	"context"
	"testing"

	"github.com/sanskarpan/dht-system/dht-system/internal/store"
)

type noopHandler struct{}

func (n *noopHandler) HandleFindSuccessor(context.Context, [20]byte) (NodeRef, error) {
	return NodeRef{}, nil
}
func (n *noopHandler) HandleGetPredecessor(context.Context) (*NodeRef, error)         { return nil, nil }
func (n *noopHandler) HandleNotify(context.Context, NodeRef) error                    { return nil }
func (n *noopHandler) HandleGetSuccessorList(context.Context) ([]NodeRef, error)      { return nil, nil }
func (n *noopHandler) HandleTransferKeys(context.Context, []*store.ValueEntry) error  { return nil }
func (n *noopHandler) HandleUpdateFingerTable(context.Context, NodeRef, int) error    { return nil }
func (n *noopHandler) HandleKPing(context.Context, NodeRef) error                     { return nil }
func (n *noopHandler) HandleKStore(context.Context, NodeRef, *store.ValueEntry) error { return nil }
func (n *noopHandler) HandleFindNode(context.Context, NodeRef, [20]byte) ([]NodeRef, error) {
	return nil, nil
}
func (n *noopHandler) HandleFindValue(context.Context, NodeRef, [20]byte) (*FindValueResult, error) {
	return &FindValueResult{}, nil
}
func (n *noopHandler) HandlePing(context.Context) error { return nil }
func (n *noopHandler) HandleGetEntry(context.Context, [20]byte) (*store.ValueEntry, error) {
	return &store.ValueEntry{}, nil
}
func (n *noopHandler) HandlePutEntry(context.Context, [20]byte, *store.ValueEntry) error { return nil }
func (n *noopHandler) HandleGetAllEntries(context.Context) ([]*store.ValueEntry, error) {
	return nil, nil
}

func TestInProcessTransport_ContextSenderRespectsPartition(t *testing.T) {
	tr := NewInProcessTransport(0, 0)
	tr.Register("a", &noopHandler{})
	tr.Register("b", &noopHandler{})
	tr.SetPartitions([][]string{{"a"}, {"b"}})

	ctx := WithSender(context.Background(), NodeRef{Addr: "a"})
	_, err := tr.GetEntry(ctx, NodeRef{Addr: "b"}, [20]byte{1})
	if err != ErrPartitioned {
		t.Fatalf("GetEntry with sender context err = %v, want %v", err, ErrPartitioned)
	}
}

func TestInProcessTransport_ExplicitSenderRespectsPartition(t *testing.T) {
	tr := NewInProcessTransport(0, 0)
	tr.Register("a", &noopHandler{})
	tr.Register("b", &noopHandler{})
	tr.SetPartitions([][]string{{"a"}, {"b"}})

	err := tr.KPing(context.Background(), NodeRef{Addr: "a"}, NodeRef{Addr: "b"})
	if err != ErrPartitioned {
		t.Fatalf("KPing err = %v, want %v", err, ErrPartitioned)
	}
}

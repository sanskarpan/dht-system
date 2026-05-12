package transport_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/sanskarpan/dht-system/dht-system/internal/store"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
)

// stubHandler is a minimal NodeHandler for testing.
type stubHandler struct{}

func (s *stubHandler) HandleFindSuccessor(_ context.Context, id [20]byte) (transport.NodeRef, error) {
	return transport.NodeRef{ID: id, Addr: "stub"}, nil
}
func (s *stubHandler) HandleGetPredecessor(_ context.Context) (*transport.NodeRef, error) {
	return nil, nil
}
func (s *stubHandler) HandleNotify(_ context.Context, _ transport.NodeRef) error { return nil }
func (s *stubHandler) HandleGetSuccessorList(_ context.Context) ([]transport.NodeRef, error) {
	return []transport.NodeRef{{Addr: "successor"}}, nil
}
func (s *stubHandler) HandleTransferKeys(_ context.Context, _ []*store.ValueEntry) error {
	return nil
}
func (s *stubHandler) HandleUpdateFingerTable(_ context.Context, _ transport.NodeRef, _ int) error {
	return nil
}
func (s *stubHandler) HandleKPing(_ context.Context, _ transport.NodeRef) error { return nil }
func (s *stubHandler) HandleKStore(_ context.Context, _ transport.NodeRef, _ *store.ValueEntry) error {
	return nil
}
func (s *stubHandler) HandleFindNode(_ context.Context, _ transport.NodeRef, _ [20]byte) ([]transport.NodeRef, error) {
	return nil, nil
}
func (s *stubHandler) HandleFindValue(_ context.Context, _ transport.NodeRef, key [20]byte) (*transport.FindValueResult, error) {
	return &transport.FindValueResult{Found: false}, nil
}
func (s *stubHandler) HandlePing(_ context.Context) error { return nil }
func (s *stubHandler) HandleGetEntry(_ context.Context, key [20]byte) (*store.ValueEntry, error) {
	return &store.ValueEntry{Key: key, Value: []byte("hello")}, nil
}
func (s *stubHandler) HandlePutEntry(_ context.Context, _ [20]byte, _ *store.ValueEntry) error {
	return nil
}
func (s *stubHandler) HandleGetAllEntries(_ context.Context) ([]*store.ValueEntry, error) {
	return []*store.ValueEntry{{Key: [20]byte{1}, Value: []byte("v1")}}, nil
}

// freePort returns an available TCP port.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func TestTCPTransport_RegisterAndPing(t *testing.T) {
	addr := freePort(t)
	tr := transport.NewTCPTransport()
	defer tr.Deregister(addr)

	if err := tr.Register(addr, &stubHandler{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Give the server a moment to be ready
	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	target := transport.NodeRef{Addr: addr}
	if err := tr.Ping(ctx, target); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestTCPTransport_GetEntry(t *testing.T) {
	addr := freePort(t)
	tr := transport.NewTCPTransport()
	defer tr.Deregister(addr)

	if err := tr.Register(addr, &stubHandler{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	key := [20]byte{0xab}
	target := transport.NodeRef{Addr: addr}
	entry, err := tr.GetEntry(ctx, target, key)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if string(entry.Value) != "hello" {
		t.Errorf("expected value 'hello', got %q", entry.Value)
	}
}

func TestTCPTransport_GetAllEntries(t *testing.T) {
	addr := freePort(t)
	tr := transport.NewTCPTransport()
	defer tr.Deregister(addr)

	if err := tr.Register(addr, &stubHandler{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	entries, err := tr.GetAllEntries(ctx, transport.NodeRef{Addr: addr})
	if err != nil {
		t.Fatalf("GetAllEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}

func TestTCPTransport_ConnectionReuse(t *testing.T) {
	addr := freePort(t)
	tr := transport.NewTCPTransport()
	defer tr.Deregister(addr)

	if err := tr.Register(addr, &stubHandler{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	ctx := context.Background()
	target := transport.NodeRef{Addr: addr}

	// Make 10 rapid consecutive RPCs — all should succeed via keep-alive connections
	for i := 0; i < 10; i++ {
		if err := tr.Ping(ctx, target); err != nil {
			t.Fatalf("Ping #%d: %v", i, err)
		}
	}
}

func TestTCPTransport_Deregister(t *testing.T) {
	addr := freePort(t)
	tr := transport.NewTCPTransport()

	if err := tr.Register(addr, &stubHandler{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	tr.Deregister(addr)
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// After deregistration, the server is gone — Ping should fail
	if err := tr.Ping(ctx, transport.NodeRef{Addr: addr}); err == nil {
		t.Error("expected error after Deregister, got nil")
	}
}

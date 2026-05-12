// Package transport — TCPTransport provides a real network transport
// using JSON-over-HTTP with per-address connection pooling.
//
// Each registered node starts an HTTP server that exposes POST /rpc.
// RPCs are serialised as { "method": "<name>", "args": <json> } and
// responses are { "result": <json>, "error": "<message>" }.
//
// This transport is primarily for integration testing and the
// multi-process stretch goal; the simulation uses InProcessTransport.
package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/sanskarpan/dht-system/dht-system/internal/store"
)

// ── Wire types ────────────────────────────────────────────────────────────────

type rpcRequest struct {
	Method string          `json:"method"`
	Args   json.RawMessage `json:"args"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// Helper: encode any value as json.RawMessage.
// Returns a JSON null on error to avoid sending invalid data.
func mustMarshal(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return json.RawMessage(b)
}

// ── Per-method argument structs ───────────────────────────────────────────────

type argsFindSuccessor struct {
	ID [20]byte `json:"id"`
}
type argsGetEntry struct {
	Key [20]byte `json:"key"`
}
type argsPutEntry struct {
	Key   [20]byte         `json:"key"`
	Entry *store.ValueEntry `json:"entry"`
}
type argsNotify struct {
	Sender NodeRef `json:"sender"`
}
type argsTransferKeys struct {
	Entries []*store.ValueEntry `json:"entries"`
}
type argsUpdateFingerTable struct {
	S NodeRef `json:"s"`
	I int     `json:"i"`
}
type argsKStore struct {
	Sender NodeRef          `json:"sender"`
	Entry  *store.ValueEntry `json:"entry"`
}
type argsFindNode struct {
	Sender NodeRef  `json:"sender"`
	ID     [20]byte `json:"id"`
}
type argsFindValue struct {
	Sender NodeRef  `json:"sender"`
	Key    [20]byte `json:"key"`
}
type argsKPing struct {
	Sender NodeRef `json:"sender"`
}

// ── TCPTransport ──────────────────────────────────────────────────────────────

// TCPTransport implements Transport over real TCP connections using
// JSON-encoded HTTP POST requests.
//
// Connection reuse: a per-address *http.Client with keep-alive is maintained
// in a sync.Map. Clients are created lazily on first use and remain until
// Deregister is called.
type TCPTransport struct {
	mu       sync.RWMutex
	servers  map[string]*http.Server   // addr → running HTTP server
	handlers map[string]NodeHandler    // addr → handler (for local dispatch)
	clients  sync.Map                  // addr → *http.Client (reusable)
}

// NewTCPTransport creates an empty TCPTransport.
func NewTCPTransport() *TCPTransport {
	return &TCPTransport{
		servers:  make(map[string]*http.Server),
		handlers: make(map[string]NodeHandler),
	}
}

// Register starts an HTTP RPC server for the given node and stores its handler.
// addr must be a valid "host:port" string (e.g., "127.0.0.1:9001").
func (t *TCPTransport) Register(addr string, handler NodeHandler) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.servers[addr]; exists {
		return fmt.Errorf("tcp: addr %q already registered", addr)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		t.serveRPC(handler, w, r)
	})

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("tcp: listen %q: %w", addr, err)
	}

	t.servers[addr] = srv
	t.handlers[addr] = handler

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			fmt.Printf("tcp: server %s closed unexpectedly: %v\n", addr, err)
		}
	}()
	return nil
}

// Deregister gracefully shuts down the HTTP server for the given addr and
// removes the connection-pool entry so the client is not reused.
func (t *TCPTransport) Deregister(addr string) {
	t.mu.Lock()
	srv := t.servers[addr]
	delete(t.servers, addr)
	delete(t.handlers, addr)
	t.mu.Unlock()

	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	t.clients.Delete(addr)
}

// client returns or creates a reusable *http.Client for addr.
func (t *TCPTransport) client(addr string) *http.Client {
	if v, ok := t.clients.Load(addr); ok {
		return v.(*http.Client)
	}
	c := &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
			MaxIdleConnsPerHost:   4,
			IdleConnTimeout:       30 * time.Second,
			ResponseHeaderTimeout: 5 * time.Second,
		},
		Timeout: 10 * time.Second,
	}
	actual, _ := t.clients.LoadOrStore(addr, c)
	return actual.(*http.Client)
}

// call performs a single RPC to the node at addr.
func (t *TCPTransport) call(ctx context.Context, addr, method string, args, result interface{}) error {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("tcp: marshal args: %w", err)
	}

	reqBody, err := json.Marshal(rpcRequest{Method: method, Args: argsJSON})
	if err != nil {
		return fmt.Errorf("tcp: marshal request: %w", err)
	}

	url := "http://" + addr + "/rpc"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("tcp: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := t.client(addr).Do(httpReq)
	if err != nil {
		return fmt.Errorf("tcp: %s: %w", method, err)
	}
	defer resp.Body.Close()

	var rpcResp rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return fmt.Errorf("tcp: decode response: %w", err)
	}
	if rpcResp.Error != "" {
		return fmt.Errorf("tcp: remote error: %s", rpcResp.Error)
	}
	if result != nil && rpcResp.Result != nil {
		return json.Unmarshal(rpcResp.Result, result)
	}
	return nil
}

// serveRPC dispatches an incoming RPC to the local NodeHandler.
func (t *TCPTransport) serveRPC(h NodeHandler, w http.ResponseWriter, r *http.Request) {
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, fmt.Sprintf("decode: %v", err))
		return
	}

	ctx := r.Context()
	var result interface{}
	var rpcErr error

	switch req.Method {
	// ── Chord ──────────────────────────────────────────────────────────────
	case "FindSuccessor":
		var a argsFindSuccessor
		rpcErr = json.Unmarshal(req.Args, &a)
		if rpcErr == nil {
			result, rpcErr = h.HandleFindSuccessor(ctx, a.ID)
		}
	case "GetPredecessor":
		result, rpcErr = h.HandleGetPredecessor(ctx)
	case "Notify":
		var a argsNotify
		rpcErr = json.Unmarshal(req.Args, &a)
		if rpcErr == nil {
			rpcErr = h.HandleNotify(ctx, a.Sender)
		}
	case "GetSuccessorList":
		result, rpcErr = h.HandleGetSuccessorList(ctx)
	case "TransferKeys":
		var a argsTransferKeys
		rpcErr = json.Unmarshal(req.Args, &a)
		if rpcErr == nil {
			rpcErr = h.HandleTransferKeys(ctx, a.Entries)
		}
	case "UpdateFingerTable":
		var a argsUpdateFingerTable
		rpcErr = json.Unmarshal(req.Args, &a)
		if rpcErr == nil {
			rpcErr = h.HandleUpdateFingerTable(ctx, a.S, a.I)
		}

	// ── Kademlia ───────────────────────────────────────────────────────────
	case "KPing":
		var a argsKPing
		rpcErr = json.Unmarshal(req.Args, &a)
		if rpcErr == nil {
			rpcErr = h.HandleKPing(ctx, a.Sender)
		}
	case "KStore":
		var a argsKStore
		rpcErr = json.Unmarshal(req.Args, &a)
		if rpcErr == nil {
			rpcErr = h.HandleKStore(ctx, a.Sender, a.Entry)
		}
	case "FindNode":
		var a argsFindNode
		rpcErr = json.Unmarshal(req.Args, &a)
		if rpcErr == nil {
			result, rpcErr = h.HandleFindNode(ctx, a.Sender, a.ID)
		}
	case "FindValue":
		var a argsFindValue
		rpcErr = json.Unmarshal(req.Args, &a)
		if rpcErr == nil {
			result, rpcErr = h.HandleFindValue(ctx, a.Sender, a.Key)
		}

	// ── Common ─────────────────────────────────────────────────────────────
	case "Ping":
		rpcErr = h.HandlePing(ctx)
	case "GetEntry":
		var a argsGetEntry
		rpcErr = json.Unmarshal(req.Args, &a)
		if rpcErr == nil {
			result, rpcErr = h.HandleGetEntry(ctx, a.Key)
		}
	case "PutEntry":
		var a argsPutEntry
		rpcErr = json.Unmarshal(req.Args, &a)
		if rpcErr == nil {
			rpcErr = h.HandlePutEntry(ctx, a.Key, a.Entry)
		}
	case "GetAllEntries":
		result, rpcErr = h.HandleGetAllEntries(ctx)

	default:
		writeRPCError(w, fmt.Sprintf("unknown method %q", req.Method))
		return
	}

	if rpcErr != nil {
		writeRPCError(w, rpcErr.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rpcResponse{Result: mustMarshal(result)})
}

func writeRPCError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // always 200; error is in payload
	_ = json.NewEncoder(w).Encode(rpcResponse{Error: msg})
}

// ── Transport interface implementation ────────────────────────────────────────

func (t *TCPTransport) FindSuccessor(ctx context.Context, target NodeRef, id [20]byte) (NodeRef, error) {
	var out NodeRef
	return out, t.call(ctx, target.Addr, "FindSuccessor", argsFindSuccessor{ID: id}, &out)
}

func (t *TCPTransport) GetPredecessor(ctx context.Context, target NodeRef) (*NodeRef, error) {
	var out *NodeRef
	return out, t.call(ctx, target.Addr, "GetPredecessor", nil, &out)
}

func (t *TCPTransport) Notify(ctx context.Context, target NodeRef, sender NodeRef) error {
	return t.call(ctx, target.Addr, "Notify", argsNotify{Sender: sender}, nil)
}

func (t *TCPTransport) GetSuccessorList(ctx context.Context, target NodeRef) ([]NodeRef, error) {
	var out []NodeRef
	return out, t.call(ctx, target.Addr, "GetSuccessorList", nil, &out)
}

func (t *TCPTransport) TransferKeys(ctx context.Context, target NodeRef, entries []*store.ValueEntry) error {
	return t.call(ctx, target.Addr, "TransferKeys", argsTransferKeys{Entries: entries}, nil)
}

func (t *TCPTransport) UpdateFingerTable(ctx context.Context, target NodeRef, s NodeRef, i int) error {
	return t.call(ctx, target.Addr, "UpdateFingerTable", argsUpdateFingerTable{S: s, I: i}, nil)
}

func (t *TCPTransport) KPing(ctx context.Context, sender NodeRef, target NodeRef) error {
	return t.call(ctx, target.Addr, "KPing", argsKPing{Sender: sender}, nil)
}

func (t *TCPTransport) KStore(ctx context.Context, sender NodeRef, target NodeRef, entry *store.ValueEntry) error {
	return t.call(ctx, target.Addr, "KStore", argsKStore{Sender: sender, Entry: entry}, nil)
}

func (t *TCPTransport) FindNode(ctx context.Context, sender NodeRef, target NodeRef, id [20]byte) ([]NodeRef, error) {
	var out []NodeRef
	return out, t.call(ctx, target.Addr, "FindNode", argsFindNode{Sender: sender, ID: id}, &out)
}

func (t *TCPTransport) FindValue(ctx context.Context, sender NodeRef, target NodeRef, key [20]byte) (*FindValueResult, error) {
	var out FindValueResult
	return &out, t.call(ctx, target.Addr, "FindValue", argsFindValue{Sender: sender, Key: key}, &out)
}

func (t *TCPTransport) Ping(ctx context.Context, target NodeRef) error {
	return t.call(ctx, target.Addr, "Ping", nil, nil)
}

func (t *TCPTransport) GetEntry(ctx context.Context, target NodeRef, key [20]byte) (*store.ValueEntry, error) {
	var out store.ValueEntry
	err := t.call(ctx, target.Addr, "GetEntry", argsGetEntry{Key: key}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (t *TCPTransport) PutEntry(ctx context.Context, target NodeRef, key [20]byte, entry *store.ValueEntry) error {
	return t.call(ctx, target.Addr, "PutEntry", argsPutEntry{Key: key, Entry: entry}, nil)
}

func (t *TCPTransport) GetAllEntries(ctx context.Context, target NodeRef) ([]*store.ValueEntry, error) {
	var out []*store.ValueEntry
	return out, t.call(ctx, target.Addr, "GetAllEntries", nil, &out)
}

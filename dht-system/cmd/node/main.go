// Command node is a standalone DHT node process.
//
// It starts a single Chord or Kademlia node that communicates with peers via
// the TCP transport (JSON-over-HTTP RPCs). An embedded HTTP server provides a
// minimal REST API for health checks and key-value operations.
//
// Usage:
//
//	node [flags]
//
// Flags:
//
//	--addr       Listen address for both the RPC server and HTTP API (default "127.0.0.1:0")
//	--bootstrap  Address of an existing node to join; empty means create a new ring/network
//	--protocol   DHT protocol to use: "chord" or "kademlia" (default "chord")
//	--data-dir   Directory for BadgerDB persistence; empty uses an in-memory store
//
// HTTP API:
//
//	GET  /health        → 200 {"status":"ok","addr":"<addr>","protocol":"<proto>"}
//	POST /kv            → body {"key":"<string>","value":"<string>"} → 201 on success
//	GET  /kv/{key}      → 200 {"key":"...","value":"..."} or 404
package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/sanskarpan/dht-system/dht-system/internal/badgerstore"
	"github.com/sanskarpan/dht-system/dht-system/internal/chord"
	"github.com/sanskarpan/dht-system/dht-system/internal/consistent"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/kademlia"
	"github.com/sanskarpan/dht-system/dht-system/internal/store"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
)

// ── CLI flags ─────────────────────────────────────────────────────────────────

var (
	flagAddr      = flag.String("addr", "127.0.0.1:0", "listen address (host:port; port 0 picks a random free port)")
	flagBootstrap = flag.String("bootstrap", "", "address of a peer to join; empty creates a new ring/network")
	flagProtocol  = flag.String("protocol", "chord", "DHT protocol: chord or kademlia")
	flagDataDir   = flag.String("data-dir", "", "directory for BadgerDB persistence; empty uses in-memory store")
)

// ── node abstraction ──────────────────────────────────────────────────────────

// dhtNode is the common interface that both ChordNode and KademliaNode satisfy
// for the operations needed by the CLI.
type dhtNode interface {
	// Addr returns the network address this node is listening on.
	nodeAddr() string
	// Start launches background goroutines.
	start()
	// Leave gracefully departs the DHT (if the protocol supports it).
	leave(ctx context.Context) error
	// Put stores a key-value pair locally.
	put(key, value string) error
	// Get retrieves a value by key. Returns ("", false) if not found.
	get(key string) (string, bool)
	// Register plugs this node into the TCP transport so it can receive RPCs.
	register(t *transport.TCPTransport) error
}

// ── Chord wrapper ─────────────────────────────────────────────────────────────

type chordWrapper struct {
	node  *chord.ChordNode
	bstor *badgerstore.BadgerStore // non-nil when --data-dir is set
}

func newChordWrapper(addr string, bs *badgerstore.BadgerStore, bus events.EventEmitter) *chordWrapper {
	zapL, _ := zap.NewProduction()
	n := chord.NewChordNode(addr, nil, transport.NewTCPTransport(), bus, zapL)
	return &chordWrapper{node: n, bstor: bs}
}

func (w *chordWrapper) nodeAddr() string { return w.node.Addr }

func (w *chordWrapper) start() { w.node.Start() }

func (w *chordWrapper) leave(ctx context.Context) error { return w.node.Leave(ctx) }

func (w *chordWrapper) register(t *transport.TCPTransport) error {
	return t.Register(w.node.Addr, w.node)
}

func (w *chordWrapper) put(key, value string) error {
	k := sha1.Sum([]byte(key))
	entry := &store.ValueEntry{
		Key:       k,
		Value:     []byte(value),
		Clock:     map[string]uint64{consistent.IDToHex(w.node.ID): 1},
		Timestamp: time.Now(),
		TTL:       0,
		NodeID:    consistent.IDToHex(w.node.ID),
	}
	w.node.Store.Put(entry)
	if w.bstor != nil {
		return w.bstor.Put(entry)
	}
	return nil
}

func (w *chordWrapper) get(key string) (string, bool) {
	k := sha1.Sum([]byte(key))
	// Try in-memory store first (fast path).
	if entry, ok := w.node.Store.Get(k); ok {
		return string(entry.Value), true
	}
	// Fall back to BadgerDB on a cache miss.
	if w.bstor != nil {
		if entry, ok := w.bstor.Get(k); ok {
			// Warm the in-memory cache.
			w.node.Store.Put(entry)
			return string(entry.Value), true
		}
	}
	return "", false
}

// ── Kademlia wrapper ──────────────────────────────────────────────────────────

type kademliaWrapper struct {
	node  *kademlia.KademliaNode
	bstor *badgerstore.BadgerStore
}

func newKademliaWrapper(addr string, bs *badgerstore.BadgerStore, bus events.EventEmitter) *kademliaWrapper {
	zapL, _ := zap.NewProduction()
	n := kademlia.NewKademliaNode(addr, nil, transport.NewTCPTransport(), bus, zapL)
	return &kademliaWrapper{node: n, bstor: bs}
}

func (w *kademliaWrapper) nodeAddr() string { return w.node.Addr }

func (w *kademliaWrapper) start() { w.node.Start() }

// Kademlia does not have a formal Leave RPC; just stop background goroutines.
func (w *kademliaWrapper) leave(_ context.Context) error {
	w.node.Stop()
	return nil
}

func (w *kademliaWrapper) register(t *transport.TCPTransport) error {
	return t.Register(w.node.Addr, w.node)
}

func (w *kademliaWrapper) put(key, value string) error {
	k := sha1.Sum([]byte(key))
	entry := &store.ValueEntry{
		Key:       k,
		Value:     []byte(value),
		Clock:     map[string]uint64{consistent.IDToHex([20]byte(w.node.ID)): 1},
		Timestamp: time.Now(),
		TTL:       0,
		NodeID:    consistent.IDToHex([20]byte(w.node.ID)),
	}
	w.node.KVStore.Put(entry)
	if w.bstor != nil {
		return w.bstor.Put(entry)
	}
	return nil
}

func (w *kademliaWrapper) get(key string) (string, bool) {
	k := sha1.Sum([]byte(key))
	if entry, ok := w.node.KVStore.Get(k); ok {
		return string(entry.Value), true
	}
	if w.bstor != nil {
		if entry, ok := w.bstor.Get(k); ok {
			w.node.KVStore.Put(entry)
			return string(entry.Value), true
		}
	}
	return "", false
}

// ── HTTP handlers ─────────────────────────────────────────────────────────────

type server struct {
	node     dhtNode
	protocol string
	mux      *http.ServeMux
}

func newServer(n dhtNode, protocol string) *server {
	s := &server{node: n, protocol: protocol, mux: http.NewServeMux()}
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/kv/", s.handleKVGet)
	s.mux.HandleFunc("/kv", s.handleKVPost)
	return s
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// GET /health
func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":   "ok",
		"addr":     s.node.nodeAddr(),
		"protocol": s.protocol,
	})
}

// POST /kv   body: {"key":"...","value":"..."}
func (s *server) handleKVPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, fmt.Sprintf("bad request: %v", err), http.StatusBadRequest)
		return
	}
	if body.Key == "" {
		http.Error(w, "key must not be empty", http.StatusBadRequest)
		return
	}
	if err := s.node.put(body.Key, body.Value); err != nil {
		http.Error(w, fmt.Sprintf("store error: %v", err), http.StatusInternalServerError)
		return
	}
	h := sha1.Sum([]byte(body.Key))
	keyHash := hex.EncodeToString(h[:])
	writeJSON(w, http.StatusCreated, map[string]string{
		"key":     body.Key,
		"keyHash": keyHash,
	})
}

// GET /kv/{key}
func (s *server) handleKVGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Strip the "/kv/" prefix.
	key := strings.TrimPrefix(r.URL.Path, "/kv/")
	if key == "" {
		http.Error(w, "key must not be empty", http.StatusBadRequest)
		return
	}
	value, ok := s.node.get(key)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"key":   key,
		"value": value,
	})
}

// ── main ──────────────────────────────────────────────────────────────────────

func main() {
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// ── Resolve the listen address (handle port 0) ────────────────────────────
	// Allocate a listener early so we know the actual port when 0 is given.
	ln, err := net.Listen("tcp", *flagAddr)
	if err != nil {
		logger.Error("failed to bind address", "addr", *flagAddr, "err", err)
		os.Exit(1)
	}
	resolvedAddr := ln.Addr().String()
	// Close the probe listener; the transport and HTTP server will open their own.
	ln.Close()

	// ── Optional BadgerDB persistence ────────────────────────────────────────
	var bstor *badgerstore.BadgerStore
	if *flagDataDir != "" {
		bstor, err = badgerstore.New(*flagDataDir)
		if err != nil {
			logger.Error("failed to open BadgerDB", "data-dir", *flagDataDir, "err", err)
			os.Exit(1)
		}
		logger.Info("BadgerDB opened", "data-dir", *flagDataDir)
	}

	// ── Event bus ────────────────────────────────────────────────────────────
	bus := events.NewEventBus()

	// ── Create node ───────────────────────────────────────────────────────────
	tcp := transport.NewTCPTransport()

	var n dhtNode
	protocol := strings.ToLower(*flagProtocol)
	switch protocol {
	case "chord":
		cw := newChordWrapper(resolvedAddr, bstor, bus)
		cw.node.Transport = tcp
		n = cw
	case "kademlia":
		kw := newKademliaWrapper(resolvedAddr, bstor, bus)
		kw.node.Transport = tcp
		n = kw
	default:
		logger.Error("unknown protocol; use 'chord' or 'kademlia'", "protocol", *flagProtocol)
		os.Exit(1)
	}

	// Register with the TCP transport so this node can receive RPCs.
	if err := n.register(tcp); err != nil {
		logger.Error("failed to register with TCP transport", "addr", resolvedAddr, "err", err)
		os.Exit(1)
	}

	// ── Seed in-memory store from BadgerDB (warm-up after restart) ────────────
	if bstor != nil {
		seedFromBadger(n, bstor, logger)
	}

	// ── Join or create ────────────────────────────────────────────────────────
	ctx := context.Background()
	if *flagBootstrap != "" {
		if err := joinDHT(ctx, n, *flagBootstrap, protocol, logger); err != nil {
			logger.Error("failed to join DHT", "bootstrap", *flagBootstrap, "err", err)
			os.Exit(1)
		}
	} else {
		createDHT(n, protocol, logger)
	}

	// ── Start background stabilization ────────────────────────────────────────
	n.start()

	logger.Info("node started",
		"addr", n.nodeAddr(),
		"protocol", protocol,
		"bootstrap", *flagBootstrap,
		"data-dir", *flagDataDir,
	)

	// ── HTTP API server ───────────────────────────────────────────────────────
	// The HTTP API shares the same address as the RPC server but we need a
	// separate port. We pick the next available port above the RPC port so the
	// operator can derive the API URL from the node address.
	apiAddr, err := nextFreeAddr(n.nodeAddr())
	if err != nil {
		logger.Error("failed to pick API port", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:         apiAddr,
		Handler:      newServer(n, protocol),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("HTTP API listening", "api-addr", apiAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP API error", "err", err)
		}
	}()

	// ── Graceful shutdown on SIGINT / SIGTERM ─────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutdown signal received, leaving DHT...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Stop the HTTP server first so no new requests arrive.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("HTTP server shutdown error", "err", err)
	}

	// Leave the DHT gracefully.
	if err := n.leave(shutdownCtx); err != nil {
		logger.Warn("DHT leave error", "err", err)
	}

	// Close persistent store.
	if bstor != nil {
		if err := bstor.Close(); err != nil {
			logger.Warn("BadgerDB close error", "err", err)
		}
	}

	bus.Stop()
	logger.Info("node stopped cleanly")
}

// ── helpers ───────────────────────────────────────────────────────────────────

// joinDHT contacts the bootstrap node and joins the ring/network.
func joinDHT(ctx context.Context, n dhtNode, bootstrapAddr, protocol string, logger *slog.Logger) error {
	logger.Info("joining DHT", "bootstrap", bootstrapAddr)
	switch w := n.(type) {
	case *chordWrapper:
		bootstrapID := consistent.NodeIDFromAddr(bootstrapAddr)
		return w.node.Join(ctx, transport.NodeRef{ID: bootstrapID, Addr: bootstrapAddr})
	case *kademliaWrapper:
		bootstrapID := consistent.NodeIDFromAddr(bootstrapAddr)
		return w.node.Join(ctx, kademlia.Contact{
			ID:       kademlia.NodeID(bootstrapID),
			Addr:     bootstrapAddr,
			LastSeen: time.Now(),
		})
	}
	return fmt.Errorf("joinDHT: unknown node type for protocol %q", protocol)
}

// createDHT initialises a fresh ring/network with this node as the sole member.
func createDHT(n dhtNode, protocol string, logger *slog.Logger) {
	logger.Info("creating new DHT ring/network")
	switch w := n.(type) {
	case *chordWrapper:
		w.node.CreateRing()
	case *kademliaWrapper:
		w.node.CreateNetwork()
	}
}

// seedFromBadger pre-populates the in-memory store from persisted entries so
// a restarted node can serve reads before the DHT re-routes traffic to it.
func seedFromBadger(n dhtNode, bstor *badgerstore.BadgerStore, logger *slog.Logger) {
	entries := bstor.GetAll()
	if len(entries) == 0 {
		return
	}
	switch w := n.(type) {
	case *chordWrapper:
		for _, e := range entries {
			w.node.Store.Put(e)
		}
	case *kademliaWrapper:
		for _, e := range entries {
			w.node.KVStore.Put(e)
		}
	}
	logger.Info("seeded in-memory store from BadgerDB", "entries", len(entries))
}

// nextFreeAddr returns a free TCP address on the same host as nodeAddr,
// using any available port (OS-assigned).
func nextFreeAddr(nodeAddr string) (string, error) {
	host, _, err := net.SplitHostPort(nodeAddr)
	if err != nil {
		return "", fmt.Errorf("nextFreeAddr: parse %q: %w", nodeAddr, err)
	}
	ln, err := net.Listen("tcp", host+":0")
	if err != nil {
		return "", fmt.Errorf("nextFreeAddr: listen: %w", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr, nil
}

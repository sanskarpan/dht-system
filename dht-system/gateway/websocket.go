package gateway

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	gorillaws "github.com/gorilla/websocket"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/simulation"
	"go.uber.org/zap"
)

// WebSocket timing constants. Adjust via environment or config in production.
const (
	// wsPingInterval controls how often the server sends a WebSocket ping frame.
	wsPingInterval = 30 * time.Second
	// wsReadDeadline is the maximum time to wait for a pong after a ping.
	wsReadDeadline = 60 * time.Second
	// ringStateBroadcastInterval is the period between periodic ring_state pushes.
	ringStateBroadcastInterval = 5 * time.Second
)

// NOTE: CheckOrigin accepts all origins for local development.
// In production, validate r.Header.Get("Origin") against an allowlist.
var upgrader = gorillaws.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type client struct {
	conn     *gorillaws.Conn
	send     chan events.Event
	done     chan struct{}
	doneOnce sync.Once
	types    []events.EventType
	mu       sync.Mutex
}

// Hub manages all WebSocket clients.
type Hub struct {
	mu       sync.RWMutex
	clients  map[*client]bool
	bus      *events.EventBus
	orch     *simulation.Orchestrator
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewHub creates a Hub.
func NewHub(bus *events.EventBus, orch *simulation.Orchestrator) *Hub {
	return &Hub{
		clients: make(map[*client]bool),
		bus:     bus,
		orch:    orch,
		stopCh:  make(chan struct{}),
	}
}

// Run starts the hub's event dispatch loop. Call in a goroutine.
func (h *Hub) Run() {
	sub := h.bus.Subscribe([]events.EventType{events.EventAll})
	defer h.bus.Unsubscribe(sub)

	ticker := time.NewTicker(ringStateBroadcastInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopCh:
			return
		case event, ok := <-sub:
			if !ok {
				return
			}
			h.broadcast(event)
		case <-ticker.C:
			// Periodic ring_state broadcast
			h.broadcastRingState()
		}
	}
}

// Stop drains the hub by preventing new broadcasts and closing active client sockets.
func (h *Hub) Stop() {
	h.stopOnce.Do(func() {
		close(h.stopCh)

		h.mu.Lock()
		clients := make([]*client, 0, len(h.clients))
		for c := range h.clients {
			clients = append(clients, c)
		}
		h.clients = make(map[*client]bool)
		h.mu.Unlock()

		for _, c := range clients {
			c.stop()
			if c.conn != nil {
				_ = c.conn.Close()
			}
		}
	})
}

func (c *client) stop() {
	c.doneOnce.Do(func() {
		close(c.done)
	})
}

func (h *Hub) broadcast(event events.Event) {
	// Increment Prometheus counters for key event types.
	switch event.Type {
	case events.EventStabilize:
		var payload events.StabilizePayload
		if err := json.Unmarshal(event.Payload, &payload); err == nil {
			DHTStabilizeCycles.WithLabelValues(payload.NodeID).Inc()
		} else {
			DHTStabilizeCycles.WithLabelValues("").Inc()
		}
	case events.EventGossipSync:
		var payload events.GossipSyncPayload
		if err := json.Unmarshal(event.Payload, &payload); err == nil {
			DHTGossipSyncs.WithLabelValues(payload.FromNode).Inc()
		} else {
			DHTGossipSyncs.WithLabelValues("").Inc()
		}
	}

	h.mu.RLock()
	cls := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		cls = append(cls, c)
	}
	h.mu.RUnlock()

	for _, c := range cls {
		c.mu.Lock()
		matched := matchEventTypes(c.types, event.Type)
		c.mu.Unlock()
		if matched {
			select {
			case c.send <- event:
			default:
				// client too slow; drop
			}
		}
	}
}

func (h *Hub) broadcastRingState() {
	state := h.orch.GetNetworkState()
	payload, err := json.Marshal(mapNetworkState(state))
	if err != nil {
		log.Printf("broadcastRingState: marshal error: %v", err)
		return
	}
	event := events.MakeEvent(events.EventRingState, json.RawMessage(payload))
	h.broadcast(event)
}

func matchEventTypes(types []events.EventType, t events.EventType) bool {
	for _, ty := range types {
		if ty == events.EventAll || ty == t {
			return true
		}
	}
	return false
}

// HandleUpgrade upgrades an HTTP connection to WebSocket.
func (h *Hub) HandleUpgrade(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		requestLogger(c).Warn("WS upgrade error", zap.Error(err))
		return
	}

	cl := &client{
		conn:  conn,
		send:  make(chan events.Event, 256),
		done:  make(chan struct{}),
		types: nil,
	}

	h.mu.Lock()
	h.clients[cl] = true
	h.mu.Unlock()
	DHTWebSocketClientsConnected.Inc()

	// Send current ring state on connect
	state := h.orch.GetNetworkState()
	payload, err := json.Marshal(mapNetworkState(state))
	if err == nil {
		initialEvent := events.MakeEvent(events.EventRingState, json.RawMessage(payload))
		cl.send <- initialEvent
	}

	go h.writePump(cl)
	go h.readPump(cl)
}

func (h *Hub) writePump(cl *client) {
	ticker := time.NewTicker(wsPingInterval)
	defer func() {
		ticker.Stop()
		cl.conn.Close()
		h.mu.Lock()
		delete(h.clients, cl)
		h.mu.Unlock()
		DHTWebSocketClientsConnected.Dec()
	}()

	for {
		select {
		case <-cl.done:
			return
		case event, ok := <-cl.send:
			if !ok {
				return
			}
			if err := cl.conn.WriteJSON(event); err != nil {
				return
			}
		case <-ticker.C:
			if err := cl.conn.WriteMessage(gorillaws.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *Hub) readPump(cl *client) {
	defer cl.stop()

	cl.conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
	cl.conn.SetPongHandler(func(string) error {
		cl.conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
		return nil
	})

	type subscribeMsg struct {
		Action string             `json:"action"`
		Events []events.EventType `json:"events"`
	}

	for {
		_, msg, err := cl.conn.ReadMessage()
		if err != nil {
			return
		}
		var sub subscribeMsg
		if err := json.Unmarshal(msg, &sub); err == nil && sub.Action == "subscribe" {
			cl.mu.Lock()
			cl.types = sub.Events
			cl.mu.Unlock()
		}
	}
}

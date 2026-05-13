package gateway

import (
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/simulation"
)

func TestHubStopClosesActiveClients(t *testing.T) {
	bus := events.NewEventBus()
	orch := simulation.NewOrchestrator(simulation.DefaultConfig(), bus)
	srv := NewServer(orch, bus, 0)

	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	parsed, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}
	parsed.Scheme = "ws"
	parsed.Path = "/ws"

	conn, _, err := websocket.DefaultDialer.Dial(parsed.String(), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read initial ring state: %v", err)
	}

	srv.hub.Stop()

	srv.hub.mu.RLock()
	if got := len(srv.hub.clients); got != 0 {
		srv.hub.mu.RUnlock()
		t.Fatalf("expected no clients after stop, got %d", got)
	}
	srv.hub.mu.RUnlock()

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected websocket read to fail after hub stop")
	}
}

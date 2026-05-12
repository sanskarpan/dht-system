package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/simulation"
)

func testConfig(protocol string) *simulation.Config {
	cfg := simulation.DefaultConfig()
	cfg.Protocol = protocol
	cfg.StabilizeInterval = 30 * time.Millisecond
	cfg.FixFingersInterval = 50 * time.Millisecond
	cfg.RepublishInterval = 10 * time.Minute
	cfg.ReplicationN = 3
	cfg.WriteQuorum = 2
	cfg.ReadQuorum = 2
	return cfg
}

func buildTestServer(t *testing.T, protocol string, nodeCount int) (*simulation.Orchestrator, *Server) {
	t.Helper()

	bus := events.NewEventBus()
	orch := simulation.NewOrchestrator(testConfig(protocol), bus)
	for i := 0; i < nodeCount; i++ {
		if _, err := orch.SpawnNode(""); err != nil {
			t.Fatalf("SpawnNode %d failed: %v", i, err)
		}
	}
	time.Sleep(800 * time.Millisecond)

	t.Cleanup(func() {
		bus.Stop()
	})

	return orch, NewServer(orch, bus, 0)
}

func TestGetKeyUsesQuorumRead(t *testing.T) {
	orch, server := buildTestServer(t, "kademlia", 3)

	const key = "quorum-read-key"
	if err := orch.Insert(key, "value-1"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kv/"+key, nil)
	rec := httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET before partition status=%d body=%s", rec.Code, rec.Body.String())
	}

	var okResp simulation.KeyState
	if err := json.Unmarshal(rec.Body.Bytes(), &okResp); err != nil {
		t.Fatalf("unmarshal success response: %v", err)
	}
	if okResp.Value != "value-1" {
		t.Fatalf("GET before partition value=%q want %q", okResp.Value, "value-1")
	}

	state := orch.GetNetworkState()
	addrByID := make(map[string]string, len(state.Nodes))
	for _, node := range state.Nodes {
		addrByID[node.ID] = node.Addr
	}

	replicas := orch.GetKeyReplicas(key)
	if len(replicas) != 3 {
		t.Fatalf("expected 3 replicas, got %d", len(replicas))
	}

	groups := make([][]string, 0, len(replicas))
	for _, replica := range replicas {
		addr := addrByID[replica.NodeID]
		if addr == "" {
			t.Fatalf("missing addr for replica %s", replica.NodeID)
		}
		groups = append(groups, []string{addr})
	}
	orch.SetPartition(groups)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/kv/"+key, nil)
	rec = httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET during quorum loss status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestStartNetworkResetsExistingClusterAndAppliesProtocol(t *testing.T) {
	_, server := buildTestServer(t, "chord", 2)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/network/start",
		bytes.NewBufferString(`{"protocol":"kademlia","nodeCount":3}`),
	)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("startNetwork status=%d body=%s", rec.Code, rec.Body.String())
	}

	var state simulation.NetworkState
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatalf("unmarshal network state: %v", err)
	}
	if state.Protocol != "kademlia" {
		t.Fatalf("protocol=%q want %q", state.Protocol, "kademlia")
	}
	if len(state.Nodes) != 3 {
		t.Fatalf("node count=%d want %d", len(state.Nodes), 3)
	}
	for i, node := range state.Nodes {
		if node.Protocol != "kademlia" {
			t.Fatalf("node %d protocol=%q want %q", i, node.Protocol, "kademlia")
		}
	}
}

func TestFaultInjectionEndpoints(t *testing.T) {
	orch, server := buildTestServer(t, "kademlia", 4)
	state := orch.GetNetworkState()
	if len(state.Nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(state.Nodes))
	}

	groupA := []string{state.Nodes[0].ID, state.Nodes[1].ID}
	groupB := []string{state.Nodes[2].ID, state.Nodes[3].ID}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/network/partition",
		bytes.NewBufferString(`{"groups":[["`+groupA[0]+`","`+groupA[1]+`"],["`+groupB[0]+`","`+groupB[1]+`"]]}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("partition status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/network/faults", nil)
	rec = httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("faults status=%d body=%s", rec.Code, rec.Body.String())
	}
	var faults struct {
		Partitions [][]string `json:"partitions"`
		Links      []struct {
			From      string `json:"from"`
			To        string `json:"to"`
			LatencyMs int64  `json:"latencyMs"`
		} `json:"links"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &faults); err != nil {
		t.Fatalf("unmarshal faults: %v", err)
	}
	if len(faults.Partitions) != 2 {
		t.Fatalf("partitions=%d want %d", len(faults.Partitions), 2)
	}

	req = httptest.NewRequest(
		http.MethodPut,
		"/api/v1/network/links",
		bytes.NewBufferString(`{"from":"`+state.Nodes[0].ID+`","to":"`+state.Nodes[1].ID+`","latencyMs":25}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("link latency status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/network/faults", nil)
	rec = httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("faults after link status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &faults); err != nil {
		t.Fatalf("unmarshal faults after link: %v", err)
	}
	if len(faults.Links) != 1 || faults.Links[0].LatencyMs != 25 {
		t.Fatalf("links=%+v want one 25ms link", faults.Links)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/network/links", nil)
	rec = httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear links status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/network/heal", nil)
	rec = httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("heal status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/network/reset", nil)
	rec = httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/network/faults", nil)
	rec = httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("faults after reset status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &faults); err != nil {
		t.Fatalf("unmarshal faults after reset: %v", err)
	}
	if len(faults.Partitions) != 0 || len(faults.Links) != 0 {
		t.Fatalf("faults after reset = %+v, want empty", faults)
	}
}

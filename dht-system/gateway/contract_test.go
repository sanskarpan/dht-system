package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestRESTAndWebSocketSchemasMatchFrontendContracts(t *testing.T) {
	orch, server := buildTestServer(t, "kademlia", 3)

	const key = "contract-key"
	if err := orch.Insert(key, "contract-value"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	state := decodeJSONMap(t, requestBody(t, server, httptest.NewRequest(http.MethodGet, "/api/v1/network/state", nil)))
	assertNetworkStateContract(t, state)

	config := decodeJSONMap(t, requestBody(t, server, httptest.NewRequest(http.MethodGet, "/api/v1/network/config", nil)))
	assertConfigContract(t, config)

	lookupReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/lookup",
		strings.NewReader(`{"key":"`+key+`"}`),
	)
	lookupReq.Header.Set("Content-Type", "application/json")
	lookup := decodeJSONMap(t, requestBody(t, server, lookupReq))
	assertLookupTraceContract(t, lookup)

	ts := httptest.NewServer(server.Router())
	t.Cleanup(ts.Close)

	parsed, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse websocket url: %v", err)
	}
	parsed.Scheme = "ws"
	parsed.Path = "/ws"

	conn, _, err := websocket.DefaultDialer.Dial(parsed.String(), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})

	var event struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := conn.ReadJSON(&event); err != nil {
		t.Fatalf("read initial websocket event: %v", err)
	}
	if event.Type != "ring_state" {
		t.Fatalf("initial websocket event type=%q want ring_state", event.Type)
	}

	ringState := decodeJSONMap(t, event.Payload)
	assertNetworkStateContract(t, ringState)
}

func requestBody(t *testing.T, server *Server, req *http.Request) []byte {
	t.Helper()

	rec := httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s status=%d body=%s", req.Method, req.URL.Path, rec.Code, rec.Body.String())
	}
	return rec.Body.Bytes()
}

func decodeJSONMap(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	return out
}

func assertNetworkStateContract(t *testing.T, state map[string]any) {
	t.Helper()

	requireStringField(t, state, "protocol")
	nodes := requireArrayField(t, state, "nodes")
	if len(nodes) == 0 {
		t.Fatal("expected at least one node in network state")
	}
	for _, raw := range nodes {
		node := requireObjectValue(t, raw, "nodes[]")
		requireStringField(t, node, "id")
		requireStringField(t, node, "addr")
		requireStringField(t, node, "status")
		requireStringField(t, node, "protocol")
		requireNumberField(t, node, "keyCount")
	}

	keys := requireArrayField(t, state, "keys")
	for _, raw := range keys {
		key := requireObjectValue(t, raw, "keys[]")
		requireStringField(t, key, "id")
		requireStringField(t, key, "key")
		requireStringField(t, key, "value")
		requireNumberField(t, key, "replicaCount")
	}

	cfg := requireObjectField(t, state, "config")
	assertConfigContract(t, cfg)
}

func assertConfigContract(t *testing.T, cfg map[string]any) {
	t.Helper()

	for _, field := range []string{
		"protocol",
		"m",
		"virtualNodes",
		"replicationN",
		"writeQuorum",
		"readQuorum",
		"successorListSize",
		"kBucketSize",
		"alpha",
		"stabilizeIntervalMs",
		"fixFingersIntervalMs",
		"gossipIntervalMs",
		"republishIntervalMs",
		"simDelayMs",
		"simLossRate",
		"initialNodes",
	} {
		if field == "protocol" {
			requireStringField(t, cfg, field)
			continue
		}
		if field == "simLossRate" {
			requireNumberField(t, cfg, field)
			continue
		}
		requireNumberField(t, cfg, field)
	}
}

func assertLookupTraceContract(t *testing.T, trace map[string]any) {
	t.Helper()

	requireStringField(t, trace, "key")
	requireStringField(t, trace, "keyHash")
	requireStringField(t, trace, "targetNode")
	requireNumberField(t, trace, "totalHops")
	requireNumberField(t, trace, "latencyMs")

	hops := requireArrayField(t, trace, "hops")
	if len(hops) == 0 {
		t.Fatal("expected at least one hop in lookup trace")
	}
	for _, raw := range hops {
		hop := requireObjectValue(t, raw, "hops[]")
		requireStringField(t, hop, "fromNode")
		requireStringField(t, hop, "toNode")
		requireStringField(t, hop, "mechanism")
		requireNumberField(t, hop, "hopIndex")
	}
}

func requireObjectField(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()

	raw, ok := parent[key]
	if !ok {
		t.Fatalf("missing field %q", key)
	}
	return requireObjectValue(t, raw, key)
}

func requireArrayField(t *testing.T, parent map[string]any, key string) []any {
	t.Helper()

	raw, ok := parent[key]
	if !ok {
		t.Fatalf("missing field %q", key)
	}
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("field %q has type %T, want array", key, raw)
	}
	return items
}

func requireObjectValue(t *testing.T, raw any, field string) map[string]any {
	t.Helper()

	obj, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("field %q has type %T, want object", field, raw)
	}
	return obj
}

func requireStringField(t *testing.T, parent map[string]any, key string) string {
	t.Helper()

	raw, ok := parent[key]
	if !ok {
		t.Fatalf("missing field %q", key)
	}
	value, ok := raw.(string)
	if !ok {
		t.Fatalf("field %q has type %T, want string", key, raw)
	}
	if value == "" {
		t.Fatalf("field %q is empty", key)
	}
	return value
}

func requireNumberField(t *testing.T, parent map[string]any, key string) float64 {
	t.Helper()

	raw, ok := parent[key]
	if !ok {
		t.Fatalf("missing field %q", key)
	}
	value, ok := raw.(float64)
	if !ok {
		t.Fatalf("field %q has type %T, want number", key, raw)
	}
	return value
}

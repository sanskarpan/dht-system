package gateway

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/simulation"
)

func TestMutationProtectionRequiresAPIKeyWhenConfigured(t *testing.T) {
	t.Setenv("GATEWAY_API_TOKEN", "secret")
	t.Setenv("GATEWAY_MUTATION_LIMIT_PER_MINUTE", "")

	bus := events.NewEventBus()
	orch := simulation.NewOrchestrator(simulation.DefaultConfig(), bus)
	srv := NewServer(orch, bus, 0)
	t.Cleanup(srv.hub.Stop)

	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/network/reset", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("issue unauthenticated request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without API key, got %d", resp.StatusCode)
	}

	req, err = http.NewRequest(http.MethodPost, ts.URL+"/api/v1/network/reset", nil)
	if err != nil {
		t.Fatalf("create authenticated request: %v", err)
	}
	req.Header.Set("X-API-Key", "secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("issue authenticated request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with valid API key, got %d", resp.StatusCode)
	}
}

func TestMutationProtectionRateLimitsProtectedRoutes(t *testing.T) {
	t.Setenv("GATEWAY_API_TOKEN", "")
	t.Setenv("GATEWAY_MUTATION_LIMIT_PER_MINUTE", "1")

	bus := events.NewEventBus()
	orch := simulation.NewOrchestrator(simulation.DefaultConfig(), bus)
	srv := NewServer(orch, bus, 0)
	t.Cleanup(srv.hub.Stop)

	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	for i, want := range []int{http.StatusOK, http.StatusTooManyRequests} {
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/network/reset", nil)
		if err != nil {
			t.Fatalf("create request %d: %v", i, err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("issue request %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("request %d: expected %d, got %d", i, want, resp.StatusCode)
		}
	}
}

func TestRequestIDMiddlewareSetsHeaderAndStructuredLog(t *testing.T) {
	oldWriter := gin.DefaultWriter
	t.Cleanup(func() {
		gin.DefaultWriter = oldWriter
	})

	var logs bytes.Buffer
	gin.DefaultWriter = &logs

	r := gin.New()
	r.Use(requestIDMiddleware())
	r.Use(loggingMiddleware())
	r.GET("/health", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-Request-Id", "req-123")
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	if got := resp.Header().Get("X-Request-Id"); got != "req-123" {
		t.Fatalf("expected request id header to be preserved, got %q", got)
	}

	line := logs.String()
	for _, want := range []string{
		"request_id=req-123",
		"method=GET",
		"path=/health",
		"status=204",
		"latency=",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("log line %q missing %q", line, want)
		}
	}
}

func waitForMetricsText(t *testing.T, baseURL, want string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/metrics")
		if err == nil {
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr == nil && strings.Contains(string(body), want) {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("metric text did not contain %q", want)
}

func TestWebSocketClientMetricTracksConnections(t *testing.T) {
	bus := events.NewEventBus()
	orch := simulation.NewOrchestrator(simulation.DefaultConfig(), bus)
	srv := NewServer(orch, bus, 0)
	t.Cleanup(srv.hub.Stop)

	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	parsed, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	parsed.Scheme = "ws"
	parsed.Path = "/ws"

	conn, _, err := websocket.DefaultDialer.Dial(parsed.String(), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read initial websocket payload: %v", err)
	}

	waitForMetricsText(t, ts.URL, `dht_websocket_clients_connected 1`)

	if err := conn.Close(); err != nil {
		t.Fatalf("close websocket: %v", err)
	}
	waitForMetricsText(t, ts.URL, `dht_websocket_clients_connected 0`)
}

func TestScenarioExecutionMetricsTrackSuccessAndFailure(t *testing.T) {
	bus := events.NewEventBus()
	orch := simulation.NewOrchestrator(simulation.DefaultConfig(), bus)
	srv := NewServer(orch, bus, 0)
	t.Cleanup(srv.hub.Stop)

	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/scenarios/bootstrap/run", strings.NewReader(`{"nodeCount":1}`))
	if err != nil {
		t.Fatalf("create bootstrap request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("issue bootstrap request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected bootstrap request to be accepted, got %d", resp.StatusCode)
	}

	waitForMetricsText(t, ts.URL, `dht_scenario_executions_total{scenario="bootstrap",status="started"} 1`)
	waitForMetricsText(t, ts.URL, `dht_scenario_executions_total{scenario="bootstrap",status="completed"} 1`)

	req, err = http.NewRequest(http.MethodPost, ts.URL+"/api/v1/scenarios/partition/run", nil)
	if err != nil {
		t.Fatalf("create partition request: %v", err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("issue partition request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected partition request to be accepted, got %d", resp.StatusCode)
	}

	waitForMetricsText(t, ts.URL, `dht_scenario_executions_total{scenario="partition",status="started"} 1`)
	waitForMetricsText(t, ts.URL, `dht_scenario_executions_total{scenario="partition",status="error"} 1`)
}

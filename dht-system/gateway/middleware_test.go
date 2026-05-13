package gateway

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
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

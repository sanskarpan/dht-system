package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/simulation"
)

func TestServerReturnsServiceUnavailableWhenFrontendAssetsAreMissing(t *testing.T) {
	bus := events.NewEventBus()
	orch := simulation.NewOrchestrator(testConfig("chord"), bus)
	t.Cleanup(func() {
		bus.Stop()
	})

	server := newServerWithFrontendAssets(orch, bus, 0, filepath.Join(t.TempDir(), "frontend-dist"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET / status=%d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Error string `json:"error"`
		Hint  string `json:"hint"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.Error == "" || body.Hint == "" {
		t.Fatalf("unexpected response body: %+v", body)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/network/state", nil)
	server.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/network/state status=%d body=%s", rec.Code, rec.Body.String())
	}
}

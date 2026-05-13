package gateway

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// DHT-specific Prometheus metrics registered at gateway startup.
var (
	// DHTNodeCount tracks the current number of active DHT nodes.
	DHTNodeCount = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "dht",
		Name:      "node_count",
		Help:      "Current number of active DHT nodes.",
	}, []string{"protocol"})

	// DHTKeyCount tracks the total number of unique keys across all nodes.
	DHTKeyCount = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "dht",
		Name:      "key_count",
		Help:      "Total key-value entries across all nodes.",
	}, []string{"protocol"})

	// DHTLookupHops records the hop count distribution per lookup.
	DHTLookupHops = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "dht",
		Name:      "lookup_hops",
		Help:      "Number of routing hops per key lookup.",
		Buckets:   []float64{1, 2, 3, 4, 5, 6, 7, 8, 10, 12, 16, 20},
	}, []string{"protocol"})

	// DHTLookupDurationMs records lookup latency in milliseconds.
	DHTLookupDurationMs = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "dht",
		Name:      "lookup_duration_ms",
		Help:      "Lookup duration in milliseconds.",
		Buckets:   prometheus.ExponentialBuckets(1, 2, 12),
	}, []string{"protocol"})

	// DHTWriteTotal counts quorum write operations.
	DHTWriteTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "dht",
		Name:      "writes_total",
		Help:      "Total quorum write operations.",
	}, []string{"protocol", "status"})

	// DHTReadTotal counts quorum read operations.
	DHTReadTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "dht",
		Name:      "reads_total",
		Help:      "Total quorum read operations.",
	}, []string{"protocol", "status"})

	// DHTStabilizeCycles counts stabilization cycles (Chord only).
	DHTStabilizeCycles = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "dht",
		Name:      "stabilize_cycles_total",
		Help:      "Number of Chord stabilization cycles completed.",
	}, []string{"node"})

	// DHTGossipSyncs counts anti-entropy gossip reconciliations.
	DHTGossipSyncs = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "dht",
		Name:      "gossip_syncs_total",
		Help:      "Total gossip sync operations.",
	}, []string{"from_node"})

	// DHTWebSocketClientsConnected tracks the current number of connected WebSocket clients.
	DHTWebSocketClientsConnected = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "dht",
		Name:      "websocket_clients_connected",
		Help:      "Current number of connected WebSocket clients.",
	})

	// DHTScenarioExecutionsTotal counts scenario lifecycle transitions.
	DHTScenarioExecutionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "dht",
		Name:      "scenario_executions_total",
		Help:      "Total scenario lifecycle transitions observed by the gateway.",
	}, []string{"scenario", "status"})
)

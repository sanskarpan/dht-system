package gateway

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sanskarpan/dht-system/dht-system/internal/consistent"
	"github.com/sanskarpan/dht-system/dht-system/internal/simulation"
	"go.uber.org/zap"
)

type restHandler struct {
	orch *simulation.Orchestrator
}

func registerRoutes(api *gin.RouterGroup, orch *simulation.Orchestrator) {
	h := &restHandler{orch: orch}

	// Read-only routes remain open for local development and dashboards.
	api.GET("/network/state", h.getNetworkState)
	api.GET("/network/config", h.getConfig)
	api.GET("/network/faults", h.getFaults)
	api.GET("/nodes/:id", h.getNode)
	api.GET("/kv/:key", h.getKey)
	api.GET("/kv/:key/replicas", h.getKeyReplicas)
	api.POST("/lookup", h.traceLookup)
	api.GET("/scenarios", h.listScenarios)
	api.GET("/metrics/json", h.getMetricsJSON)

	// Mutating routes can be protected with API-key auth and/or rate limiting.
	protected := api.Group("/")
	protected.Use(mutationProtectionMiddleware(loadMutationSecurityConfig()))

	// Network
	protected.POST("/network/start", h.startNetwork)
	protected.POST("/network/reset", h.resetNetwork)
	protected.PUT("/network/config", h.updateConfig)
	protected.POST("/network/partition", h.setPartition)
	protected.POST("/network/heal", h.healPartition)
	protected.PUT("/network/links", h.setLinkLatency)
	protected.DELETE("/network/links", h.clearLinkLatencies)

	// Nodes
	protected.POST("/nodes", h.spawnNode)
	protected.DELETE("/nodes/:id", h.killNode)
	protected.POST("/nodes/:id/crash", h.crashNode)

	// KV
	protected.POST("/kv", h.putKey)
	protected.DELETE("/kv/:key", h.deleteKey)

	// Scenarios
	protected.POST("/scenarios/:name/run", h.runScenario)
}

// Network handlers

func (h *restHandler) startNetwork(c *gin.Context) {
	var body struct {
		Protocol  string `json:"protocol"`
		NodeCount int    `json:"nodeCount"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.NodeCount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nodeCount must be greater than 0"})
		return
	}
	nextCfg := h.orch.OrchestratorConfig()
	if body.Protocol != "" {
		var err error
		nextCfg, err = mergeConfig(nextCfg, configPatch{Protocol: &body.Protocol})
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	if h.orch.NodeCount() > 0 {
		h.orch.Reset()
	}
	h.orch.UpdateConfig(nextCfg)
	for i := 0; i < body.NodeCount; i++ {
		if _, err := h.orch.SpawnNode(""); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, mapNetworkState(h.orch.GetNetworkState()))
}

func (h *restHandler) resetNetwork(c *gin.Context) {
	h.orch.Reset()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "network reset"})
}

func (h *restHandler) getNetworkState(c *gin.Context) {
	c.JSON(http.StatusOK, mapNetworkState(h.orch.GetNetworkState()))
}

func (h *restHandler) getConfig(c *gin.Context) {
	c.JSON(http.StatusOK, mapConfig(h.orch.OrchestratorConfig()))
}

func (h *restHandler) updateConfig(c *gin.Context) {
	var patch configPatch
	if err := c.ShouldBindJSON(&patch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	current := h.orch.OrchestratorConfig()
	next, err := mergeConfig(current, patch)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	state := h.orch.GetNetworkState()
	protocolChanged := normalizeProtocol(current.Protocol) != normalizeProtocol(next.Protocol)
	if protocolChanged && len(state.Nodes) > 0 {
		nodeCount := len(state.Nodes)
		h.orch.Reset()
		h.orch.UpdateConfig(next)
		for i := 0; i < nodeCount; i++ {
			if _, err := h.orch.SpawnNode(""); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
	} else {
		h.orch.UpdateConfig(next)
	}

	c.JSON(http.StatusOK, mapConfig(h.orch.OrchestratorConfig()))
}

func (h *restHandler) setPartition(c *gin.Context) {
	var body struct {
		Groups [][]string `json:"groups"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(body.Groups) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least two partition groups are required"})
		return
	}

	resolved := make([][]string, 0, len(body.Groups))
	seen := make(map[string]struct{})
	for _, group := range body.Groups {
		if len(group) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "partition groups must not be empty"})
			return
		}
		var resolvedGroup []string
		for _, member := range group {
			addr := h.resolveAddr(member)
			if addr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unknown node %q", member)})
				return
			}
			if _, ok := seen[addr]; ok {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("node %q appears in multiple groups", member)})
				return
			}
			seen[addr] = struct{}{}
			resolvedGroup = append(resolvedGroup, addr)
		}
		resolved = append(resolved, resolvedGroup)
	}

	h.orch.SetPartition(resolved)
	c.JSON(http.StatusOK, gin.H{"groups": resolved, "success": true})
}

func (h *restHandler) healPartition(c *gin.Context) {
	h.orch.HealPartition()
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *restHandler) getFaults(c *gin.Context) {
	type linkJSON struct {
		From      string `json:"from"`
		To        string `json:"to"`
		LatencyMs int64  `json:"latencyMs"`
	}

	links := h.orch.LinkLatencies()
	items := make([]linkJSON, 0, len(links))
	for _, link := range links {
		items = append(items, linkJSON{
			From:      link.From,
			To:        link.To,
			LatencyMs: link.Delay.Milliseconds(),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].From == items[j].From {
			return items[i].To < items[j].To
		}
		return items[i].From < items[j].From
	})

	c.JSON(http.StatusOK, gin.H{
		"partitions": h.orch.PartitionGroups(),
		"links":      items,
	})
}

func (h *restHandler) setLinkLatency(c *gin.Context) {
	var body struct {
		From      string `json:"from"`
		To        string `json:"to"`
		LatencyMs int64  `json:"latencyMs"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.LatencyMs < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "latencyMs must be non-negative"})
		return
	}

	from := h.resolveAddr(body.From)
	to := h.resolveAddr(body.To)
	if from == "" || to == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from and to must identify existing nodes"})
		return
	}

	h.orch.SetLinkLatency(from, to, time.Duration(body.LatencyMs)*time.Millisecond)
	c.JSON(http.StatusOK, gin.H{
		"from":      from,
		"to":        to,
		"latencyMs": body.LatencyMs,
		"success":   true,
	})
}

func (h *restHandler) clearLinkLatencies(c *gin.Context) {
	h.orch.ClearLinkLatencies()
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// Node handlers

func (h *restHandler) spawnNode(c *gin.Context) {
	var body struct {
		Addr string `json:"addr"`
	}
	if err := c.ShouldBindJSON(&body); err != nil && err.Error() != "EOF" {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.Addr != "" && !isValidAddr(body.Addr) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "addr must be a valid host:port pair"})
		return
	}
	node, err := h.orch.SpawnNode(body.Addr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ref := node.Ref()
	c.JSON(http.StatusCreated, gin.H{
		"id":   consistent.IDToHex(ref.ID),
		"addr": ref.Addr,
	})
}

func (h *restHandler) killNode(c *gin.Context) {
	id := c.Param("id")
	if !isHexNodeID(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid node id: must be a 40-character hex string"})
		return
	}
	addr := h.resolveAddr(id)
	if addr == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}
	if err := h.orch.KillNode(addr); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *restHandler) crashNode(c *gin.Context) {
	id := c.Param("id")
	if !isHexNodeID(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid node id: must be a 40-character hex string"})
		return
	}
	addr := h.resolveAddr(id)
	if addr == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}
	if err := h.orch.CrashNode(addr); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *restHandler) getNode(c *gin.Context) {
	id := c.Param("id")
	if !isHexNodeID(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid node id: must be a 40-character hex string"})
		return
	}
	state := h.orch.GetNetworkState()
	for _, n := range state.Nodes {
		if n.ID == id || n.Addr == id {
			c.JSON(http.StatusOK, n)
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
}

// KV handlers

func (h *restHandler) putKey(c *gin.Context) {
	var body struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	proto := h.orch.Protocol()
	if err := h.orch.Insert(body.Key, body.Value); err != nil {
		DHTWriteTotal.WithLabelValues(proto, "error").Inc()
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	DHTWriteTotal.WithLabelValues(proto, "ok").Inc()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"keyHash": consistent.IDToHex(consistent.KeyID(body.Key)),
	})
}

func (h *restHandler) getKey(c *gin.Context) {
	key := c.Param("key")
	keyID := consistent.KeyID(key)
	proto := h.orch.Protocol()

	replicaEntries := h.orch.GetKeyReplicas(key)
	if len(replicaEntries) == 0 {
		DHTReadTotal.WithLabelValues(proto, "not_found").Inc()
		c.JSON(http.StatusNotFound, gin.H{"error": "key not found"})
		return
	}

	value, err := h.orch.Read(key)
	if err != nil {
		DHTReadTotal.WithLabelValues(proto, "error").Inc()
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	replicas := make([]string, 0, len(replicaEntries))
	for _, replica := range replicaEntries {
		replicas = append(replicas, replica.NodeID)
	}
	sort.Strings(replicas)

	DHTReadTotal.WithLabelValues(proto, "ok").Inc()
	c.JSON(http.StatusOK, simulation.KeyState{
		ID:           consistent.IDToHex(keyID),
		Key:          key,
		Value:        string(value),
		Replicas:     replicas,
		ReplicaCount: len(replicas),
	})
}

func (h *restHandler) deleteKey(c *gin.Context) {
	key := c.Param("key")
	if err := h.orch.DeleteKey(key); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *restHandler) getKeyReplicas(c *gin.Context) {
	key := c.Param("key")
	replicaEntries := h.orch.GetKeyReplicas(key)
	if len(replicaEntries) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "key not found"})
		return
	}

	type entryJSON struct {
		Key       string            `json:"key"`
		Value     string            `json:"value"`
		Clock     map[string]uint64 `json:"clock"`
		Timestamp string            `json:"timestamp"`
		TTLNs     int64             `json:"ttlNs"`
		NodeID    string            `json:"nodeId"`
	}
	type replicaItem struct {
		NodeID string    `json:"nodeId"`
		Entry  entryJSON `json:"entry"`
	}

	items := make([]replicaItem, 0, len(replicaEntries))
	for _, r := range replicaEntries {
		clock := r.Entry.Clock
		if clock == nil {
			clock = map[string]uint64{}
		}
		items = append(items, replicaItem{
			NodeID: r.NodeID,
			Entry: entryJSON{
				Key:       consistent.IDToHex(r.Entry.Key),
				Value:     base64.StdEncoding.EncodeToString(r.Entry.Value),
				Clock:     clock,
				Timestamp: r.Entry.Timestamp.Format(time.RFC3339Nano),
				TTLNs:     r.Entry.TTL.Nanoseconds(),
				NodeID:    r.Entry.NodeID,
			},
		})
	}
	c.JSON(http.StatusOK, gin.H{"replicas": items})
}

// Lookup handlers

func (h *restHandler) traceLookup(c *gin.Context) {
	var body struct {
		Key string `json:"key"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	proto := h.orch.Protocol()
	trace, err := h.orch.Lookup(body.Key)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	DHTLookupHops.WithLabelValues(proto).Observe(float64(trace.TotalHops))
	DHTLookupDurationMs.WithLabelValues(proto).Observe(float64(trace.LatencyMs))
	c.JSON(http.StatusOK, mapLookupTrace(trace))
}

// Scenario handlers

func (h *restHandler) listScenarios(c *gin.Context) {
	scenarios := []gin.H{
		{"name": "bootstrap", "description": "Spawn nodes one by one and watch ring form"},
		{"name": "churn", "description": "Add/remove nodes continuously for 30s"},
		{"name": "partition", "description": "Split network and heal it"},
		{"name": "hotkey", "description": "Write 1000 keys and observe load distribution"},
		{"name": "benchmark", "description": "Measure lookup hop count as network scales from 10 to 50 nodes"},
	}
	c.JSON(http.StatusOK, gin.H{"scenarios": scenarios})
}

func (h *restHandler) runScenario(c *gin.Context) {
	name := c.Param("name")
	// Scenarios run in background goroutines; use a detached context so the
	// scenario is not canceled when the HTTP response is sent.
	bgCtx := context.Background()

	// Optional parameters from request body
	var body struct {
		NodeCount int     `json:"nodeCount"`
		DurationS float64 `json:"durationSeconds"`
		KeyCount  int     `json:"keyCount"`
	}
	if err := c.ShouldBindJSON(&body); err != nil && err.Error() != "EOF" {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var runErr error
	switch name {
	case "bootstrap":
		nodeCount := body.NodeCount
		if nodeCount <= 0 {
			nodeCount = 10
		}
		go func() {
			if err := h.orch.ScenarioBootstrap(bgCtx, nodeCount); err != nil {
				zap.L().Warn("scenario bootstrap error", zap.Error(err))
			}
		}()
	case "churn":
		nodeCount := body.NodeCount
		if nodeCount <= 0 {
			nodeCount = 6
		}
		dur := time.Duration(body.DurationS * float64(time.Second))
		if dur <= 0 {
			dur = 30 * time.Second
		}
		go func() {
			if err := h.orch.ScenarioChurn(bgCtx, dur, nodeCount); err != nil {
				zap.L().Warn("scenario churn error", zap.Error(err))
			}
		}()
	case "partition":
		keys := []string{"p-key-0", "p-key-1", "p-key-2", "p-key-3"}
		go func() {
			if err := h.orch.ScenarioPartition(bgCtx, keys, 2*time.Second); err != nil {
				zap.L().Warn("scenario partition error", zap.Error(err))
			}
		}()
	case "hotkey":
		keyCount := body.KeyCount
		if keyCount <= 0 {
			keyCount = 1000
		}
		go func() {
			if err := h.orch.ScenarioHotKey(bgCtx, keyCount); err != nil {
				zap.L().Warn("scenario hotkey error", zap.Error(err))
			}
		}()
	case "benchmark":
		go func() {
			if err := h.orch.ScenarioBenchmark(bgCtx, 10, 50, 20); err != nil {
				zap.L().Warn("scenario benchmark error", zap.Error(err))
			}
		}()
	default:
		runErr = fmt.Errorf("unknown scenario %q", name)
	}

	if runErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": runErr.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"started": true, "scenario": name,
		"message": "scenario running in background; subscribe to scenario_step events on /ws"})
}

func (h *restHandler) getMetricsJSON(c *gin.Context) {
	state := h.orch.GetNetworkState()
	c.JSON(http.StatusOK, gin.H{
		"nodeCount": len(state.Nodes),
		"keyCount":  len(state.Keys),
		"protocol":  state.Protocol,
	})
}

// resolveAddr finds a node's address from a hex ID or addr string.
func (h *restHandler) resolveAddr(idOrAddr string) string {
	state := h.orch.GetNetworkState()
	for _, n := range state.Nodes {
		if n.ID == idOrAddr || n.Addr == idOrAddr {
			return n.Addr
		}
	}
	return ""
}

// isValidAddr returns true when s is a valid host:port pair with a numeric port.
func isValidAddr(s string) bool {
	host, port, err := net.SplitHostPort(s)
	if err != nil || host == "" || port == "" {
		return false
	}
	portNum, err := strconv.Atoi(port)
	return err == nil && portNum > 0 && portNum <= 65535
}

// isHexNodeID returns true when s is a 40-character SHA-1 hex node ID.
func isHexNodeID(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

package gateway

import (
	"fmt"
	"strings"
	"time"

	"github.com/sanskarpan/dht-system/dht-system/internal/simulation"
)

type configResponse struct {
	Protocol             string  `json:"protocol"`
	M                    int     `json:"m"`
	VirtualNodes         int     `json:"virtualNodes"`
	ReplicationN         int     `json:"replicationN"`
	WriteQuorum          int     `json:"writeQuorum"`
	ReadQuorum           int     `json:"readQuorum"`
	SuccessorListSize    int     `json:"successorListSize"`
	KBucketSize          int     `json:"kBucketSize"`
	Alpha                int     `json:"alpha"`
	StabilizeIntervalMs  int64   `json:"stabilizeIntervalMs"`
	FixFingersIntervalMs int64   `json:"fixFingersIntervalMs"`
	GossipIntervalMs     int64   `json:"gossipIntervalMs"`
	RepublishIntervalMs  int64   `json:"republishIntervalMs"`
	SimDelayMs           int64   `json:"simDelayMs"`
	SimLossRate          float64 `json:"simLossRate"`
	InitialNodes         int     `json:"initialNodes"`
}

type networkStateResponse struct {
	Protocol string                 `json:"protocol"`
	Nodes    []simulation.NodeState `json:"nodes"`
	Keys     []simulation.KeyState  `json:"keys"`
	Config   configResponse         `json:"config"`
}

type hopEventResponse struct {
	FromNode  string `json:"fromNode"`
	ToNode    string `json:"toNode"`
	Mechanism string `json:"mechanism"`
	HopIndex  int    `json:"hopIndex"`
}

type lookupTraceResponse struct {
	Key        string             `json:"key"`
	KeyHash    string             `json:"keyHash"`
	TargetNode string             `json:"targetNode"`
	Hops       []hopEventResponse `json:"hops"`
	TotalHops  int                `json:"totalHops"`
	LatencyMs  int64              `json:"latencyMs"`
}

type configPatch struct {
	Protocol             *string  `json:"protocol"`
	M                    *int     `json:"m"`
	VirtualNodes         *int     `json:"virtualNodes"`
	ReplicationN         *int     `json:"replicationN"`
	WriteQuorum          *int     `json:"writeQuorum"`
	ReadQuorum           *int     `json:"readQuorum"`
	SuccessorListSize    *int     `json:"successorListSize"`
	KBucketSize          *int     `json:"kBucketSize"`
	Alpha                *int     `json:"alpha"`
	StabilizeIntervalMs  *int64   `json:"stabilizeIntervalMs"`
	FixFingersIntervalMs *int64   `json:"fixFingersIntervalMs"`
	GossipIntervalMs     *int64   `json:"gossipIntervalMs"`
	RepublishIntervalMs  *int64   `json:"republishIntervalMs"`
	SimDelayMs           *int64   `json:"simDelayMs"`
	SimLossRate          *float64 `json:"simLossRate"`
	InitialNodes         *int     `json:"initialNodes"`
}

func normalizeProtocol(protocol string) string {
	return strings.ToLower(strings.TrimSpace(protocol))
}

func mapConfig(cfg *simulation.Config) configResponse {
	if cfg == nil {
		return configResponse{}
	}
	return configResponse{
		Protocol:             cfg.Protocol,
		M:                    cfg.M,
		VirtualNodes:         cfg.VirtualNodes,
		ReplicationN:         cfg.ReplicationN,
		WriteQuorum:          cfg.WriteQuorum,
		ReadQuorum:           cfg.ReadQuorum,
		SuccessorListSize:    cfg.SuccessorListSize,
		KBucketSize:          cfg.KBucketSize,
		Alpha:                cfg.Alpha,
		StabilizeIntervalMs:  cfg.StabilizeInterval.Milliseconds(),
		FixFingersIntervalMs: cfg.FixFingersInterval.Milliseconds(),
		GossipIntervalMs:     cfg.GossipInterval.Milliseconds(),
		RepublishIntervalMs:  cfg.RepublishInterval.Milliseconds(),
		SimDelayMs:           cfg.SimDelay.Milliseconds(),
		SimLossRate:          cfg.SimLossRate,
		InitialNodes:         cfg.InitialNodes,
	}
}

func mapNetworkState(state *simulation.NetworkState) networkStateResponse {
	if state == nil {
		return networkStateResponse{}
	}
	return networkStateResponse{
		Protocol: state.Protocol,
		Nodes:    state.Nodes,
		Keys:     state.Keys,
		Config:   mapConfig(state.Config),
	}
}

func mapLookupTrace(trace *simulation.LookupTrace) lookupTraceResponse {
	hops := make([]hopEventResponse, 0, len(trace.Hops))
	for _, hop := range trace.Hops {
		hops = append(hops, hopEventResponse{
			FromNode:  hop.FromNode,
			ToNode:    hop.ToNode,
			Mechanism: hop.Mechanism,
			HopIndex:  hop.HopIndex,
		})
	}
	return lookupTraceResponse{
		Key:        trace.Key,
		KeyHash:    trace.KeyHash,
		TargetNode: trace.TargetNode,
		Hops:       hops,
		TotalHops:  trace.TotalHops,
		LatencyMs:  trace.LatencyMs,
	}
}

func mergeConfig(base *simulation.Config, patch configPatch) (*simulation.Config, error) {
	if base == nil {
		base = simulation.DefaultConfig()
	}
	next := *base

	if patch.Protocol != nil {
		next.Protocol = normalizeProtocol(*patch.Protocol)
	}
	if patch.M != nil {
		next.M = *patch.M
	}
	if patch.VirtualNodes != nil {
		next.VirtualNodes = *patch.VirtualNodes
	}
	if patch.ReplicationN != nil {
		next.ReplicationN = *patch.ReplicationN
	}
	if patch.WriteQuorum != nil {
		next.WriteQuorum = *patch.WriteQuorum
	}
	if patch.ReadQuorum != nil {
		next.ReadQuorum = *patch.ReadQuorum
	}
	if patch.SuccessorListSize != nil {
		next.SuccessorListSize = *patch.SuccessorListSize
	}
	if patch.KBucketSize != nil {
		next.KBucketSize = *patch.KBucketSize
	}
	if patch.Alpha != nil {
		next.Alpha = *patch.Alpha
	}
	if patch.StabilizeIntervalMs != nil {
		next.StabilizeInterval = time.Duration(*patch.StabilizeIntervalMs) * time.Millisecond
	}
	if patch.FixFingersIntervalMs != nil {
		next.FixFingersInterval = time.Duration(*patch.FixFingersIntervalMs) * time.Millisecond
	}
	if patch.GossipIntervalMs != nil {
		next.GossipInterval = time.Duration(*patch.GossipIntervalMs) * time.Millisecond
	}
	if patch.RepublishIntervalMs != nil {
		next.RepublishInterval = time.Duration(*patch.RepublishIntervalMs) * time.Millisecond
	}
	if patch.SimDelayMs != nil {
		next.SimDelay = time.Duration(*patch.SimDelayMs) * time.Millisecond
	}
	if patch.SimLossRate != nil {
		next.SimLossRate = *patch.SimLossRate
	}
	if patch.InitialNodes != nil {
		next.InitialNodes = *patch.InitialNodes
	}

	switch next.Protocol {
	case "", "chord", "kademlia":
	default:
		return nil, fmt.Errorf("protocol must be one of: chord, kademlia")
	}
	if next.M <= 0 {
		return nil, fmt.Errorf("m must be greater than 0")
	}
	if next.VirtualNodes <= 0 {
		return nil, fmt.Errorf("virtualNodes must be greater than 0")
	}
	if next.ReplicationN <= 0 {
		return nil, fmt.Errorf("replicationN must be greater than 0")
	}
	if next.WriteQuorum <= 0 || next.WriteQuorum > next.ReplicationN {
		return nil, fmt.Errorf("writeQuorum must be between 1 and replicationN")
	}
	if next.ReadQuorum <= 0 || next.ReadQuorum > next.ReplicationN {
		return nil, fmt.Errorf("readQuorum must be between 1 and replicationN")
	}
	if next.SuccessorListSize <= 0 {
		return nil, fmt.Errorf("successorListSize must be greater than 0")
	}
	if next.KBucketSize <= 0 {
		return nil, fmt.Errorf("kBucketSize must be greater than 0")
	}
	if next.Alpha <= 0 {
		return nil, fmt.Errorf("alpha must be greater than 0")
	}
	if next.StabilizeInterval < 0 || next.FixFingersInterval < 0 || next.GossipInterval < 0 || next.RepublishInterval < 0 || next.SimDelay < 0 {
		return nil, fmt.Errorf("interval values must be non-negative")
	}
	if next.SimLossRate < 0 || next.SimLossRate > 1 {
		return nil, fmt.Errorf("simLossRate must be between 0 and 1")
	}
	if next.InitialNodes <= 0 {
		return nil, fmt.Errorf("initialNodes must be greater than 0")
	}

	return &next, nil
}

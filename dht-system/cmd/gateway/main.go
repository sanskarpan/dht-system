package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"go.uber.org/zap"

	"github.com/sanskarpan/dht-system/dht-system/gateway"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/simulation"
)

func main() {
	logger, _ := zap.NewProduction()
	defer logger.Sync() //nolint:errcheck
	zap.ReplaceGlobals(logger)

	port := 8080
	if p := os.Getenv("PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		} else {
			logger.Warn("invalid PORT env var, using default", zap.String("value", p), zap.Int("default", port))
		}
	}

	initialNodes := 6
	if n := os.Getenv("INITIAL_NODES"); n != "" {
		if v, err := strconv.Atoi(n); err == nil {
			initialNodes = v
		}
	}

	protocol := "chord"
	if p := os.Getenv("PROTOCOL"); p != "" {
		protocol = p
	}

	bus := events.NewEventBus()
	cfg := simulation.DefaultConfig()
	cfg.Protocol = protocol
	cfg.InitialNodes = initialNodes

	orch := simulation.NewOrchestrator(cfg, bus)

	// Spawn initial nodes
	for i := 0; i < initialNodes; i++ {
		if _, err := orch.SpawnNode(""); err != nil {
			logger.Warn("failed to spawn initial node", zap.Int("index", i), zap.Error(err))
		}
	}

	logger.Info("DHT System started", zap.String("protocol", protocol), zap.Int("nodes", orch.NodeCount()))

	srv := gateway.NewServer(orch, bus, port)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := srv.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

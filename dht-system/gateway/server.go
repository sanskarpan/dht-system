package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/simulation"
	"go.uber.org/zap"
)

// Server holds all gateway dependencies.
type Server struct {
	orch   *simulation.Orchestrator
	hub    *Hub
	router *gin.Engine
	port   int
}

const defaultFrontendDistRoot = "./frontend/dist"

type frontendAssets struct {
	root    string
	ready   bool
	missing []string
}

// NewServer creates and configures the HTTP/WS gateway server.
func NewServer(orch *simulation.Orchestrator, bus *events.EventBus, port int) *Server {
	return newServerWithFrontendAssets(orch, bus, port, defaultFrontendDistRoot)
}

func newServerWithFrontendAssets(orch *simulation.Orchestrator, bus *events.EventBus, port int, frontendRoot string) *Server {
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.Use(gin.Recovery())
	r.Use(corsMiddleware())
	r.Use(requestBodyLimitMiddleware(1 << 20))
	r.Use(requestIDMiddleware())
	r.Use(loggingMiddleware())
	r.NoMethod(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") || c.Request.URL.Path == "/api" {
			c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "method not allowed"})
			return
		}
		c.Status(http.StatusMethodNotAllowed)
	})

	hub := NewHub(bus, orch)
	go hub.Run()

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// API routes
	api := r.Group("/api/v1")
	registerRoutes(api, orch)

	// Prometheus metrics (standard text format)
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// WebSocket
	r.GET("/ws", hub.HandleUpgrade)

	// Serve frontend static files
	assets := detectFrontendAssets(frontendRoot)
	if !assets.ready {
		zap.L().Warn("frontend assets missing; gateway will serve API-only mode",
			zap.String("root", assets.root),
			zap.Strings("missing", assets.missing),
		)
	}

	unavailable := func(c *gin.Context) {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "frontend assets are not built",
			"hint":  "run `cd frontend && npm run build` before starting the gateway",
		})
	}

	if assets.ready {
		r.Static("/assets", filepath.Join(assets.root, "assets"))
		r.StaticFile("/favicon.ico", filepath.Join(assets.root, "favicon.ico"))
	} else {
		r.GET("/assets/*filepath", unavailable)
		r.HEAD("/assets/*filepath", unavailable)
		r.GET("/favicon.ico", unavailable)
		r.HEAD("/favicon.ico", unavailable)
	}
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") || c.Request.URL.Path == "/api" {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if !assets.ready {
			unavailable(c)
			return
		}
		c.File(filepath.Join(assets.root, "index.html"))
	})

	return &Server{orch: orch, hub: hub, router: r, port: port}
}

func detectFrontendAssets(root string) frontendAssets {
	missing := make([]string, 0, 3)
	required := []string{
		"index.html",
		"assets",
		"favicon.ico",
	}
	for _, name := range required {
		path := filepath.Join(root, name)
		if _, err := os.Stat(path); err != nil {
			missing = append(missing, path)
		}
	}
	return frontendAssets{
		root:    root,
		ready:   len(missing) == 0,
		missing: missing,
	}
}

// Run starts the HTTP server and shuts it down when ctx is canceled.
func (s *Server) Run(ctx context.Context) error {
	zap.L().Info("DHT Gateway listening", zap.Int("port", s.port))
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", s.port),
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		s.hub.Stop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

// Router returns the gin engine (for testing).
func (s *Server) Router() *gin.Engine {
	return s.router
}

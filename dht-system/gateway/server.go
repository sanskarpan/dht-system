package gateway

import (
	"fmt"
	"net/http"
	"strings"

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

// NewServer creates and configures the HTTP/WS gateway server.
func NewServer(orch *simulation.Orchestrator, bus *events.EventBus, port int) *Server {
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.Use(gin.Recovery())
	r.Use(corsMiddleware())
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
	r.Static("/assets", "./frontend/dist/assets")
	r.StaticFile("/favicon.ico", "./frontend/dist/favicon.ico")
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") || c.Request.URL.Path == "/api" {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.File("./frontend/dist/index.html")
	})

	return &Server{orch: orch, hub: hub, router: r, port: port}
}

// Run starts the HTTP server (blocking).
func (s *Server) Run() error {
	zap.L().Info("DHT Gateway listening", zap.Int("port", s.port))
	return s.router.Run(fmt.Sprintf(":%d", s.port))
}

// Router returns the gin engine (for testing).
func (s *Server) Router() *gin.Engine {
	return s.router
}

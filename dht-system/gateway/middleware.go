package gateway

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// NOTE: CORS wildcard is intentionally permissive for local development.
// In production, replace "*" with an explicit origin allowlist controlled
// by a CORS_ORIGINS environment variable.
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type,Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}

func loggingMiddleware() gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return param.Method + " " + param.Path + " " +
			param.ClientIP + " " + param.Latency.String() +
			" " + time.Now().Format(time.RFC3339) + "\n"
	})
}

// requestBodyLimitMiddleware rejects oversized request bodies before handlers
// attempt to parse them. It also wraps the body reader so chunked requests are
// capped even when Content-Length is absent.
func requestBodyLimitMiddleware(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		if c.Request.ContentLength > maxBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		c.Next()
	}
}

type mutationSecurityConfig struct {
	apiToken    string
	ratePerMin  int
	rateEnabled bool
}

func loadMutationSecurityConfig() mutationSecurityConfig {
	cfg := mutationSecurityConfig{
		apiToken: strings.TrimSpace(os.Getenv("GATEWAY_API_TOKEN")),
	}

	if raw := strings.TrimSpace(os.Getenv("GATEWAY_MUTATION_LIMIT_PER_MINUTE")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			cfg.ratePerMin = n
			cfg.rateEnabled = true
		}
	}

	return cfg
}

type mutationLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	buckets map[string]*mutationBucket
}

type mutationBucket struct {
	windowStart time.Time
	count       int
}

func newMutationLimiter(limit int, window time.Duration) *mutationLimiter {
	return &mutationLimiter{
		limit:   limit,
		window:  window,
		buckets: make(map[string]*mutationBucket),
	}
}

func (l *mutationLimiter) allow(key string, now time.Time) bool {
	if l == nil || l.limit <= 0 {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	bucket := l.buckets[key]
	if bucket == nil || now.Sub(bucket.windowStart) >= l.window {
		l.buckets[key] = &mutationBucket{windowStart: now, count: 1}
		return true
	}

	if bucket.count >= l.limit {
		return false
	}

	bucket.count++
	return true
}

func mutationProtectionMiddleware(cfg mutationSecurityConfig) gin.HandlerFunc {
	limiter := newMutationLimiter(cfg.ratePerMin, time.Minute)

	return func(c *gin.Context) {
		if cfg.apiToken != "" {
			token := strings.TrimSpace(c.GetHeader("X-API-Key"))
			if token == "" {
				authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
				if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
					token = strings.TrimSpace(authHeader[len("Bearer "):])
				}
			}
			if token != cfg.apiToken {
				c.Header("WWW-Authenticate", `Bearer realm="dht-system"`)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
				return
			}
		}

		if cfg.rateEnabled && !limiter.allow(c.ClientIP(), time.Now()) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "mutation rate limit exceeded"})
			return
		}

		c.Next()
	}
}

package gateway

import (
	"net/http"
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

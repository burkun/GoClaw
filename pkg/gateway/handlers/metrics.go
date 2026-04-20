// Package handlers provides Prometheus metrics collection and exposition.
package handlers

import (
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"goclaw/pkg/metrics"
)

// MetricsHandler handles metrics-related endpoints.
type MetricsHandler struct{}

// NewMetricsHandler creates a new MetricsHandler.
func NewMetricsHandler() *MetricsHandler {
	return &MetricsHandler{}
}

// PrometheusHandler returns the Prometheus HTTP handler for /metrics endpoint.
func (h *MetricsHandler) PrometheusHandler() gin.HandlerFunc {
	return gin.WrapH(promhttp.Handler())
}

// HealthHandler handles health check endpoints.
type HealthHandler struct {
	startTime time.Time
}

// NewHealthHandler creates a new HealthHandler.
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{
		startTime: time.Now(),
	}
}

// HealthResponse represents the health check response.
type HealthResponse struct {
	Status    string            `json:"status"`
	Service   string            `json:"service"`
	Version   string            `json:"version,omitempty"`
	Uptime    string            `json:"uptime"`
	Timestamp time.Time         `json:"timestamp"`
	Checks    map[string]string `json:"checks,omitempty"`
}

// Health returns the health status of the service.
func (h *HealthHandler) Health(c *gin.Context) {
	response := HealthResponse{
		Status:    "healthy",
		Service:   "goclaw-gateway",
		Uptime:    time.Since(h.startTime).String(),
		Timestamp: time.Now(),
		Checks: map[string]string{
			"gateway": "ok",
		},
	}
	c.JSON(http.StatusOK, response)
}

// Ready returns the readiness status of the service.
func (h *HealthHandler) Ready(c *gin.Context) {
	// Check if the service is ready to accept traffic.
	// This can include checking database connections, external services, etc.
	ready := true
	checks := make(map[string]string)

	// Add readiness checks here as needed.
	// Example: check database connection, cache availability, etc.
	checks["gateway"] = "ok"

	if ready {
		c.JSON(http.StatusOK, HealthResponse{
			Status:    "ready",
			Service:   "goclaw-gateway",
			Uptime:    time.Since(h.startTime).String(),
			Timestamp: time.Now(),
			Checks:    checks,
		})
	} else {
		c.JSON(http.StatusServiceUnavailable, HealthResponse{
			Status:    "not_ready",
			Service:   "goclaw-gateway",
			Uptime:    time.Since(h.startTime).String(),
			Timestamp: time.Now(),
			Checks:    checks,
		})
	}
}

// Live returns the liveness status of the service.
func (h *HealthHandler) Live(c *gin.Context) {
	// Check if the service is alive.
	// This should be a lightweight check that confirms the process is running.
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	c.JSON(http.StatusOK, gin.H{
		"status":  "alive",
		"service": "goclaw-gateway",
		"memory": gin.H{
			"alloc":       m.Alloc,
			"total_alloc": m.TotalAlloc,
			"sys":         m.Sys,
			"num_gc":      m.NumGC,
		},
	})
}

// PrometheusMiddleware creates a Gin middleware for collecting HTTP metrics.
func PrometheusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		// Process request.
		c.Next()

		// Record metrics after request is processed.
		duration := time.Since(start)
		statusCode := c.Writer.Status()

		metrics.RecordHTTPRequest(c.Request.Method, path, duration, statusCode)
	}
}

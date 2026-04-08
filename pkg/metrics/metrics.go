// Package metrics provides Prometheus metrics collection for GoClaw.
// This package is independent to avoid import cycles.
package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// AgentRunTotal counts the total number of agent runs.
	AgentRunTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agent_run_total",
			Help: "Total number of agent runs",
		},
		[]string{"agent_name", "status"},
	)

	// AgentRunDurationSeconds tracks the duration of agent runs.
	AgentRunDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "agent_run_duration_seconds",
			Help:    "Duration of agent runs in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"agent_name", "status"},
	)

	// ToolExecutionTotal counts the total number of tool executions.
	ToolExecutionTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tool_execution_total",
			Help: "Total number of tool executions",
		},
		[]string{"tool_name", "status"},
	)

	// ToolExecutionDurationSeconds tracks the duration of tool executions.
	ToolExecutionDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "tool_execution_duration_seconds",
			Help:    "Duration of tool executions in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"tool_name", "status"},
	)

	// ActiveThreads tracks the number of active threads.
	ActiveThreads = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "active_threads",
			Help: "Number of active threads",
		},
	)

	// QueueSize tracks the current queue size.
	QueueSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "queue_size",
			Help: "Current queue size",
		},
	)

	// ActiveRuns tracks the number of active runs.
	ActiveRuns = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "active_runs",
			Help: "Number of active runs",
		},
	)

	// HTTPRequestDurationSeconds tracks HTTP request durations.
	HTTPRequestDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Duration of HTTP requests in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path", "status"},
	)

	// HTTPRequestTotal counts total HTTP requests.
	HTTPRequestTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_request_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)
)

func init() {
	// Register all metrics with the default registry.
	prometheus.MustRegister(AgentRunTotal)
	prometheus.MustRegister(AgentRunDurationSeconds)
	prometheus.MustRegister(ToolExecutionTotal)
	prometheus.MustRegister(ToolExecutionDurationSeconds)
	prometheus.MustRegister(ActiveThreads)
	prometheus.MustRegister(QueueSize)
	prometheus.MustRegister(ActiveRuns)
	prometheus.MustRegister(HTTPRequestDurationSeconds)
	prometheus.MustRegister(HTTPRequestTotal)
}

// RecordAgentRun records an agent run with its duration and status.
func RecordAgentRun(agentName string, duration time.Duration, status string) {
	AgentRunTotal.WithLabelValues(agentName, status).Inc()
	AgentRunDurationSeconds.WithLabelValues(agentName, status).Observe(duration.Seconds())
}

// RecordToolExecution records a tool execution with its duration and status.
func RecordToolExecution(toolName string, duration time.Duration, status string) {
	ToolExecutionTotal.WithLabelValues(toolName, status).Inc()
	ToolExecutionDurationSeconds.WithLabelValues(toolName, status).Observe(duration.Seconds())
}

// SetActiveThreads sets the current number of active threads.
func SetActiveThreads(count float64) {
	ActiveThreads.Set(count)
}

// SetQueueSize sets the current queue size.
func SetQueueSize(size float64) {
	QueueSize.Set(size)
}

// SetActiveRuns sets the current number of active runs.
func SetActiveRuns(count float64) {
	ActiveRuns.Set(count)
}

// RecordHTTPRequest records an HTTP request with its duration and status.
func RecordHTTPRequest(method, path string, duration time.Duration, statusCode int) {
	status := http.StatusText(statusCode)
	if status == "" {
		status = "unknown"
	}
	HTTPRequestDurationSeconds.WithLabelValues(method, path, status).Observe(duration.Seconds())
	HTTPRequestTotal.WithLabelValues(method, path, status).Inc()
}

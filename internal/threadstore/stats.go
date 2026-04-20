package threadstore

import (
	"encoding/json"
	"sync"
	"time"
)

// QueryStats tracks query performance statistics.
type QueryStats struct {
	mu sync.RWMutex

	// Query counts
	TotalQueries      int64
	QueriesByType     map[string]int64
	QueriesByEndpoint map[string]int64

	// Performance metrics
	AverageLatencyMs     float64
	MaxLatencyMs         float64
	SlowQueryCount       int64
	SlowQueryThresholdMs float64

	// Index metrics
	IndexHitRate float64
	IndexHits    int64
	IndexMisses  int64

	// Time-series data (last hour)
	RecentQueries    []QueryRecord
	MaxRecentQueries int

	// Start time
	StartTime int64
}

// QueryRecord represents a single query execution record.
type QueryRecord struct {
	Timestamp   int64  `json:"timestamp"`
	QueryType   string `json:"query_type"`
	DurationMs  int64  `json:"duration_ms"`
	ThreadCount int    `json:"thread_count"`
	Status      string `json:"status"` // "success", "error", "slow"
	UsedIndex   bool   `json:"used_index"`
}

// NewQueryStats creates a new QueryStats instance.
func NewQueryStats() *QueryStats {
	return &QueryStats{
		QueriesByType:        make(map[string]int64),
		QueriesByEndpoint:    make(map[string]int64),
		SlowQueryThresholdMs: 100.0, // 100ms considered slow
		RecentQueries:        make([]QueryRecord, 0),
		MaxRecentQueries:     1000, // Keep last 1000 queries
		StartTime:            time.Now().UnixMilli(),
	}
}

// RecordQuery records a query execution.
func (qs *QueryStats) RecordQuery(queryType string, durationMs int64, threadCount int, usedIndex bool, hasError bool) {
	qs.mu.Lock()
	defer qs.mu.Unlock()

	// Update counts
	qs.TotalQueries++
	qs.QueriesByType[queryType]++

	// Calculate status
	status := "success"
	if hasError {
		status = "error"
	} else if float64(durationMs) > qs.SlowQueryThresholdMs {
		status = "slow"
		qs.SlowQueryCount++
	}

	// Update latency metrics
	qs.updateLatencyMetrics(float64(durationMs))

	// Update index metrics
	if usedIndex {
		qs.IndexHits++
	} else {
		qs.IndexMisses++
	}
	qs.IndexHitRate = float64(qs.IndexHits) / float64(qs.IndexHits+qs.IndexMisses) * 100

	// Add to recent queries
	record := QueryRecord{
		Timestamp:   time.Now().UnixMilli(),
		QueryType:   queryType,
		DurationMs:  durationMs,
		ThreadCount: threadCount,
		Status:      status,
		UsedIndex:   usedIndex,
	}
	qs.RecentQueries = append(qs.RecentQueries, record)

	// Trim old records
	if len(qs.RecentQueries) > qs.MaxRecentQueries {
		qs.RecentQueries = qs.RecentQueries[len(qs.RecentQueries)-qs.MaxRecentQueries:]
	}
}

// updateLatencyMetrics updates average and max latency (must be called with lock held).
func (qs *QueryStats) updateLatencyMetrics(durationMs float64) {
	// Update max
	if durationMs > qs.MaxLatencyMs {
		qs.MaxLatencyMs = durationMs
	}

	// Update average (incremental mean)
	if qs.TotalQueries == 1 {
		qs.AverageLatencyMs = durationMs
	} else {
		qs.AverageLatencyMs = qs.AverageLatencyMs + (durationMs-qs.AverageLatencyMs)/float64(qs.TotalQueries)
	}
}

// GetStats returns current statistics.
func (qs *QueryStats) GetStats() map[string]any {
	qs.mu.RLock()
	defer qs.mu.RUnlock()

	return map[string]any{
		"total_queries":      qs.TotalQueries,
		"queries_by_type":    qs.cloneMap(qs.QueriesByType),
		"average_latency_ms": qs.AverageLatencyMs,
		"max_latency_ms":     qs.MaxLatencyMs,
		"slow_query_count":   qs.SlowQueryCount,
		"index_hit_rate":     qs.IndexHitRate,
		"index_hits":         qs.IndexHits,
		"index_misses":       qs.IndexMisses,
		"uptime_seconds":     (time.Now().UnixMilli() - qs.StartTime) / 1000,
		"recent_query_count": len(qs.RecentQueries),
	}
}

// GetSlowQueries returns queries that exceeded the slow threshold.
func (qs *QueryStats) GetSlowQueries() []QueryRecord {
	qs.mu.RLock()
	defer qs.mu.RUnlock()

	slow := make([]QueryRecord, 0)
	for _, q := range qs.RecentQueries {
		if q.Status == "slow" {
			slow = append(slow, q)
		}
	}
	return slow
}

// GetRecentQueries returns the most recent queries.
func (qs *QueryStats) GetRecentQueries(limit int) []QueryRecord {
	qs.mu.RLock()
	defer qs.mu.RUnlock()

	if limit <= 0 || limit > len(qs.RecentQueries) {
		limit = len(qs.RecentQueries)
	}

	// Return last N queries
	start := len(qs.RecentQueries) - limit
	if start < 0 {
		start = 0
	}

	result := make([]QueryRecord, limit)
	copy(result, qs.RecentQueries[start:])
	return result
}

// cloneMap creates a copy of a map (must be called with lock held).
func (qs *QueryStats) cloneMap(m map[string]int64) map[string]int64 {
	result := make(map[string]int64, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// Reset resets all statistics.
func (qs *QueryStats) Reset() {
	qs.mu.Lock()
	defer qs.mu.Unlock()

	qs.TotalQueries = 0
	qs.QueriesByType = make(map[string]int64)
	qs.AverageLatencyMs = 0
	qs.MaxLatencyMs = 0
	qs.SlowQueryCount = 0
	qs.IndexHitRate = 0
	qs.IndexHits = 0
	qs.IndexMisses = 0
	qs.RecentQueries = make([]QueryRecord, 0)
	qs.StartTime = time.Now().UnixMilli()
}

// ToJSON returns statistics as JSON.
func (qs *QueryStats) ToJSON() ([]byte, error) {
	return json.Marshal(qs.GetStats())
}

// PerformanceMetrics represents aggregated performance data.
type PerformanceMetrics struct {
	Timestamp       int64   `json:"timestamp"`
	QueryThroughput float64 `json:"query_throughput"` // queries per second
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
	P95LatencyMs    float64 `json:"p95_latency_ms"`
	IndexEfficiency float64 `json:"index_efficiency"` // percentage of queries using index
	ErrorRate       float64 `json:"error_rate"`
	ThreadCount     int64   `json:"thread_count"`
}

// GetMetrics returns current performance metrics.
func (qs *QueryStats) GetMetrics(threadCount int64) PerformanceMetrics {
	qs.mu.RLock()
	defer qs.mu.RUnlock()

	uptimeSeconds := float64(time.Now().UnixMilli()-qs.StartTime) / 1000
	throughput := float64(0)
	if uptimeSeconds > 0 {
		throughput = float64(qs.TotalQueries) / uptimeSeconds
	}

	// Calculate P95 latency
	p95Latency := qs.calculatePercentileLatency(95)

	// Calculate error rate from recent queries
	errorRate := qs.calculateErrorRate()

	return PerformanceMetrics{
		Timestamp:       time.Now().UnixMilli(),
		QueryThroughput: throughput,
		AvgLatencyMs:    qs.AverageLatencyMs,
		P95LatencyMs:    p95Latency,
		IndexEfficiency: qs.IndexHitRate,
		ErrorRate:       errorRate,
		ThreadCount:     threadCount,
	}
}

// calculatePercentileLatency calculates latency at a given percentile.
func (qs *QueryStats) calculatePercentileLatency(percentile int) float64 {
	if len(qs.RecentQueries) == 0 {
		return 0
	}

	// Collect latencies
	latencies := make([]float64, len(qs.RecentQueries))
	for i, q := range qs.RecentQueries {
		latencies[i] = float64(q.DurationMs)
	}

	// Sort latencies
	for i := 0; i < len(latencies); i++ {
		for j := i + 1; j < len(latencies); j++ {
			if latencies[j] < latencies[i] {
				latencies[i], latencies[j] = latencies[j], latencies[i]
			}
		}
	}

	// Find percentile index
	index := (percentile * len(latencies)) / 100
	if index >= len(latencies) {
		index = len(latencies) - 1
	}

	return latencies[index]
}

// calculateErrorRate calculates error rate from recent queries.
func (qs *QueryStats) calculateErrorRate() float64 {
	if len(qs.RecentQueries) == 0 {
		return 0
	}

	errorCount := 0
	for _, q := range qs.RecentQueries {
		if q.Status == "error" {
			errorCount++
		}
	}

	return float64(errorCount) / float64(len(qs.RecentQueries)) * 100
}

package threadstore

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryStats_New(t *testing.T) {
	qs := NewQueryStats()

	assert.NotNil(t, qs)
	assert.Equal(t, int64(0), qs.TotalQueries)
	assert.NotNil(t, qs.QueriesByType)
	assert.NotNil(t, qs.QueriesByEndpoint)
	assert.Equal(t, 100.0, qs.SlowQueryThresholdMs)
	assert.Equal(t, 1000, qs.MaxRecentQueries)
	assert.NotZero(t, qs.StartTime)
}

func TestQueryStats_RecordQuery_Success(t *testing.T) {
	qs := NewQueryStats()

	qs.RecordQuery("search", 50, 10, true, false)

	assert.Equal(t, int64(1), qs.TotalQueries)
	assert.Equal(t, int64(1), qs.QueriesByType["search"])
	assert.Equal(t, int64(1), qs.IndexHits)
	assert.Equal(t, int64(0), qs.IndexMisses)
	assert.Equal(t, 100.0, qs.IndexHitRate)
	assert.Equal(t, float64(50), qs.AverageLatencyMs)
	assert.Equal(t, float64(50), qs.MaxLatencyMs)
	assert.Equal(t, int64(0), qs.SlowQueryCount)
	assert.Len(t, qs.RecentQueries, 1)
	assert.Equal(t, "success", qs.RecentQueries[0].Status)
}

func TestQueryStats_RecordQuery_Slow(t *testing.T) {
	qs := NewQueryStats()

	qs.RecordQuery("search", 150, 10, true, false)

	assert.Equal(t, int64(1), qs.TotalQueries)
	assert.Equal(t, int64(1), qs.SlowQueryCount)
	assert.Equal(t, "slow", qs.RecentQueries[0].Status)
}

func TestQueryStats_RecordQuery_Error(t *testing.T) {
	qs := NewQueryStats()

	qs.RecordQuery("get", 30, 1, false, true)

	assert.Equal(t, int64(1), qs.TotalQueries)
	assert.Equal(t, int64(0), qs.IndexHits)
	assert.Equal(t, int64(1), qs.IndexMisses)
	assert.Equal(t, "error", qs.RecentQueries[0].Status)
}

func TestQueryStats_RecordQuery_Multiple(t *testing.T) {
	qs := NewQueryStats()

	// Record multiple queries
	qs.RecordQuery("search", 50, 10, true, false)
	qs.RecordQuery("get", 30, 1, false, false)
	qs.RecordQuery("update", 150, 1, true, false)
	qs.RecordQuery("search", 80, 5, true, false)

	assert.Equal(t, int64(4), qs.TotalQueries)
	assert.Equal(t, int64(2), qs.QueriesByType["search"])
	assert.Equal(t, int64(1), qs.QueriesByType["get"])
	assert.Equal(t, int64(1), qs.QueriesByType["update"])
	assert.Equal(t, int64(3), qs.IndexHits)
	assert.Equal(t, int64(1), qs.IndexMisses)
	assert.Equal(t, int64(1), qs.SlowQueryCount)
	assert.Len(t, qs.RecentQueries, 4)
}

func TestQueryStats_UpdateLatencyMetrics(t *testing.T) {
	qs := NewQueryStats()

	// First query
	qs.RecordQuery("search", 100, 10, true, false)
	assert.Equal(t, float64(100), qs.AverageLatencyMs)
	assert.Equal(t, float64(100), qs.MaxLatencyMs)

	// Second query
	qs.RecordQuery("search", 200, 10, true, false)
	assert.Equal(t, float64(150), qs.AverageLatencyMs) // (100 + 200) / 2
	assert.Equal(t, float64(200), qs.MaxLatencyMs)

	// Third query
	qs.RecordQuery("search", 50, 10, true, false)
	assert.InDelta(t, float64(116.67), qs.AverageLatencyMs, 0.1)
	assert.Equal(t, float64(200), qs.MaxLatencyMs) // Max stays
}

func TestQueryStats_GetStats(t *testing.T) {
	qs := NewQueryStats()

	qs.RecordQuery("search", 50, 10, true, false)
	qs.RecordQuery("get", 30, 1, false, false)

	stats := qs.GetStats()

	assert.Equal(t, int64(2), stats["total_queries"])
	assert.NotNil(t, stats["queries_by_type"])
	assert.Equal(t, float64(40), stats["average_latency_ms"])
	assert.Equal(t, float64(50), stats["max_latency_ms"])
	assert.Equal(t, int64(0), stats["slow_query_count"])
	assert.Equal(t, 50.0, stats["index_hit_rate"])
	assert.Equal(t, int64(1), stats["index_hits"])
	assert.Equal(t, int64(1), stats["index_misses"])
	assert.GreaterOrEqual(t, stats["uptime_seconds"], int64(0))
	assert.Equal(t, 2, stats["recent_query_count"])
}

func TestQueryStats_GetSlowQueries(t *testing.T) {
	qs := NewQueryStats()

	// Record mix of queries
	qs.RecordQuery("search", 50, 10, true, false)
	qs.RecordQuery("search", 150, 10, true, false) // Slow
	qs.RecordQuery("get", 30, 1, false, false)
	qs.RecordQuery("search", 200, 5, true, false) // Slow

	slowQueries := qs.GetSlowQueries()

	assert.Len(t, slowQueries, 2)
	for _, q := range slowQueries {
		assert.Equal(t, "slow", q.Status)
	}
}

func TestQueryStats_GetSlowQueries_NoSlow(t *testing.T) {
	qs := NewQueryStats()

	qs.RecordQuery("search", 50, 10, true, false)
	qs.RecordQuery("get", 30, 1, false, false)

	slowQueries := qs.GetSlowQueries()

	assert.Len(t, slowQueries, 0)
}

func TestQueryStats_GetRecentQueries(t *testing.T) {
	qs := NewQueryStats()

	// Record multiple queries
	for i := 1; i <= 10; i++ {
		qs.RecordQuery("search", int64(i*10), i, true, false)
	}

	// Get last 5
	recent := qs.GetRecentQueries(5)

	assert.Len(t, recent, 5)
	// Should be the last 5 (6-10)
	assert.Equal(t, int64(60), recent[0].DurationMs)
	assert.Equal(t, int64(100), recent[4].DurationMs)
}

func TestQueryStats_GetRecentQueries_All(t *testing.T) {
	qs := NewQueryStats()

	for i := 1; i <= 5; i++ {
		qs.RecordQuery("search", int64(i*10), i, true, false)
	}

	// Get all (limit <= 0 or limit > len)
	recent := qs.GetRecentQueries(0)
	assert.Len(t, recent, 5)

	recent = qs.GetRecentQueries(100)
	assert.Len(t, recent, 5)
}

func TestQueryStats_GetRecentQueries_Empty(t *testing.T) {
	qs := NewQueryStats()

	recent := qs.GetRecentQueries(10)

	assert.Len(t, recent, 0)
}

func TestQueryStats_CloneMap(t *testing.T) {
	qs := NewQueryStats()

	original := map[string]int64{
		"search": 10,
		"get":    5,
	}

	cloned := qs.cloneMap(original)

	assert.Equal(t, original, cloned)

	// Modify clone should not affect original
	cloned["search"] = 20
	assert.Equal(t, int64(10), original["search"])
}

func TestQueryStats_Reset(t *testing.T) {
	qs := NewQueryStats()

	// Record some queries
	qs.RecordQuery("search", 50, 10, true, false)
	qs.RecordQuery("get", 30, 1, false, false)

	// Reset
	qs.Reset()

	assert.Equal(t, int64(0), qs.TotalQueries)
	assert.Len(t, qs.QueriesByType, 0)
	assert.Equal(t, float64(0), qs.AverageLatencyMs)
	assert.Equal(t, float64(0), qs.MaxLatencyMs)
	assert.Equal(t, int64(0), qs.SlowQueryCount)
	assert.Equal(t, float64(0), qs.IndexHitRate)
	assert.Equal(t, int64(0), qs.IndexHits)
	assert.Equal(t, int64(0), qs.IndexMisses)
	assert.Len(t, qs.RecentQueries, 0)
}

func TestQueryStats_ToJSON(t *testing.T) {
	qs := NewQueryStats()

	qs.RecordQuery("search", 50, 10, true, false)

	jsonData, err := qs.ToJSON()

	require.NoError(t, err)
	assert.Contains(t, string(jsonData), "total_queries")
	assert.Contains(t, string(jsonData), "average_latency_ms")
}

func TestQueryStats_GetMetrics(t *testing.T) {
	qs := NewQueryStats()

	// Record queries
	qs.RecordQuery("search", 50, 10, true, false)
	qs.RecordQuery("get", 30, 1, false, false)
	qs.RecordQuery("update", 150, 1, true, true) // Error

	metrics := qs.GetMetrics(100)

	assert.NotZero(t, metrics.Timestamp)
	assert.GreaterOrEqual(t, metrics.QueryThroughput, float64(0))
	assert.InDelta(t, 76.67, metrics.AvgLatencyMs, 0.1) // Approximate
	assert.GreaterOrEqual(t, metrics.P95LatencyMs, float64(0))
	assert.GreaterOrEqual(t, metrics.IndexEfficiency, float64(0))
	assert.GreaterOrEqual(t, metrics.ErrorRate, float64(0))
	assert.Equal(t, int64(100), metrics.ThreadCount)
}

func TestQueryStats_CalculatePercentileLatency(t *testing.T) {
	qs := NewQueryStats()

	// Record queries with latencies 10, 20, 30, 40, 50
	for i := 1; i <= 5; i++ {
		qs.RecordQuery("search", int64(i*10), i, true, false)
	}

	// P50 should be around 30
	p50 := qs.calculatePercentileLatency(50)
	assert.Equal(t, float64(30), p50)

	// P90 should be around 50
	p90 := qs.calculatePercentileLatency(90)
	assert.Equal(t, float64(50), p90)

	// P100 should be 50
	p100 := qs.calculatePercentileLatency(100)
	assert.Equal(t, float64(50), p100)
}

func TestQueryStats_CalculatePercentileLatency_Empty(t *testing.T) {
	qs := NewQueryStats()

	p50 := qs.calculatePercentileLatency(50)

	assert.Equal(t, float64(0), p50)
}

func TestQueryStats_CalculateErrorRate(t *testing.T) {
	qs := NewQueryStats()

	// Record mix of success and error
	qs.RecordQuery("search", 50, 10, true, false) // Success
	qs.RecordQuery("get", 30, 1, false, true)     // Error
	qs.RecordQuery("search", 40, 5, true, false)  // Success
	qs.RecordQuery("update", 60, 1, false, true)  // Error

	errorRate := qs.calculateErrorRate()

	assert.Equal(t, 50.0, errorRate) // 2 errors out of 4 queries
}

func TestQueryStats_CalculateErrorRate_Empty(t *testing.T) {
	qs := NewQueryStats()

	errorRate := qs.calculateErrorRate()

	assert.Equal(t, float64(0), errorRate)
}

func TestQueryStats_RecordQuery_TrimOldRecords(t *testing.T) {
	qs := NewQueryStats()
	qs.MaxRecentQueries = 5 // Reduce limit for testing

	// Record more than limit
	for i := 1; i <= 10; i++ {
		qs.RecordQuery("search", int64(i*10), i, true, false)
	}

	// Should only keep last 5
	assert.Len(t, qs.RecentQueries, 5)
	assert.Equal(t, int64(60), qs.RecentQueries[0].DurationMs) // 6th query
	assert.Equal(t, int64(100), qs.RecentQueries[4].DurationMs) // 10th query
}

func TestQueryStats_GetMetrics_Throughput(t *testing.T) {
	qs := NewQueryStats()

	// Record queries over time
	for i := 1; i <= 10; i++ {
		qs.RecordQuery("search", 50, 10, true, false)
	}

	// Wait a bit to ensure uptime > 0
	time.Sleep(10 * time.Millisecond)

	metrics := qs.GetMetrics(100)

	// Throughput = total_queries / uptime_seconds
	// Since uptime is very small, throughput should be large
	assert.GreaterOrEqual(t, metrics.QueryThroughput, float64(0))
}

func TestQueryStats_IndexHitRate(t *testing.T) {
	qs := NewQueryStats()

	// 3 index hits, 1 miss
	qs.RecordQuery("search", 50, 10, true, false)
	qs.RecordQuery("search", 50, 10, true, false)
	qs.RecordQuery("search", 50, 10, true, false)
	qs.RecordQuery("get", 30, 1, false, false)

	assert.Equal(t, 75.0, qs.IndexHitRate) // 3/4 * 100
}

func TestQueryStats_Concurrent(t *testing.T) {
	qs := NewQueryStats()

	// Simulate concurrent queries
	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				qs.RecordQuery("search", int64(j), 10, true, false)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// All queries should be recorded
	assert.Equal(t, int64(1000), qs.TotalQueries)
}

func TestQueryRecord(t *testing.T) {
	record := QueryRecord{
		Timestamp:   time.Now().UnixMilli(),
		QueryType:   "search",
		DurationMs:  50,
		ThreadCount: 10,
		Status:      "success",
		UsedIndex:   true,
	}

	assert.NotZero(t, record.Timestamp)
	assert.Equal(t, "search", record.QueryType)
	assert.Equal(t, int64(50), record.DurationMs)
	assert.Equal(t, 10, record.ThreadCount)
	assert.Equal(t, "success", record.Status)
	assert.True(t, record.UsedIndex)
}

func TestPerformanceMetrics(t *testing.T) {
	metrics := PerformanceMetrics{
		Timestamp:       time.Now().UnixMilli(),
		QueryThroughput: 100.5,
		AvgLatencyMs:    50.0,
		P95LatencyMs:    150.0,
		IndexEfficiency: 85.0,
		ErrorRate:       2.5,
		ThreadCount:     100,
	}

	assert.NotZero(t, metrics.Timestamp)
	assert.Equal(t, 100.5, metrics.QueryThroughput)
	assert.Equal(t, 50.0, metrics.AvgLatencyMs)
	assert.Equal(t, 150.0, metrics.P95LatencyMs)
	assert.Equal(t, 85.0, metrics.IndexEfficiency)
	assert.Equal(t, 2.5, metrics.ErrorRate)
	assert.Equal(t, int64(100), metrics.ThreadCount)
}

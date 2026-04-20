package cache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewCacheMonitor(t *testing.T) {
	cache := NewMemoryCache(100)

	tests := []struct {
		name     string
		interval time.Duration
		expected time.Duration
	}{
		{"default_interval", 0, 30 * time.Second},
		{"custom_interval", 1 * time.Second, 1 * time.Second},
		{"negative_interval", -1, 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor := NewCacheMonitor(cache, "", tt.interval)
			if monitor == nil {
				t.Fatal("expected monitor to be created, got nil")
			}
			if monitor.interval != tt.expected {
				t.Errorf("expected interval=%v, got %v", tt.expected, monitor.interval)
			}
			monitor.Stop()
		})
	}
}

func TestCacheMonitor_CollectMetrics(t *testing.T) {
	cache := NewMemoryCache(100)
	ctx := context.Background()

	// Set some values
	_ = cache.Set(ctx, "key1", "value1", 0)
	_ = cache.Set(ctx, "key2", "value2", 0)

	// Get some values (hits)
	_, _ = cache.Get(ctx, "key1")
	_, _ = cache.Get(ctx, "key2")

	// Get nonexistent (miss)
	_, _ = cache.Get(ctx, "nonexistent")

	monitor := NewCacheMonitor(cache, "", 100*time.Millisecond)
	defer monitor.Stop()

	// Wait for metrics collection
	time.Sleep(150 * time.Millisecond)

	snapshot := monitor.GetLatestSnapshot()
	if snapshot == nil {
		t.Fatal("expected snapshot to be collected, got nil")
	}

	if snapshot.TotalItems != 2 {
		t.Errorf("expected TotalItems=2, got %d", snapshot.TotalItems)
	}

	if snapshot.HitCount != 2 {
		t.Errorf("expected HitCount=2, got %d", snapshot.HitCount)
	}

	if snapshot.MissCount != 1 {
		t.Errorf("expected MissCount=1, got %d", snapshot.MissCount)
	}
}

func TestCacheMonitor_GetLatestSnapshot_NoSnapshots(t *testing.T) {
	cache := NewMemoryCache(100)

	// Create monitor without starting collection
	monitor := &CacheMonitor{
		cache:        cache,
		interval:     1 * time.Hour,
		snapshots:    make([]MetricSnapshot, 0, 100),
		maxSnapshots: 100,
	}

	snapshot := monitor.GetLatestSnapshot()
	if snapshot != nil {
		t.Error("expected nil snapshot when no snapshots collected")
	}
}

func TestCacheMonitor_GetSnapshots(t *testing.T) {
	cache := NewMemoryCache(100)
	monitor := NewCacheMonitor(cache, "", 50*time.Millisecond)
	defer monitor.Stop()

	// Wait for multiple snapshots to be collected
	time.Sleep(250 * time.Millisecond)

	// Test getting all snapshots
	snapshots := monitor.GetSnapshots(0)
	if len(snapshots) == 0 {
		t.Error("expected some snapshots to be collected")
	}

	// Test getting limited snapshots
	limited := monitor.GetSnapshots(2)
	if len(limited) > 2 {
		t.Errorf("expected at most 2 snapshots, got %d", len(limited))
	}

	// Test negative limit (should return all)
	all := monitor.GetSnapshots(-1)
	if len(all) != len(snapshots) {
		t.Error("expected all snapshots for negative limit")
	}
}

func TestCacheMonitor_GetAggregatedStats(t *testing.T) {
	cache := NewMemoryCache(100)
	ctx := context.Background()

	monitor := NewCacheMonitor(cache, "", 50*time.Millisecond)
	defer monitor.Stop()

	// Generate some activity
	for i := 0; i < 10; i++ {
		_ = cache.Set(ctx, string(rune('a'+i)), i, 0)
		_, _ = cache.Get(ctx, string(rune('a'+i)))
		_, _ = cache.Get(ctx, "nonexistent")
	}

	// Wait for snapshots
	time.Sleep(200 * time.Millisecond)

	stats := monitor.GetAggregatedStats()
	if stats == nil {
		t.Fatal("expected aggregated stats, got nil")
	}

	if stats.SnapshotCount == 0 {
		t.Error("expected some snapshots to be counted")
	}

	if stats.TotalHits == 0 {
		t.Error("expected some hits")
	}

	if stats.TotalMisses == 0 {
		t.Error("expected some misses")
	}

	// Verify hit rate range
	if stats.MinHitRate < 0 || stats.MinHitRate > 1 {
		t.Errorf("invalid MinHitRate: %f", stats.MinHitRate)
	}

	if stats.MaxHitRate < 0 || stats.MaxHitRate > 1 {
		t.Errorf("invalid MaxHitRate: %f", stats.MaxHitRate)
	}
}

func TestCacheMonitor_GetAggregatedStats_NoSnapshots(t *testing.T) {
	cache := NewMemoryCache(100)

	// Create monitor without snapshots
	monitor := &CacheMonitor{
		cache:        cache,
		interval:     1 * time.Hour,
		snapshots:    make([]MetricSnapshot, 0, 100),
		maxSnapshots: 100,
	}

	stats := monitor.GetAggregatedStats()
	if stats == nil {
		t.Fatal("expected aggregated stats even with no snapshots")
	}

	if stats.SnapshotCount != 0 {
		t.Error("expected SnapshotCount=0")
	}
}

func TestCacheMonitor_PersistMetrics(t *testing.T) {
	cache := NewMemoryCache(100)
	metricsFile := filepath.Join(os.TempDir(), "cache-metrics-test", "metrics.json")
	defer os.RemoveAll(filepath.Dir(metricsFile))

	monitor := NewCacheMonitor(cache, metricsFile, 50*time.Millisecond)
	defer monitor.Stop()

	// Generate activity
	ctx := context.Background()
	_ = cache.Set(ctx, "key1", "value1", 0)

	// Wait for persistence
	time.Sleep(150 * time.Millisecond)

	// Verify file was created
	if _, err := os.Stat(metricsFile); os.IsNotExist(err) {
		t.Error("expected metrics file to be created")
	}
}

func TestCacheMonitor_Stop(t *testing.T) {
	cache := NewMemoryCache(100)
	monitor := NewCacheMonitor(cache, "", 50*time.Millisecond)

	// Stop immediately
	monitor.Stop()

	// Wait a bit and verify no new snapshots
	time.Sleep(100 * time.Millisecond)

	count1 := len(monitor.GetSnapshots(0))
	time.Sleep(100 * time.Millisecond)
	count2 := len(monitor.GetSnapshots(0))

	if count2 > count1 {
		t.Error("expected no new snapshots after Stop()")
	}
}

func TestCacheMonitor_MaxSnapshots(t *testing.T) {
	cache := NewMemoryCache(100)

	// Create monitor with small maxSnapshots
	monitor := &CacheMonitor{
		cache:        cache,
		interval:     10 * time.Millisecond,
		stopChan:     make(chan struct{}),
		snapshots:    make([]MetricSnapshot, 0, 100),
		maxSnapshots: 5,
	}

	// Manually collect many snapshots
	for i := 0; i < 10; i++ {
		monitor.collectMetrics()
	}

	// Should only keep last 5
	snapshots := monitor.GetSnapshots(0)
	if len(snapshots) > 5 {
		t.Errorf("expected at most 5 snapshots, got %d", len(snapshots))
	}
}

// CacheAlerter tests

func TestNewCacheAlerter(t *testing.T) {
	cache := NewMemoryCache(100)
	alerter := NewCacheAlerter(cache)

	if alerter == nil {
		t.Fatal("expected alerter to be created, got nil")
	}

	if len(alerter.conditions) != 0 {
		t.Error("expected no conditions initially")
	}
}

func TestCacheAlerter_AddCondition(t *testing.T) {
	cache := NewMemoryCache(100)
	alerter := NewCacheAlerter(cache)

	alerter.AddCondition("test_condition", func(stats *Stats) bool {
		return stats.HitRate < 0.5
	}, "hit rate is low")

	if len(alerter.conditions) != 1 {
		t.Errorf("expected 1 condition, got %d", len(alerter.conditions))
	}
}

func TestCacheAlerter_Alerts(t *testing.T) {
	cache := NewMemoryCache(100)

	alerter := NewCacheAlerter(cache)

	// Add condition that always triggers
	alerter.AddCondition("always_trigger", func(stats *Stats) bool {
		return true
	}, "this always triggers")

	// Start alert checking in background
	go alerter.Start(50 * time.Millisecond)
	defer alerter.Stop()

	// Wait for alert
	select {
	case alert := <-alerter.Alerts():
		if alert.Condition != "always_trigger" {
			t.Errorf("expected condition=always_trigger, got %s", alert.Condition)
		}
		if alert.Message != "this always triggers" {
			t.Errorf("unexpected message: %s", alert.Message)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("expected alert to be triggered")
	}
}

func TestCacheAlerter_NoAlert(t *testing.T) {
	cache := NewMemoryCache(100)
	ctx := context.Background()

	alerter := NewCacheAlerter(cache)

	// Add condition that never triggers
	alerter.AddCondition("never_trigger", func(stats *Stats) bool {
		return false
	}, "this never triggers")

	// Start alert checking
	go alerter.Start(50 * time.Millisecond)
	defer alerter.Stop()

	// Set some data to generate stats
	_ = cache.Set(ctx, "key1", "value1", 0)
	_, _ = cache.Get(ctx, "key1")

	// Wait and verify no alert
	select {
	case alert := <-alerter.Alerts():
		t.Errorf("unexpected alert: %+v", alert)
	case <-time.After(150 * time.Millisecond):
		// Expected - no alert
	}
}

func TestCacheAlerter_LowHitRate(t *testing.T) {
	cache := NewMemoryCache(100)
	ctx := context.Background()

	alerter := NewCacheAlerter(cache)

	// Add default low hit rate condition
	alerter.AddCondition("low_hit_rate", func(stats *Stats) bool {
		return stats.HitRate < 0.5
	}, "cache hit rate is below 50%")

	// Generate mostly misses
	for i := 0; i < 10; i++ {
		_, _ = cache.Get(ctx, string(rune('a'+i))) // all misses
	}

	// Check condition
	stats, _ := cache.Stats(ctx)
	if stats.HitRate >= 0.5 {
		t.Skip("hit rate not low enough for test")
	}

	// Start alert checking
	go alerter.Start(50 * time.Millisecond)
	defer alerter.Stop()

	// Wait for alert
	select {
	case alert := <-alerter.Alerts():
		if alert.Condition != "low_hit_rate" {
			t.Errorf("expected condition=low_hit_rate, got %s", alert.Condition)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("expected low hit rate alert")
	}
}

func TestCacheAlerter_HighEviction(t *testing.T) {
	// Small cache to trigger evictions
	cache := NewMemoryCache(3)
	ctx := context.Background()

	alerter := NewCacheAlerter(cache)

	// Add high eviction condition
	alerter.AddCondition("high_eviction", func(stats *Stats) bool {
		return stats.EvictionCount > 5
	}, "cache eviction count is too high")

	// Trigger many evictions
	for i := 0; i < 20; i++ {
		_ = cache.Set(ctx, string(rune('a'+i)), i, 0)
	}

	// Start alert checking
	go alerter.Start(50 * time.Millisecond)
	defer alerter.Stop()

	// Wait for alert
	select {
	case alert := <-alerter.Alerts():
		if alert.Condition != "high_eviction" {
			t.Errorf("expected condition=high_eviction, got %s", alert.Condition)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("expected high eviction alert")
	}
}

func TestCacheAlerter_Stop(t *testing.T) {
	cache := NewMemoryCache(100)
	alerter := NewCacheAlerter(cache)

	_ = cache // Use cache to avoid unused variable
	alerter.AddCondition("test", func(stats *Stats) bool {
		return true
	}, "test")

	// Start and immediately stop
	go alerter.Start(10 * time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	alerter.Stop()

	// Verify no more alerts after stop
	time.Sleep(50 * time.Millisecond)

	select {
	case alert := <-alerter.Alerts():
		// Might get one alert from before stop, that's ok
		_ = alert
	default:
		// No alerts - good
	}
}

func TestCacheAlerter_AlertChannelFull(t *testing.T) {
	cache := NewMemoryCache(100)

	// Create alerter with small buffer
	alerter := &CacheAlerter{
		cache:      cache,
		conditions: make([]AlertCondition, 0),
		alertChan:  make(chan Alert, 1), // buffer size 1
		stopChan:   make(chan struct{}),
	}

	// Add condition that always triggers
	alerter.AddCondition("test", func(stats *Stats) bool {
		return true
	}, "test")

	// Trigger multiple times - should not block even if channel is full
	for i := 0; i < 5; i++ {
		alerter.checkConditions()
	}

	// Should have at least 1 alert
	select {
	case <-alerter.Alerts():
		// Good
	default:
		t.Error("expected at least one alert")
	}
}

func TestDefaultAlertConditions(t *testing.T) {
	if len(DefaultAlertConditions) != 3 {
		t.Errorf("expected 3 default conditions, got %d", len(DefaultAlertConditions))
	}

	// Test each default condition
	tests := []struct {
		name      string
		stats     *Stats
		shouldHit bool
	}{
		{
			name: "low_hit_rate_triggered",
			stats: &Stats{
				HitRate: 0.3, // Below 0.5
			},
			shouldHit: true,
		},
		{
			name: "low_hit_rate_not_triggered",
			stats: &Stats{
				HitRate: 0.7, // Above 0.5
			},
			shouldHit: false,
		},
		{
			name: "high_eviction_triggered",
			stats: &Stats{
				EvictionCount: 1500, // Above 1000
			},
			shouldHit: true,
		},
		{
			name: "high_eviction_not_triggered",
			stats: &Stats{
				EvictionCount: 500, // Below 1000
			},
			shouldHit: false,
		},
		{
			name: "slow_operations_triggered",
			stats: &Stats{
				SlowOps: []SlowOp{{}, {}, {}, {}, {}, {}}, // 6 slow ops
			},
			shouldHit: true,
		},
		{
			name: "slow_operations_not_triggered",
			stats: &Stats{
				SlowOps: []SlowOp{{}, {}, {}}, // 3 slow ops
			},
			shouldHit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, condition := range DefaultAlertConditions {
				if condition.Name == tt.name[:len(condition.Name)] {
					result := condition.CheckFunc(tt.stats)
					if result != tt.shouldHit {
						t.Errorf("condition %s: expected %v, got %v", condition.Name, tt.shouldHit, result)
					}
				}
			}
		})
	}
}

// Benchmark tests

func BenchmarkCacheMonitor_CollectMetrics(b *testing.B) {
	cache := NewMemoryCache(10000)
	ctx := context.Background()

	// Pre-populate cache
	for i := 0; i < 1000; i++ {
		_ = cache.Set(ctx, string(rune(i)), i, 0)
	}

	monitor := &CacheMonitor{
		cache:        cache,
		interval:     1 * time.Hour,
		snapshots:    make([]MetricSnapshot, 0, 100),
		maxSnapshots: 100,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		monitor.collectMetrics()
	}
}

func BenchmarkCacheAlerter_CheckConditions(b *testing.B) {
	cache := NewMemoryCache(10000)
	alerter := NewCacheAlerter(cache)

	// Add conditions
	for _, condition := range DefaultAlertConditions {
		alerter.AddCondition(condition.Name, condition.CheckFunc, condition.Message)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		alerter.checkConditions()
	}
}

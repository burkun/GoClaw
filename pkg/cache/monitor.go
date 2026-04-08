package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CacheMonitor 缓存监控器
type CacheMonitor struct {
	cache       Cache
	metricsFile string
	interval    time.Duration
	stopChan    chan struct{}
	mu          sync.RWMutex
	snapshots   []MetricSnapshot
	maxSnapshots int
}

// MetricSnapshot 监控快照
type MetricSnapshot struct {
	Timestamp    time.Time `json:"timestamp"`
	TotalItems   int64     `json:"total_items"`
	HitCount     int64     `json:"hit_count"`
	MissCount    int64     `json:"miss_count"`
	HitRate      float64   `json:"hit_rate"`
	TotalSize    int64     `json:"total_size"`
	AvgLatency   string    `json:"avg_latency"`
	EvictionCount int64    `json:"eviction_count"`
}

// NewCacheMonitor 创建缓存监控器
func NewCacheMonitor(cache Cache, metricsFile string, interval time.Duration) *CacheMonitor {
	if interval <= 0 {
		interval = 30 * time.Second
	}

	cm := &CacheMonitor{
		cache:        cache,
		metricsFile:  metricsFile,
		interval:     interval,
		stopChan:     make(chan struct{}),
		snapshots:    make([]MetricSnapshot, 0, 100),
		maxSnapshots: 100,
	}

	// 启动监控goroutine
	go cm.monitor()

	return cm
}

// monitor 监控循环
func (cm *CacheMonitor) monitor() {
	ticker := time.NewTicker(cm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-cm.stopChan:
			return
		case <-ticker.C:
			cm.collectMetrics()
		}
	}
}

// collectMetrics 收集监控指标
func (cm *CacheMonitor) collectMetrics() {
	ctx := context.Background()
	stats, err := cm.cache.Stats(ctx)
	if err != nil {
		return
	}

	snapshot := MetricSnapshot{
		Timestamp:     time.Now(),
		TotalItems:    stats.TotalItems,
		HitCount:      stats.HitCount,
		MissCount:     stats.MissCount,
		HitRate:       stats.HitRate,
		TotalSize:     stats.TotalSize,
		AvgLatency:    stats.AvgLatency.String(),
		EvictionCount: stats.EvictionCount,
	}

	cm.mu.Lock()
	cm.snapshots = append(cm.snapshots, snapshot)
	// 只保留最近的快照
	if len(cm.snapshots) > cm.maxSnapshots {
		cm.snapshots = cm.snapshots[len(cm.snapshots)-cm.maxSnapshots:]
	}
	cm.mu.Unlock()

	// 持久化到文件
	if cm.metricsFile != "" {
		cm.persistMetrics(snapshot)
	}
}

// persistMetrics 持久化监控指标
func (cm *CacheMonitor) persistMetrics(snapshot MetricSnapshot) error {
	// 确保目录存在
	dir := filepath.Dir(cm.metricsFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create metrics directory: %w", err)
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("failed to marshal metrics: %w", err)
	}

	if err := os.WriteFile(cm.metricsFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write metrics file: %w", err)
	}

	return nil
}

// GetLatestSnapshot 获取最新的监控快照
func (cm *CacheMonitor) GetLatestSnapshot() *MetricSnapshot {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if len(cm.snapshots) == 0 {
		return nil
	}

	snapshot := cm.snapshots[len(cm.snapshots)-1]
	return &snapshot
}

// GetSnapshots 获取历史快照
func (cm *CacheMonitor) GetSnapshots(limit int) []MetricSnapshot {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if limit <= 0 || limit > len(cm.snapshots) {
		limit = len(cm.snapshots)
	}

	result := make([]MetricSnapshot, limit)
	copy(result, cm.snapshots[len(cm.snapshots)-limit:])
	return result
}

// GetAggregatedStats 获取聚合统计
func (cm *CacheMonitor) GetAggregatedStats() *AggregatedStats {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if len(cm.snapshots) == 0 {
		return &AggregatedStats{}
	}

	var totalHits, totalMisses int64
	var totalHitRate float64
	var minHitRate, maxHitRate float64 = 100, 0

	for _, snapshot := range cm.snapshots {
		totalHits += snapshot.HitCount
		totalMisses += snapshot.MissCount
		totalHitRate += snapshot.HitRate

		if snapshot.HitRate < minHitRate {
			minHitRate = snapshot.HitRate
		}
		if snapshot.HitRate > maxHitRate {
			maxHitRate = snapshot.HitRate
		}
	}

	latest := cm.snapshots[len(cm.snapshots)-1]
	avgHitRate := totalHitRate / float64(len(cm.snapshots))

	return &AggregatedStats{
		SnapshotCount:    len(cm.snapshots),
		TotalHits:        totalHits,
		TotalMisses:      totalMisses,
		AverageHitRate:   avgHitRate,
		MinHitRate:       minHitRate,
		MaxHitRate:       maxHitRate,
		CurrentItems:     latest.TotalItems,
		CurrentSize:      latest.TotalSize,
		TotalEvictions:   latest.EvictionCount,
		FirstSnapshot:    cm.snapshots[0].Timestamp,
		LatestSnapshot:   latest.Timestamp,
	}
}

// Stop 停止监控
func (cm *CacheMonitor) Stop() {
	close(cm.stopChan)
}

// AggregatedStats 聚合统计
type AggregatedStats struct {
	SnapshotCount    int       `json:"snapshot_count"`
	TotalHits        int64     `json:"total_hits"`
	TotalMisses      int64     `json:"total_misses"`
	AverageHitRate   float64   `json:"average_hit_rate"`
	MinHitRate       float64   `json:"min_hit_rate"`
	MaxHitRate       float64   `json:"max_hit_rate"`
	CurrentItems     int64     `json:"current_items"`
	CurrentSize      int64     `json:"current_size"`
	TotalEvictions   int64     `json:"total_evictions"`
	FirstSnapshot    time.Time `json:"first_snapshot"`
	LatestSnapshot   time.Time `json:"latest_snapshot"`
}

// AlertCondition 告警条件
type AlertCondition struct {
	Name      string
	CheckFunc func(stats *Stats) bool
	Message   string
}

// Alert 告警
type Alert struct {
	Condition string    `json:"condition"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// CacheAlerter 缓存告警器
type CacheAlerter struct {
	cache      Cache
	conditions []AlertCondition
	alertChan  chan Alert
	stopChan   chan struct{}
}

// NewCacheAlerter 创建缓存告警器
func NewCacheAlerter(cache Cache) *CacheAlerter {
	return &CacheAlerter{
		cache:      cache,
		conditions: make([]AlertCondition, 0),
		alertChan:  make(chan Alert, 100),
		stopChan:   make(chan struct{}),
	}
}

// AddCondition 添加告警条件
func (ca *CacheAlerter) AddCondition(name string, checkFunc func(stats *Stats) bool, message string) {
	ca.conditions = append(ca.conditions, AlertCondition{
		Name:      name,
		CheckFunc: checkFunc,
		Message:   message,
	})
}

// Start 启动告警检查
func (ca *CacheAlerter) Start(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ca.stopChan:
			return
		case <-ticker.C:
			ca.checkConditions()
		}
	}
}

// checkConditions 检查告警条件
func (ca *CacheAlerter) checkConditions() {
	ctx := context.Background()
	stats, err := ca.cache.Stats(ctx)
	if err != nil {
		return
	}

	for _, condition := range ca.conditions {
		if condition.CheckFunc(stats) {
			alert := Alert{
				Condition: condition.Name,
				Message:   condition.Message,
				Timestamp: time.Now(),
			}
			select {
			case ca.alertChan <- alert:
			default:
				// channel已满,丢弃告警
			}
		}
	}
}

// Alerts 返回告警channel
func (ca *CacheAlerter) Alerts() <-chan Alert {
	return ca.alertChan
}

// Stop 停止告警器
func (ca *CacheAlerter) Stop() {
	close(ca.stopChan)
}

// DefaultAlertConditions 默认告警条件
var DefaultAlertConditions = []AlertCondition{
	{
		Name: "low_hit_rate",
		CheckFunc: func(stats *Stats) bool {
			return stats.HitRate < 0.5 // 命中率低于50%
		},
		Message: "cache hit rate is below 50%",
	},
	{
		Name: "high_eviction",
		CheckFunc: func(stats *Stats) bool {
			return stats.EvictionCount > 1000 // 驱逐次数过多
		},
		Message: "cache eviction count is too high",
	},
	{
		Name: "slow_operations",
		CheckFunc: func(stats *Stats) bool {
			return len(stats.SlowOps) > 5 // 慢操作过多
		},
		Message: "too many slow cache operations detected",
	},
}

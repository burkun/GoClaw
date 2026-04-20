package cache

import (
	"context"
	"sync"
	"time"
)

// MemoryCache 内存缓存实现 (L1)
type MemoryCache struct {
	items    sync.Map // map[string]*CacheItem
	stats    *memoryStats
	mu       sync.RWMutex
	maxItems int
	onEvict  func(key string, value interface{})
}

type memoryStats struct {
	hitCount      int64
	missCount     int64
	evictionCount int64
	slowOps       []SlowOp
	slowThreshold time.Duration
	mu            sync.RWMutex
}

// NewMemoryCache 创建内存缓存
func NewMemoryCache(maxItems int) *MemoryCache {
	if maxItems <= 0 {
		maxItems = 10000 // 默认最大10000项
	}

	mc := &MemoryCache{
		maxItems: maxItems,
		stats: &memoryStats{
			slowOps:       make([]SlowOp, 0, 10),
			slowThreshold: 100 * time.Millisecond, // 慢操作阈值
		},
	}

	// 启动后台清理goroutine
	go mc.cleanupExpired()

	return mc
}

// Get 获取缓存值
func (mc *MemoryCache) Get(ctx context.Context, key string) (interface{}, error) {
	start := time.Now()

	val, ok := mc.items.Load(key)
	if !ok {
		mc.recordMiss()
		return nil, ErrNotFound
	}

	item := val.(*CacheItem)

	// 检查是否过期
	if item.IsExpired() {
		mc.items.Delete(key)
		mc.recordMiss()
		return nil, ErrExpired
	}

	// 更新命中计数
	item.HitCount++
	mc.recordHit()

	// 记录慢操作
	mc.recordSlowOp("get", key, time.Since(start))

	return item.Value, nil
}

// Set 设置缓存值
func (mc *MemoryCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	start := time.Now()

	mc.mu.Lock()
	defer mc.mu.Unlock()

	// 检查是否需要驱逐
	count := 0
	mc.items.Range(func(_, _ interface{}) bool {
		count++
		return true
	})

	if count >= mc.maxItems {
		// 驱逐策略: 删除过期项和最少使用的项
		mc.evict(count - mc.maxItems + 1)
	}

	now := time.Now()
	item := &CacheItem{
		Value:     value,
		CreatedAt: now,
		HitCount:  0,
	}

	if ttl > 0 {
		item.ExpireAt = now.Add(ttl)
	}

	mc.items.Store(key, item)
	mc.recordSlowOp("set", key, time.Since(start))

	return nil
}

// Delete 删除缓存值
func (mc *MemoryCache) Delete(ctx context.Context, key string) error {
	start := time.Now()

	mc.items.Delete(key)
	mc.recordSlowOp("delete", key, time.Since(start))

	return nil
}

// Exists 检查缓存是否存在
func (mc *MemoryCache) Exists(ctx context.Context, key string) (bool, error) {
	val, ok := mc.items.Load(key)
	if !ok {
		return false, nil
	}

	item := val.(*CacheItem)
	return !item.IsExpired(), nil
}

// Clear 清空所有缓存
func (mc *MemoryCache) Clear(ctx context.Context) error {
	mc.items = sync.Map{}
	mc.mu.Lock()
	mc.stats.hitCount = 0
	mc.stats.missCount = 0
	mc.stats.evictionCount = 0
	mc.stats.slowOps = make([]SlowOp, 0, 10)
	mc.mu.Unlock()
	return nil
}

// Stats 获取缓存统计信息
func (mc *MemoryCache) Stats(ctx context.Context) (*Stats, error) {
	mc.stats.mu.RLock()
	defer mc.stats.mu.RUnlock()

	count := int64(0)
	mc.items.Range(func(_, _ interface{}) bool {
		count++
		return true
	})

	var hitRate float64
	total := mc.stats.hitCount + mc.stats.missCount
	if total > 0 {
		hitRate = float64(mc.stats.hitCount) / float64(total)
	}

	slowOps := make([]SlowOp, len(mc.stats.slowOps))
	copy(slowOps, mc.stats.slowOps)

	var avgLatency time.Duration
	if len(slowOps) > 0 {
		var totalLatency time.Duration
		for _, op := range slowOps {
			totalLatency += op.Latency
		}
		avgLatency = totalLatency / time.Duration(len(slowOps))
	}

	return &Stats{
		TotalItems:    count,
		HitCount:      mc.stats.hitCount,
		MissCount:     mc.stats.missCount,
		HitRate:       hitRate,
		EvictionCount: mc.stats.evictionCount,
		AvgLatency:    avgLatency,
		SlowOps:       slowOps,
	}, nil
}

// SetOnEvict 设置驱逐回调函数
func (mc *MemoryCache) SetOnEvict(fn func(key string, value interface{})) {
	mc.onEvict = fn
}

// evict 驱逐缓存项
func (mc *MemoryCache) evict(count int) {
	// 先删除过期的项
	var expiredKeys []string
	mc.items.Range(func(key, value interface{}) bool {
		item := value.(*CacheItem)
		if item.IsExpired() {
			expiredKeys = append(expiredKeys, key.(string))
		}
		return true
	})

	for _, key := range expiredKeys {
		if val, ok := mc.items.Load(key); ok {
			item := val.(*CacheItem)
			if mc.onEvict != nil {
				mc.onEvict(key, item.Value)
			}
			mc.items.Delete(key)
			mc.stats.evictionCount++
		}
	}

	// 如果还需要驱逐更多,删除最少使用的
	if count > len(expiredKeys) {
		type keyValue struct {
			key       string
			hitCount  int64
			createdAt time.Time
		}
		var items []keyValue

		mc.items.Range(func(key, value interface{}) bool {
			item := value.(*CacheItem)
			items = append(items, keyValue{
				key:       key.(string),
				hitCount:  item.HitCount,
				createdAt: item.CreatedAt,
			})
			return true
		})

		// 按命中次数和创建时间排序 (最少使用 + FIFO)
		for i := 0; i < len(items)-1; i++ {
			for j := i + 1; j < len(items); j++ {
				if items[i].hitCount > items[j].hitCount ||
					(items[i].hitCount == items[j].hitCount && items[i].createdAt.After(items[j].createdAt)) {
					items[i], items[j] = items[j], items[i]
				}
			}
		}

		// 删除最少使用的项
		remaining := count - len(expiredKeys)
		for i := 0; i < remaining && i < len(items); i++ {
			if val, ok := mc.items.Load(items[i].key); ok {
				item := val.(*CacheItem)
				if mc.onEvict != nil {
					mc.onEvict(items[i].key, item.Value)
				}
				mc.items.Delete(items[i].key)
				mc.stats.evictionCount++
			}
		}
	}
}

// cleanupExpired 定期清理过期项
func (mc *MemoryCache) cleanupExpired() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		var keysToDelete []string
		mc.items.Range(func(key, value interface{}) bool {
			item := value.(*CacheItem)
			if item.IsExpired() {
				keysToDelete = append(keysToDelete, key.(string))
			}
			return true
		})

		for _, key := range keysToDelete {
			if val, ok := mc.items.Load(key); ok {
				item := val.(*CacheItem)
				if mc.onEvict != nil {
					mc.onEvict(key, item.Value)
				}
				mc.items.Delete(key)
				mc.stats.evictionCount++
			}
		}
	}
}

// recordHit 记录命中
func (mc *MemoryCache) recordHit() {
	mc.stats.mu.Lock()
	mc.stats.hitCount++
	mc.stats.mu.Unlock()
}

// recordMiss 记录未命中
func (mc *MemoryCache) recordMiss() {
	mc.stats.mu.Lock()
	mc.stats.missCount++
	mc.stats.mu.Unlock()
}

// recordSlowOp 记录慢操作
func (mc *MemoryCache) recordSlowOp(operation, key string, latency time.Duration) {
	if latency >= mc.stats.slowThreshold {
		mc.stats.mu.Lock()
		mc.stats.slowOps = append(mc.stats.slowOps, SlowOp{
			Operation: operation,
			Key:       key,
			Latency:   latency,
			Timestamp: time.Now(),
		})
		// 只保留最近10条慢操作记录
		if len(mc.stats.slowOps) > 10 {
			mc.stats.slowOps = mc.stats.slowOps[len(mc.stats.slowOps)-10:]
		}
		mc.stats.mu.Unlock()
	}
}

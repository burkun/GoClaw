package cache

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MultiLevelCache 多级缓存实现
type MultiLevelCache struct {
	l1         Cache // 内存缓存 (快速)
	l2         Cache // 文件缓存 (持久化)
	stats      *multiLevelStats
	writeLevel int // 写入级别: 1=只写L1, 2=写L1和L2
	mu         sync.RWMutex
}

type multiLevelStats struct {
	l1HitCount int64
	l2HitCount int64
	missCount  int64
	mu         sync.RWMutex
}

// NewMultiLevelCache 创建多级缓存
func NewMultiLevelCache(l1 Cache, l2 Cache, writeLevel int) *MultiLevelCache {
	if writeLevel < 1 || writeLevel > 2 {
		writeLevel = 2 // 默认写入L1和L2
	}

	return &MultiLevelCache{
		l1:         l1,
		l2:         l2,
		writeLevel: writeLevel,
		stats:      &multiLevelStats{},
	}
}

// Get 获取缓存值 (先查L1,再查L2)
func (mlc *MultiLevelCache) Get(ctx context.Context, key string) (interface{}, error) {
	// 先从L1获取
	val, err := mlc.l1.Get(ctx, key)
	if err == nil {
		mlc.stats.mu.Lock()
		mlc.stats.l1HitCount++
		mlc.stats.mu.Unlock()
		return val, nil
	}

	// L1未命中或过期,尝试从L2获取
	if mlc.l2 != nil {
		val, err = mlc.l2.Get(ctx, key)
		if err == nil {
			// 将L2的数据回填到L1
			// 需要从L2重新获取带TTL的CacheItem
			// 这里简化处理,假设回填时使用默认TTL
			_ = mlc.l1.Set(ctx, key, val, 5*time.Minute)

			mlc.stats.mu.Lock()
			mlc.stats.l2HitCount++
			mlc.stats.mu.Unlock()
			return val, nil
		}
	}

	// 两级缓存都未命中
	mlc.stats.mu.Lock()
	mlc.stats.missCount++
	mlc.stats.mu.Unlock()

	return nil, ErrNotFound
}

// Set 设置缓存值
func (mlc *MultiLevelCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	mlc.mu.Lock()
	defer mlc.mu.Unlock()

	// 写入L1
	if err := mlc.l1.Set(ctx, key, value, ttl); err != nil {
		return fmt.Errorf("failed to set L1 cache: %w", err)
	}

	// 根据配置决定是否写入L2
	if mlc.writeLevel >= 2 && mlc.l2 != nil {
		if err := mlc.l2.Set(ctx, key, value, ttl); err != nil {
			// L2写入失败不影响整体操作,只记录日志
			// 可以考虑添加日志记录
			_ = err
		}
	}

	return nil
}

// Delete 删除缓存值
func (mlc *MultiLevelCache) Delete(ctx context.Context, key string) error {
	mlc.mu.Lock()
	defer mlc.mu.Unlock()

	// 从L1删除
	if err := mlc.l1.Delete(ctx, key); err != nil && err != ErrNotFound {
		return err
	}

	// 从L2删除
	if mlc.l2 != nil {
		if err := mlc.l2.Delete(ctx, key); err != nil && err != ErrNotFound {
			return err
		}
	}

	return nil
}

// Exists 检查缓存是否存在
func (mlc *MultiLevelCache) Exists(ctx context.Context, key string) (bool, error) {
	// 先检查L1
	exists, err := mlc.l1.Exists(ctx, key)
	if err != nil {
		return false, err
	}
	if exists {
		return true, nil
	}

	// 再检查L2
	if mlc.l2 != nil {
		return mlc.l2.Exists(ctx, key)
	}

	return false, nil
}

// Clear 清空所有缓存
func (mlc *MultiLevelCache) Clear(ctx context.Context) error {
	mlc.mu.Lock()
	defer mlc.mu.Unlock()

	// 清空L1
	if err := mlc.l1.Clear(ctx); err != nil {
		return err
	}

	// 清空L2
	if mlc.l2 != nil {
		if err := mlc.l2.Clear(ctx); err != nil {
			return err
		}
	}

	// 重置统计
	mlc.stats.mu.Lock()
	mlc.stats.l1HitCount = 0
	mlc.stats.l2HitCount = 0
	mlc.stats.missCount = 0
	mlc.stats.mu.Unlock()

	return nil
}

// Stats 获取缓存统计信息
func (mlc *MultiLevelCache) Stats(ctx context.Context) (*Stats, error) {
	mlc.stats.mu.RLock()
	l1HitCount := mlc.stats.l1HitCount
	l2HitCount := mlc.stats.l2HitCount
	missCount := mlc.stats.missCount
	mlc.stats.mu.RUnlock()

	// 获取各级缓存的统计
	l1Stats, err := mlc.l1.Stats(ctx)
	if err != nil {
		return nil, err
	}

	var l2Stats *Stats
	if mlc.l2 != nil {
		l2Stats, err = mlc.l2.Stats(ctx)
		if err != nil {
			l2Stats = &Stats{}
		}
	} else {
		l2Stats = &Stats{}
	}

	// 计算总体命中率
	var hitRate float64
	total := l1HitCount + l2HitCount + missCount
	if total > 0 {
		hitRate = float64(l1HitCount+l2HitCount) / float64(total)
	}

	// 合并慢操作记录
	slowOps := make([]SlowOp, 0, len(l1Stats.SlowOps)+len(l2Stats.SlowOps))
	slowOps = append(slowOps, l1Stats.SlowOps...)
	slowOps = append(slowOps, l2Stats.SlowOps...)

	// 计算平均延迟
	var avgLatency time.Duration
	if len(slowOps) > 0 {
		var totalLatency time.Duration
		for _, op := range slowOps {
			totalLatency += op.Latency
		}
		avgLatency = totalLatency / time.Duration(len(slowOps))
	}

	return &Stats{
		TotalItems:    l1Stats.TotalItems + l2Stats.TotalItems,
		HitCount:      l1HitCount + l2HitCount,
		MissCount:     missCount,
		HitRate:       hitRate,
		TotalSize:     l1Stats.TotalSize + l2Stats.TotalSize,
		EvictionCount: l1Stats.EvictionCount + l2Stats.EvictionCount,
		AvgLatency:    avgLatency,
		SlowOps:       slowOps,
	}, nil
}

// GetL1Stats 获取L1缓存统计
func (mlc *MultiLevelCache) GetL1Stats(ctx context.Context) (*Stats, error) {
	return mlc.l1.Stats(ctx)
}

// GetL2Stats 获取L2缓存统计
func (mlc *MultiLevelCache) GetL2Stats(ctx context.Context) (*Stats, error) {
	if mlc.l2 == nil {
		return &Stats{}, nil
	}
	return mlc.l2.Stats(ctx)
}

// Refresh 刷新缓存 (从L2重新加载到L1)
func (mlc *MultiLevelCache) Refresh(ctx context.Context, key string) error {
	if mlc.l2 == nil {
		return fmt.Errorf("L2 cache not available")
	}

	// 从L2获取
	val, err := mlc.l2.Get(ctx, key)
	if err != nil {
		return err
	}

	// 更新到L1
	return mlc.l1.Set(ctx, key, val, 5*time.Minute)
}

package cache

import (
	"context"
	"time"
)

// Cache 定义统一的缓存接口
type Cache interface {
	// Get 获取缓存值
	Get(ctx context.Context, key string) (interface{}, error)

	// Set 设置缓存值
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error

	// Delete 删除缓存值
	Delete(ctx context.Context, key string) error

	// Exists 检查缓存是否存在
	Exists(ctx context.Context, key string) (bool, error)

	// Clear 清空所有缓存
	Clear(ctx context.Context) error

	// Stats 获取缓存统计信息
	Stats(ctx context.Context) (*Stats, error)
}

// CacheItem 缓存项
type CacheItem struct {
	Value     interface{} `json:"value"`
	ExpireAt  time.Time   `json:"expire_at"`
	CreatedAt time.Time   `json:"created_at"`
	HitCount  int64       `json:"hit_count"`
}

// IsExpired 检查缓存项是否过期
func (item *CacheItem) IsExpired() bool {
	if item.ExpireAt.IsZero() {
		return false // 永不过期
	}
	return time.Now().After(item.ExpireAt)
}

// Stats 缓存统计信息
type Stats struct {
	TotalItems    int64         `json:"total_items"`     // 总缓存项数
	HitCount      int64         `json:"hit_count"`       // 命中次数
	MissCount     int64         `json:"miss_count"`      // 未命中次数
	HitRate       float64       `json:"hit_rate"`        // 命中率
	TotalSize     int64         `json:"total_size"`      // 总大小(字节)
	EvictionCount int64         `json:"eviction_count"`  // 驱逐次数
	AvgLatency    time.Duration `json:"avg_latency"`     // 平均延迟
	SlowOps       []SlowOp      `json:"slow_ops"`        // 慢操作记录
}

// SlowOp 慢操作记录
type SlowOp struct {
	Operation string        `json:"operation"` // 操作类型: get/set/delete
	Key       string        `json:"key"`
	Latency   time.Duration `json:"latency"`
	Timestamp time.Time     `json:"timestamp"`
}

// Errors 定义缓存相关的错误
var (
	ErrNotFound      = &CacheError{Code: "NOT_FOUND", Message: "cache item not found"}
	ErrExpired       = &CacheError{Code: "EXPIRED", Message: "cache item expired"}
	ErrInvalidKey    = &CacheError{Code: "INVALID_KEY", Message: "invalid cache key"}
	ErrInvalidValue  = &CacheError{Code: "INVALID_VALUE", Message: "invalid cache value"}
	ErrCacheFull     = &CacheError{Code: "CACHE_FULL", Message: "cache is full"}
	ErrMarshalFailed = &CacheError{Code: "MARSHAL_FAILED", Message: "failed to marshal cache item"}
)

// CacheError 缓存错误类型
type CacheError struct {
	Code    string
	Message string
}

func (e *CacheError) Error() string {
	return e.Message
}

// Is 实现errors.Is接口
func (e *CacheError) Is(target error) bool {
	t, ok := target.(*CacheError)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

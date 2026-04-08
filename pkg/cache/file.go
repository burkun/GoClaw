package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FileCache 文件缓存实现 (L2)
type FileCache struct {
	baseDir   string
	stats     *fileStats
	mu        sync.RWMutex
	maxSize   int64 // 最大缓存大小(字节)
	onEvict   func(key string, value interface{})
}

type fileStats struct {
	hitCount      int64
	missCount     int64
	evictionCount int64
	slowOps       []SlowOp
	slowThreshold time.Duration
	mu            sync.RWMutex
}

// NewFileCache 创建文件缓存
func NewFileCache(baseDir string, maxSize int64) (*FileCache, error) {
	if baseDir == "" {
		baseDir = filepath.Join(os.TempDir(), "goclaw-cache")
	}

	if maxSize <= 0 {
		maxSize = 100 * 1024 * 1024 // 默认100MB
	}

	// 确保目录存在
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	fc := &FileCache{
		baseDir: baseDir,
		maxSize: maxSize,
		stats: &fileStats{
			slowOps:       make([]SlowOp, 0, 10),
			slowThreshold: 200 * time.Millisecond, // 文件缓存慢操作阈值稍高
		},
	}

	// 启动后台清理goroutine
	go fc.cleanupExpired()

	return fc, nil
}

// Get 获取缓存值
func (fc *FileCache) Get(ctx context.Context, key string) (interface{}, error) {
	start := time.Now()

	filename := fc.getFilename(key)
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			fc.recordMiss()
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}

	var item CacheItem
	if err := json.Unmarshal(data, &item); err != nil {
		_ = os.Remove(filename)
		fc.recordMiss()
		return nil, fmt.Errorf("failed to unmarshal cache item: %w", err)
	}

	// 检查是否过期
	if item.IsExpired() {
		_ = os.Remove(filename)
		fc.recordMiss()
		return nil, ErrExpired
	}

	item.HitCount++
	// 更新文件
	if data, err := json.Marshal(&item); err == nil {
		_ = os.WriteFile(filename, data, 0644)
	}

	fc.recordHit()
	fc.recordSlowOp("get", key, time.Since(start))

	return item.Value, nil
}

// Set 设置缓存值
func (fc *FileCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	start := time.Now()

	fc.mu.Lock()
	defer fc.mu.Unlock()

	// 检查是否需要驱逐
	if err := fc.checkAndEvict(); err != nil {
		return err
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

	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("failed to marshal cache item: %w", err)
	}

	filename := fc.getFilename(key)
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	fc.recordSlowOp("set", key, time.Since(start))

	return nil
}

// Delete 删除缓存值
func (fc *FileCache) Delete(ctx context.Context, key string) error {
	start := time.Now()

	filename := fc.getFilename(key)
	if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete cache file: %w", err)
	}

	fc.recordSlowOp("delete", key, time.Since(start))

	return nil
}

// Exists 检查缓存是否存在
func (fc *FileCache) Exists(ctx context.Context, key string) (bool, error) {
	filename := fc.getFilename(key)
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	var item CacheItem
	if err := json.Unmarshal(data, &item); err != nil {
		return false, nil
	}

	return !item.IsExpired(), nil
}

// Clear 清空所有缓存
func (fc *FileCache) Clear(ctx context.Context) error {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	entries, err := os.ReadDir(fc.baseDir)
	if err != nil {
		return fmt.Errorf("failed to read cache directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".cache") {
			_ = os.Remove(filepath.Join(fc.baseDir, entry.Name()))
		}
	}

	fc.stats.mu.Lock()
	fc.stats.hitCount = 0
	fc.stats.missCount = 0
	fc.stats.evictionCount = 0
	fc.stats.slowOps = make([]SlowOp, 0, 10)
	fc.stats.mu.Unlock()

	return nil
}

// Stats 获取缓存统计信息
func (fc *FileCache) Stats(ctx context.Context) (*Stats, error) {
	fc.stats.mu.RLock()
	defer fc.stats.mu.RUnlock()

	count := int64(0)
	var totalSize int64

	entries, err := os.ReadDir(fc.baseDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".cache") {
				count++
				if info, err := entry.Info(); err == nil {
					totalSize += info.Size()
				}
			}
		}
	}

	var hitRate float64
	total := fc.stats.hitCount + fc.stats.missCount
	if total > 0 {
		hitRate = float64(fc.stats.hitCount) / float64(total)
	}

	slowOps := make([]SlowOp, len(fc.stats.slowOps))
	copy(slowOps, fc.stats.slowOps)

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
		HitCount:      fc.stats.hitCount,
		MissCount:     fc.stats.missCount,
		HitRate:       hitRate,
		TotalSize:     totalSize,
		EvictionCount: fc.stats.evictionCount,
		AvgLatency:    avgLatency,
		SlowOps:       slowOps,
	}, nil
}

// SetOnEvict 设置驱逐回调函数
func (fc *FileCache) SetOnEvict(fn func(key string, value interface{})) {
	fc.onEvict = fn
}

// getFilename 获取缓存文件名
func (fc *FileCache) getFilename(key string) string {
	// 使用key的hash作为文件名,避免特殊字符问题
	hash := fmt.Sprintf("%x", key)
	return filepath.Join(fc.baseDir, hash+".cache")
}

// checkAndEvict 检查并驱逐缓存
func (fc *FileCache) checkAndEvict() error {
	// 获取当前缓存大小
	var currentSize int64
	entries, err := os.ReadDir(fc.baseDir)
	if err != nil {
		return nil
	}

	type fileInfo struct {
		key       string
		size      int64
		hitCount  int64
		createdAt time.Time
	}
	var files []fileInfo

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".cache") {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			currentSize += info.Size()

			// 读取文件内容获取元数据
			filename := filepath.Join(fc.baseDir, entry.Name())
			if data, err := os.ReadFile(filename); err == nil {
				var item CacheItem
				if err := json.Unmarshal(data, &item); err == nil {
					files = append(files, fileInfo{
						key:       strings.TrimSuffix(entry.Name(), ".cache"),
						size:      info.Size(),
						hitCount:  item.HitCount,
						createdAt: item.CreatedAt,
					})
				}
			}
		}
	}

	// 如果超过最大大小,驱逐一部分
	if currentSize > fc.maxSize {
		// 删除过期的文件
		for _, file := range files {
			filename := fc.getFilename(file.key)
			data, err := os.ReadFile(filename)
			if err != nil {
				continue
			}

			var item CacheItem
			if err := json.Unmarshal(data, &item); err != nil {
				continue
			}

			if item.IsExpired() {
				if fc.onEvict != nil {
					fc.onEvict(file.key, item.Value)
				}
				_ = os.Remove(filename)
				currentSize -= file.size
				fc.stats.evictionCount++
			}
		}

		// 如果还是太大,删除最少使用的
		if currentSize > fc.maxSize {
			// 按命中次数和创建时间排序
			for i := 0; i < len(files)-1; i++ {
				for j := i + 1; j < len(files); j++ {
					if files[i].hitCount > files[j].hitCount ||
						(files[i].hitCount == files[j].hitCount && files[i].createdAt.After(files[j].createdAt)) {
						files[i], files[j] = files[j], files[i]
					}
				}
			}

			// 删除文件直到大小满足要求
			for _, file := range files {
				filename := fc.getFilename(file.key)
				data, err := os.ReadFile(filename)
				if err != nil {
					continue
				}

				var item CacheItem
				if err := json.Unmarshal(data, &item); err != nil {
					continue
				}

				if fc.onEvict != nil {
					fc.onEvict(file.key, item.Value)
				}
				_ = os.Remove(filename)
				currentSize -= file.size
				fc.stats.evictionCount++

				if currentSize <= fc.maxSize*9/10 { // 驱逐到90%容量
					break
				}
			}
		}
	}

	return nil
}

// cleanupExpired 定期清理过期项
func (fc *FileCache) cleanupExpired() {
	ticker := time.NewTicker(5 * time.Minute) // 文件缓存清理间隔更长
	defer ticker.Stop()

	for range ticker.C {
		entries, err := os.ReadDir(fc.baseDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".cache") {
				filename := filepath.Join(fc.baseDir, entry.Name())
				data, err := os.ReadFile(filename)
				if err != nil {
					continue
				}

				var item CacheItem
				if err := json.Unmarshal(data, &item); err != nil {
					_ = os.Remove(filename)
					continue
				}

				if item.IsExpired() {
					if fc.onEvict != nil {
						fc.onEvict(strings.TrimSuffix(entry.Name(), ".cache"), item.Value)
					}
					_ = os.Remove(filename)
					fc.stats.evictionCount++
				}
			}
		}
	}
}

// recordHit 记录命中
func (fc *FileCache) recordHit() {
	fc.stats.mu.Lock()
	fc.stats.hitCount++
	fc.stats.mu.Unlock()
}

// recordMiss 记录未命中
func (fc *FileCache) recordMiss() {
	fc.stats.mu.Lock()
	fc.stats.missCount++
	fc.stats.mu.Unlock()
}

// recordSlowOp 记录慢操作
func (fc *FileCache) recordSlowOp(operation, key string, latency time.Duration) {
	if latency >= fc.stats.slowThreshold {
		fc.stats.mu.Lock()
		fc.stats.slowOps = append(fc.stats.slowOps, SlowOp{
			Operation: operation,
			Key:       key,
			Latency:   latency,
			Timestamp: time.Now(),
		})
		// 只保留最近10条慢操作记录
		if len(fc.stats.slowOps) > 10 {
			fc.stats.slowOps = fc.stats.slowOps[len(fc.stats.slowOps)-10:]
		}
		fc.stats.mu.Unlock()
	}
}

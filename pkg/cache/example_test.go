package cache

import (
	"context"
	"fmt"
	"time"
)

// Example_basicUsage 基本使用示例
func Example_basicUsage() {
	// 创建内存缓存
	memCache := NewMemoryCache(1000)
	ctx := context.Background()

	// 设置缓存
	_ = memCache.Set(ctx, "user:123", map[string]string{
		"name":  "Alice",
		"email": "alice@example.com",
	}, 5*time.Minute)

	// 获取缓存
	val, err := memCache.Get(ctx, "user:123")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("User: %v\n", val)

	// 查看统计信息
	stats, _ := memCache.Stats(ctx)
	fmt.Printf("Hit rate: %.2f%%\n", stats.HitRate*100)
}

// Example_multiLevel 多级缓存示例
func Example_multiLevel() {
	// 创建L1内存缓存(快速)
	l1 := NewMemoryCache(1000)

	// 创建L2文件缓存(持久化)
	l2, err := NewFileCache("/tmp/goclaw-cache", 100*1024*1024) // 100MB
	if err != nil {
		fmt.Printf("Error creating file cache: %v\n", err)
		return
	}

	// 创建多级缓存
	multiCache := NewMultiLevelCache(l1, l2, 2) // 写入L1和L2

	ctx := context.Background()

	// 设置缓存(会同时写入L1和L2)
	_ = multiCache.Set(ctx, "config:app", map[string]interface{}{
		"debug": true,
		"port":  8080,
	}, 10*time.Minute)

	// 获取缓存(先查L1,未命中则查L2)
	val, err := multiCache.Get(ctx, "config:app")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Config: %v\n", val)
}

// Example_monitoring 缓存监控示例
func Example_monitoring() {
	cache := NewMemoryCache(1000)

	// 创建监控器(每30秒收集一次指标)
	monitor := NewCacheMonitor(cache, "/var/log/goclaw/cache-metrics.json", 30*time.Second)
	defer monitor.Stop()

	// 使用缓存...
	ctx := context.Background()
	cache.Set(ctx, "key1", "value1", 5*time.Minute)
	cache.Get(ctx, "key1")

	// 获取最新快照
	snapshot := monitor.GetLatestSnapshot()
	if snapshot != nil {
		fmt.Printf("Current hit rate: %.2f%%\n", snapshot.HitRate*100)
	}

	// 获取聚合统计
	aggStats := monitor.GetAggregatedStats()
	fmt.Printf("Average hit rate: %.2f%%\n", aggStats.AverageHitRate*100)
}

// Example_alerting 缓存告警示例
func Example_alerting() {
	cache := NewMemoryCache(1000)

	// 创建告警器
	alerter := NewCacheAlerter(cache)

	// 添加自定义告警条件
	alerter.AddCondition(
		"very_low_hit_rate",
		func(stats *Stats) bool {
			return stats.HitRate < 0.3 // 命中率低于30%
		},
		"Cache hit rate is critically low (<30%)",
	)

	alerter.AddCondition(
		"too_many_items",
		func(stats *Stats) bool {
			return stats.TotalItems > 800 // 接近容量上限
		},
		"Cache is approaching capacity limit",
	)

	// 启动告警检查(每分钟检查一次)
	go alerter.Start(1 * time.Minute)
	defer alerter.Stop()

	// 处理告警
	go func() {
		for alert := range alerter.Alerts() {
			fmt.Printf("[%s] ALERT: %s\n", alert.Timestamp.Format(time.RFC3339), alert.Message)
			// 可以发送到监控系统、日志系统等
		}
	}()

	// 使用缓存...
}

// Example_mcpCache MCP配置缓存使用示例
/*
func Example_mcpCache() {
	// 创建MCP缓存管理器
	mgr := NewMCPCacheManager(MCPCacheConfig{
		MaxItems:    1000,
		DefaultTTL:  5 * time.Minute,
		AutoRefresh: true,
	})
	defer mgr.Stop()

	ctx := context.Background()

	// 检查配置是否改变
	serverName := "my-mcp-server"
	cfg := config.MCPServerConfig{
		Enabled: true,
		Type:    "stdio",
		Command: "/usr/bin/mcp-server",
	}

	changed, err := mgr.HasServerConfigChanged(ctx, serverName, cfg)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if changed {
		fmt.Println("Configuration has changed, updating...")
		// 重新加载配置...
	}

	// 查看缓存统计
	stats, _ := mgr.GetCacheStats(ctx)
	fmt.Printf("Cache hit rate: %.2f%%\n", stats.HitRate*100)
}
*/

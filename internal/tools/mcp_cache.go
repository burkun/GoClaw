package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"goclaw/internal/config"
	"goclaw/pkg/cache"
)

// MCPCacheManager MCP配置缓存管理器
type MCPCacheManager struct {
	cache       cache.Cache
	defaultTTL  time.Duration
	autoRefresh bool
	refreshChan chan string
	stopChan    chan struct{}
}

// MCPCacheConfig 缓存配置
type MCPCacheConfig struct {
	MaxItems    int           // 最大缓存项数
	DefaultTTL  time.Duration // 默认TTL
	AutoRefresh bool          // 是否自动刷新即将过期的缓存
}

// NewMCPCacheManager 创建MCP缓存管理器
func NewMCPCacheManager(cfg MCPCacheConfig) *MCPCacheManager {
	if cfg.MaxItems <= 0 {
		cfg.MaxItems = 1000
	}
	if cfg.DefaultTTL <= 0 {
		cfg.DefaultTTL = 5 * time.Minute
	}

	// 创建多级缓存
	memoryCache := cache.NewMemoryCache(cfg.MaxItems)
	fileCache, err := cache.NewFileCache("", 50*1024*1024) // 50MB
	if err != nil {
		// 如果文件缓存创建失败,只使用内存缓存
		return &MCPCacheManager{
			cache:       memoryCache,
			defaultTTL:  cfg.DefaultTTL,
			autoRefresh: cfg.AutoRefresh,
		}
	}

	multiCache := cache.NewMultiLevelCache(memoryCache, fileCache, 2)

	mgr := &MCPCacheManager{
		cache:       multiCache,
		defaultTTL:  cfg.DefaultTTL,
		autoRefresh: cfg.AutoRefresh,
		refreshChan: make(chan string, 100),
		stopChan:    make(chan struct{}),
	}

	// 启动自动刷新goroutine
	if cfg.AutoRefresh {
		go mgr.autoRefreshWorker()
	}

	return mgr
}

// mcpConfigSignature 生成配置签名
func mcpConfigSignature(cfg config.MCPServerConfig) string {
	b, _ := json.Marshal(cfg)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// mcpConfigCacheKey 生成缓存key
func mcpConfigCacheKey(serverName string) string {
	return fmt.Sprintf("mcp:config:%s", serverName)
}

// mcpToolConfigCacheKey 生成工具配置缓存key
func mcpToolConfigCacheKey(serverName, toolName string) string {
	return fmt.Sprintf("mcp:tool:%s:%s", serverName, toolName)
}

// SetServerConfig 缓存MCP服务器配置
func (m *MCPCacheManager) SetServerConfig(ctx context.Context, serverName string, cfg config.MCPServerConfig) error {
	key := mcpConfigCacheKey(serverName)
	sig := mcpConfigSignature(cfg)
	return m.cache.Set(ctx, key, sig, m.defaultTTL)
}

// GetServerConfigSignature 获取服务器配置签名
func (m *MCPCacheManager) GetServerConfigSignature(ctx context.Context, serverName string) (string, error) {
	key := mcpConfigCacheKey(serverName)
	val, err := m.cache.Get(ctx, key)
	if err != nil {
		return "", err
	}
	sig, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("invalid cache value type")
	}
	return sig, nil
}

// HasServerConfigChanged 检查配置是否改变
func (m *MCPCacheManager) HasServerConfigChanged(ctx context.Context, serverName string, cfg config.MCPServerConfig) (bool, error) {
	currentSig := mcpConfigSignature(cfg)
	cachedSig, err := m.GetServerConfigSignature(ctx, serverName)
	if err != nil {
		// 缓存未命中,视为改变
		_ = m.SetServerConfig(ctx, serverName, cfg)
		return true, nil
	}

	changed := cachedSig != currentSig
	if changed {
		// 更新缓存
		_ = m.SetServerConfig(ctx, serverName, cfg)
	}

	return changed, nil
}

// SetToolConfig 缓存单个工具配置
func (m *MCPCacheManager) SetToolConfig(ctx context.Context, serverName, toolName string, toolConfig interface{}) error {
	key := mcpToolConfigCacheKey(serverName, toolName)
	return m.cache.Set(ctx, key, toolConfig, m.defaultTTL)
}

// GetToolConfig 获取工具配置
func (m *MCPCacheManager) GetToolConfig(ctx context.Context, serverName, toolName string) (interface{}, error) {
	key := mcpToolConfigCacheKey(serverName, toolName)
	return m.cache.Get(ctx, key)
}

// InvalidateServerConfig 使服务器配置缓存失效
func (m *MCPCacheManager) InvalidateServerConfig(ctx context.Context, serverName string) error {
	key := mcpConfigCacheKey(serverName)
	return m.cache.Delete(ctx, key)
}

// InvalidateAll 使所有缓存失效
func (m *MCPCacheManager) InvalidateAll(ctx context.Context) error {
	return m.cache.Clear(ctx)
}

// GetCacheStats 获取缓存统计信息
func (m *MCPCacheManager) GetCacheStats(ctx context.Context) (*cache.Stats, error) {
	return m.cache.Stats(ctx)
}

// Refresh 刷新指定服务器的缓存
func (m *MCPCacheManager) Refresh(ctx context.Context, serverName string) error {
	// 删除旧缓存
	key := mcpConfigCacheKey(serverName)
	if err := m.cache.Delete(ctx, key); err != nil && err != cache.ErrNotFound {
		return err
	}

	// 通知刷新(如果有订阅者)
	select {
	case m.refreshChan <- serverName:
	default:
		// channel已满,忽略
	}

	return nil
}

// autoRefreshWorker 自动刷新worker
func (m *MCPCacheManager) autoRefreshWorker() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case serverName := <-m.refreshChan:
			// 处理刷新请求
			ctx := context.Background()
			_ = m.cache.Delete(ctx, mcpConfigCacheKey(serverName))
		case <-ticker.C:
			// 定期检查并刷新即将过期的缓存
			// 这里可以添加更智能的预刷新逻辑
		}
	}
}

// Stop 停止缓存管理器
func (m *MCPCacheManager) Stop() {
	close(m.stopChan)
}

// ---------------------------------------------------------------------------
// 向后兼容的全局函数
// ---------------------------------------------------------------------------

var defaultMCPCacheManager = NewMCPCacheManager(MCPCacheConfig{
	MaxItems:    1000,
	DefaultTTL:  5 * time.Minute,
	AutoRefresh: true,
})

// hasMCPConfigChanged 记录最新签名并返回是否改变(向后兼容)
func hasMCPConfigChanged(serverName string, cfg config.MCPServerConfig) bool {
	ctx := context.Background()
	changed, _ := defaultMCPCacheManager.HasServerConfigChanged(ctx, serverName, cfg)
	return changed
}

// InvalidateMCPConfigCache 清除所有缓存签名(向后兼容)
func InvalidateMCPConfigCache() {
	ctx := context.Background()
	_ = defaultMCPCacheManager.InvalidateAll(ctx)
}

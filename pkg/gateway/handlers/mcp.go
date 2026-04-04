// MCP handler exposes GET /api/mcp/config and PUT /api/mcp/config for
// reading and updating the MCP servers configuration.
package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/bookerbai/goclaw/internal/config"
	"github.com/bookerbai/goclaw/internal/tools"
)

// MCPHandler handles MCP configuration routes.
type MCPHandler struct {
	cfg *config.AppConfig

	mu          sync.RWMutex
	cachedMod   int64
	cachedValue *config.ExtensionsConfig
}

// NewMCPHandler creates an MCPHandler.
func NewMCPHandler(cfg *config.AppConfig) *MCPHandler {
	return &MCPHandler{cfg: cfg}
}

// GetConfig returns the current MCP config (from extensions_config.json).
func (h *MCPHandler) GetConfig(c *gin.Context) {
	extCfg, err := h.loadExtensions()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"mcp_servers": extCfg.MCPServers,
	})
}

// UpdateConfig updates the MCP servers section in extensions_config.json.
func (h *MCPHandler) UpdateConfig(c *gin.Context) {
	var req struct {
		MCPServers map[string]config.MCPServerConfig `json:"mcp_servers"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	extCfg, err := h.loadExtensions()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	extCfg.MCPServers = req.MCPServers
	if err := h.saveExtensions(extCfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.mu.Lock()
	h.cachedValue = nil
	h.mu.Unlock()
	if h.cfg != nil {
		h.cfg.Extensions.MCPServers = req.MCPServers
	}
	tools.InvalidateMCPConfigCache()

	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

func (h *MCPHandler) loadExtensions() (*config.ExtensionsConfig, error) {
	path := config.DefaultExtensionsConfigPath
	if h.cfg != nil && h.cfg.ExtensionsRef.ConfigPath != "" {
		path = h.cfg.ExtensionsRef.ConfigPath
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &config.ExtensionsConfig{MCPServers: make(map[string]config.MCPServerConfig)}, nil
		}
		return nil, err
	}

	h.mu.RLock()
	if h.cachedValue != nil && info.ModTime().UnixNano() == h.cachedMod {
		v := h.cachedValue
		h.mu.RUnlock()
		return v, nil
	}
	h.mu.RUnlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var ext config.ExtensionsConfig
	if err := json.Unmarshal(data, &ext); err != nil {
		return nil, err
	}

	h.mu.Lock()
	h.cachedValue = &ext
	h.cachedMod = info.ModTime().UnixNano()
	h.mu.Unlock()

	return &ext, nil
}

func (h *MCPHandler) saveExtensions(ext *config.ExtensionsConfig) error {
	path := config.DefaultExtensionsConfigPath
	if h.cfg != nil && h.cfg.ExtensionsRef.ConfigPath != "" {
		path = h.cfg.ExtensionsRef.ConfigPath
	}

	data, err := json.MarshalIndent(ext, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

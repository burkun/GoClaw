package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"

	"github.com/bookerbai/goclaw/internal/config"
)

type mcpConfigCache struct {
	mu   sync.Mutex
	sigs map[string]string
}

var globalMCPConfigCache = &mcpConfigCache{sigs: map[string]string{}}

func mcpConfigSignature(cfg config.MCPServerConfig) string {
	b, _ := json.Marshal(cfg)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// hasMCPConfigChanged records the latest signature and returns true if changed.
func hasMCPConfigChanged(serverName string, cfg config.MCPServerConfig) bool {
	sig := mcpConfigSignature(cfg)
	globalMCPConfigCache.mu.Lock()
	defer globalMCPConfigCache.mu.Unlock()
	prev, ok := globalMCPConfigCache.sigs[serverName]
	globalMCPConfigCache.sigs[serverName] = sig
	return !ok || prev != sig
}

// InvalidateMCPConfigCache clears cached signatures for hot-reload scenarios.
func InvalidateMCPConfigCache() {
	globalMCPConfigCache.mu.Lock()
	defer globalMCPConfigCache.mu.Unlock()
	globalMCPConfigCache.sigs = map[string]string{}
}

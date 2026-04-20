package tools

import (
	"testing"

	"goclaw/internal/config"
)

func TestHasMCPConfigChanged(t *testing.T) {
	InvalidateMCPConfigCache()

	cfg := config.MCPServerConfig{Enabled: true, Type: "stdio", Command: "demo"}
	if !hasMCPConfigChanged("srv", cfg) {
		t.Fatalf("first call should be treated as changed")
	}
	if hasMCPConfigChanged("srv", cfg) {
		t.Fatalf("same config should not be changed")
	}
	cfg.Command = "demo2"
	if !hasMCPConfigChanged("srv", cfg) {
		t.Fatalf("updated config should be changed")
	}
}

func TestStdioClientPoolInvalidateByServerName(t *testing.T) {
	pool := &stdioClientPool{clients: map[string]*pooledStdioClient{}}

	cfg1 := config.MCPServerConfig{Command: "cmd-a"}
	cfg2 := config.MCPServerConfig{Command: "cmd-b"}
	cfg3 := config.MCPServerConfig{Command: "cmd-c"}

	keyA1 := stdioPoolKey("srv-a", cfg1)
	keyA2 := stdioPoolKey("srv-a", cfg2)
	keyB1 := stdioPoolKey("srv-b", cfg3)

	pool.clients[keyA1] = &pooledStdioClient{serverName: "srv-a"}
	pool.clients[keyA2] = &pooledStdioClient{serverName: "srv-a"}
	pool.clients[keyB1] = &pooledStdioClient{serverName: "srv-b"}

	pool.invalidate("srv-a")

	if _, ok := pool.clients[keyA1]; ok {
		t.Fatalf("expected srv-a client keyA1 to be removed")
	}
	if _, ok := pool.clients[keyA2]; ok {
		t.Fatalf("expected srv-a client keyA2 to be removed")
	}
	if _, ok := pool.clients[keyB1]; !ok {
		t.Fatalf("expected srv-b client to remain")
	}
}

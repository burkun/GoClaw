package tools

import (
	"testing"

	"github.com/bookerbai/goclaw/internal/config"
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

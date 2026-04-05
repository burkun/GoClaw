package agentconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoader_GetAgentDir(t *testing.T) {
	loader := NewLoader(".test_goclaw")
	defer os.RemoveAll(".test_goclaw")

	expected := filepath.Join(".test_goclaw", "agents", "test-agent")
	result := loader.GetAgentDir("test-agent")

	if result != expected {
		t.Errorf("GetAgentDir() = %v, want %v", result, expected)
	}
}

func TestLoader_SaveAndLoadConfig(t *testing.T) {
	loader := NewLoader(".test_goclaw")
	defer os.RemoveAll(".test_goclaw")

	// 创建测试配置
	cfg := &Config{
		Name:        "test-agent",
		Model:       "gpt-4",
		Description: "Test agent",
		Skills:      []string{"skill1", "skill2"},
		ToolGroups:  []string{"web", "bash"},
	}

	// 保存配置
	err := loader.SaveConfig("test-agent", cfg)
	if err != nil {
		t.Fatalf("SaveConfig() failed: %v", err)
	}

	// 加载配置
	loaded, err := loader.LoadConfig("test-agent")
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}

	// 验证字段
	if loaded.Name != cfg.Name {
		t.Errorf("Name = %v, want %v", loaded.Name, cfg.Name)
	}
	if loaded.Model != cfg.Model {
		t.Errorf("Model = %v, want %v", loaded.Model, cfg.Model)
	}
	if len(loaded.Skills) != len(cfg.Skills) {
		t.Errorf("Skills length = %v, want %v", len(loaded.Skills), len(cfg.Skills))
	}
	if len(loaded.ToolGroups) != len(cfg.ToolGroups) {
		t.Errorf("ToolGroups length = %v, want %v", len(loaded.ToolGroups), len(cfg.ToolGroups))
	}
}

func TestLoader_SaveAndLoadSoul(t *testing.T) {
	loader := NewLoader(".test_goclaw")
	defer os.RemoveAll(".test_goclaw")

	soul := "# Test Agent\n\nYou are a test agent."

	// 保存SOUL.md
	err := loader.SaveSoul("test-agent", soul)
	if err != nil {
		t.Fatalf("SaveSoul() failed: %v", err)
	}

	// 加载SOUL.md
	loaded, err := loader.LoadSoul("test-agent")
	if err != nil {
		t.Fatalf("LoadSoul() failed: %v", err)
	}

	if loaded != soul {
		t.Errorf("Soul = %v, want %v", loaded, soul)
	}
}

func TestLoader_AgentExists(t *testing.T) {
	loader := NewLoader(".test_goclaw")
	defer os.RemoveAll(".test_goclaw")

	// 测试不存在的agent
	if loader.AgentExists("nonexistent") {
		t.Error("AgentExists() = true for nonexistent agent")
	}

	// 创建agent
	cfg := &Config{Name: "test-agent"}
	loader.SaveConfig("test-agent", cfg)

	// 测试存在的agent
	if !loader.AgentExists("test-agent") {
		t.Error("AgentExists() = false for existing agent")
	}
}

func TestLoader_ListAgents(t *testing.T) {
	loader := NewLoader(".test_goclaw")
	defer os.RemoveAll(".test_goclaw")

	// 初始应该为空
	agents, err := loader.ListAgents()
	if err != nil {
		t.Fatalf("ListAgents() failed: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("ListAgents() = %v agents, want 0", len(agents))
	}

	// 创建多个agents
	for i := 1; i <= 3; i++ {
		cfg := &Config{Name: "test-agent"}
		loader.SaveConfig("test-agent", cfg)
	}

	// 验证列表
	agents, err = loader.ListAgents()
	if err != nil {
		t.Fatalf("ListAgents() failed: %v", err)
	}
	if len(agents) != 1 {
		t.Errorf("ListAgents() = %v agents, want 1", len(agents))
	}
}

func TestLoader_MemoryOperations(t *testing.T) {
	loader := NewLoader(".test_goclaw")
	defer os.RemoveAll(".test_goclaw")

	memory := []byte(`{"facts": ["test fact"]}`)

	// 保存memory
	err := loader.SaveMemory("test-agent", memory)
	if err != nil {
		t.Fatalf("SaveMemory() failed: %v", err)
	}

	// 加载memory
	loaded, err := loader.LoadMemory("test-agent")
	if err != nil {
		t.Fatalf("LoadMemory() failed: %v", err)
	}

	if string(loaded) != string(memory) {
		t.Errorf("Memory = %v, want %v", string(loaded), string(memory))
	}
}

func TestLoader_DefaultLoader(t *testing.T) {
	// 测试默认loader
	if DefaultLoader == nil {
		t.Error("DefaultLoader is nil")
	}
}

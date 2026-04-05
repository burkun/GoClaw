// Package agentconfig provides utilities for loading per-agent configuration.
package agentconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents per-agent configuration loaded from file system.
type Config struct {
	Name        string   `yaml:"name"`
	Model       string   `yaml:"model,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Skills      []string `yaml:"skills,omitempty"`
	ToolGroups  []string `yaml:"tool_groups,omitempty"`
}

// Loader manages per-agent configuration loading.
type Loader struct {
	baseDir string
}

// NewLoader creates a new Loader with the given base directory.
func NewLoader(baseDir string) *Loader {
	if baseDir == "" {
		baseDir = ".goclaw"
	}
	return &Loader{baseDir: baseDir}
}

// GetAgentDir returns the directory path for a specific agent.
func (l *Loader) GetAgentDir(name string) string {
	return filepath.Join(l.baseDir, "agents", strings.ToLower(name))
}

// LoadConfig loads per-agent config from file system.
func (l *Loader) LoadConfig(name string) (*Config, error) {
	configPath := filepath.Join(l.GetAgentDir(name), "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read agent config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse agent config: %w", err)
	}

	// Ensure name is set
	if cfg.Name == "" {
		cfg.Name = strings.ToLower(name)
	}

	return &cfg, nil
}

// LoadSoul loads SOUL.md content for an agent.
func (l *Loader) LoadSoul(name string) (string, error) {
	soulPath := filepath.Join(l.GetAgentDir(name), "SOUL.md")
	data, err := os.ReadFile(soulPath)
	if err != nil {
		return "", fmt.Errorf("read agent soul: %w", err)
	}
	return string(data), nil
}

// LoadMemory loads per-agent memory from file system.
func (l *Loader) LoadMemory(name string) ([]byte, error) {
	memoryPath := filepath.Join(l.GetAgentDir(name), "memory.json")
	data, err := os.ReadFile(memoryPath)
	if err != nil {
		return nil, fmt.Errorf("read agent memory: %w", err)
	}
	return data, nil
}

// SaveConfig saves per-agent config to file system.
func (l *Loader) SaveConfig(name string, cfg *Config) error {
	agentDir := l.GetAgentDir(name)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return fmt.Errorf("create agent dir: %w", err)
	}

	configPath := filepath.Join(agentDir, "config.yaml")
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal agent config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("write agent config: %w", err)
	}
	return nil
}

// SaveSoul saves SOUL.md content for an agent.
func (l *Loader) SaveSoul(name, content string) error {
	agentDir := l.GetAgentDir(name)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return fmt.Errorf("create agent dir: %w", err)
	}

	soulPath := filepath.Join(agentDir, "SOUL.md")
	if err := os.WriteFile(soulPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write agent soul: %w", err)
	}
	return nil
}

// SaveMemory saves per-agent memory to file system.
func (l *Loader) SaveMemory(name string, data []byte) error {
	agentDir := l.GetAgentDir(name)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return fmt.Errorf("create agent dir: %w", err)
	}

	memoryPath := filepath.Join(agentDir, "memory.json")
	if err := os.WriteFile(memoryPath, data, 0644); err != nil {
		return fmt.Errorf("write agent memory: %w", err)
	}
	return nil
}

// AgentExists checks if an agent directory exists.
func (l *Loader) AgentExists(name string) bool {
	agentDir := l.GetAgentDir(name)
	_, err := os.Stat(agentDir)
	return err == nil
}

// ListAgents returns a list of all agent names by scanning the agents directory.
func (l *Loader) ListAgents() ([]string, error) {
	agentsDir := filepath.Join(l.baseDir, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("read agents dir: %w", err)
	}

	var agents []string
	for _, entry := range entries {
		if entry.IsDir() {
			// Check if config.yaml exists
			configPath := filepath.Join(agentsDir, entry.Name(), "config.yaml")
			if _, err := os.Stat(configPath); err == nil {
				agents = append(agents, entry.Name())
			}
		}
	}
	return agents, nil
}

// DefaultLoader is the default loader instance.
var DefaultLoader = NewLoader("")

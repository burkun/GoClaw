// Agents handler exposes CRUD and helper routes for custom agents.
package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"

	"goclaw/internal/config"
)

// AgentsHandler handles custom agent CRUD.
type AgentsHandler struct {
	cfg *config.AppConfig
}

// NewAgentsHandler creates an AgentsHandler.
func NewAgentsHandler(cfg *config.AppConfig) *AgentsHandler {
	return &AgentsHandler{cfg: cfg}
}

var agentNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{1,63}$`)

func isValidAgentName(name string) bool {
	return agentNamePattern.MatchString(name)
}

// ListAgents returns the list of configured agents.
func (h *AgentsHandler) ListAgents(c *gin.Context) {
	agents := []map[string]any{}

	// Load from in-memory config
	if h.cfg != nil {
		names := make([]string, 0, len(h.cfg.Agents))
		for name := range h.cfg.Agents {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			agentCfg := h.cfg.Agents[name]

			// Try to load per-agent config for additional fields
			agentConfig, err := loadAgentConfig(name)
			response := map[string]any{
				"name":        name,
				"enabled":     agentCfg.Enabled,
				"model":       agentCfg.Model,
				"description": agentCfg.Description,
			}

			if err == nil {
				// Add skills and tool_groups from per-agent config
				if skills, ok := agentConfig["skills"].([]interface{}); ok {
					response["skills"] = skills
				}
				if toolGroups, ok := agentConfig["tool_groups"].([]interface{}); ok {
					response["tool_groups"] = toolGroups
				}
			}

			agents = append(agents, response)
		}
	}

	c.JSON(http.StatusOK, gin.H{"agents": agents})
}

// GetAgent returns details for a single agent including SOUL.md content.
func (h *AgentsHandler) GetAgent(c *gin.Context) {
	name := strings.ToLower(c.Param("name"))

	// Try to load from per-agent config file first (P0 fix)
	agentConfig, err := loadAgentConfig(name)
	if err == nil {
		// Load SOUL.md
		soul, _ := loadAgentSoul(name)

		response := map[string]any{
			"name": name,
		}
		if desc, ok := agentConfig["description"].(string); ok {
			response["description"] = desc
		}
		if model, ok := agentConfig["model"].(string); ok {
			response["model"] = model
		}
		if skills, ok := agentConfig["skills"].([]interface{}); ok {
			response["skills"] = skills
		}
		if toolGroups, ok := agentConfig["tool_groups"].([]interface{}); ok {
			response["tool_groups"] = toolGroups
		}
		if soul != "" {
			response["soul"] = soul
		}

		// Check enabled status from main config
		if h.cfg != nil && h.cfg.Agents != nil {
			if agentCfg, ok := h.cfg.Agents[name]; ok {
				response["enabled"] = agentCfg.Enabled
			} else {
				response["enabled"] = true
			}
		}

		c.JSON(http.StatusOK, response)
		return
	}

	// Fallback to in-memory config
	if h.cfg == nil || h.cfg.Agents == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}
	agentCfg, ok := h.cfg.Agents[name]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	response := map[string]any{
		"name":        name,
		"enabled":     agentCfg.Enabled,
		"model":       agentCfg.Model,
		"description": agentCfg.Description,
	}
	c.JSON(http.StatusOK, response)
}

// CreateAgent creates a new custom agent with per-agent config files.
func (h *AgentsHandler) CreateAgent(c *gin.Context) {
	if h.cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "config unavailable"})
		return
	}
	if h.cfg.Agents == nil {
		h.cfg.Agents = map[string]config.AgentConfig{}
	}

	var req struct {
		Name        string   `json:"name"`
		Enabled     *bool    `json:"enabled,omitempty"`
		Model       string   `json:"model,omitempty"`
		Description string   `json:"description,omitempty"`
		Skills      []string `json:"skills,omitempty"`
		ToolGroups  []string `json:"tool_groups,omitempty"`
		Soul        string   `json:"soul,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	name := strings.ToLower(strings.TrimSpace(req.Name))
	if !isValidAgentName(name) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid agent name"})
		return
	}
	if _, exists := h.cfg.Agents[name]; exists {
		c.JSON(http.StatusConflict, gin.H{"error": "agent already exists"})
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	// Create per-agent directory and config files (P0 fix)
	agentDir := getAgentDir(name)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create agent directory failed"})
		return
	}

	// Write per-agent config.yaml
	agentConfig := map[string]interface{}{
		"name":        name,
		"description": strings.TrimSpace(req.Description),
	}
	if req.Model != "" {
		agentConfig["model"] = strings.TrimSpace(req.Model)
	}
	if len(req.Skills) > 0 {
		agentConfig["skills"] = req.Skills
	} else {
		agentConfig["skills"] = []string{} // Empty means no skills restriction
	}
	if len(req.ToolGroups) > 0 {
		agentConfig["tool_groups"] = req.ToolGroups
	}

	configPath := filepath.Join(agentDir, "config.yaml")
	data, err := yaml.Marshal(agentConfig)
	if err != nil {
		os.RemoveAll(agentDir)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "marshal agent config failed"})
		return
	}
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		os.RemoveAll(agentDir)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "write agent config failed"})
		return
	}

	// Write SOUL.md (agent personality)
	soulContent := req.Soul
	if soulContent == "" {
		soulContent = fmt.Sprintf("# %s\n\n%s", name, strings.TrimSpace(req.Description))
	}
	soulPath := filepath.Join(agentDir, "SOUL.md")
	if err := os.WriteFile(soulPath, []byte(soulContent), 0644); err != nil {
		os.RemoveAll(agentDir)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "write agent soul failed"})
		return
	}

	// Update in-memory config
	h.cfg.Agents[name] = config.AgentConfig{
		Enabled:     enabled,
		Model:       strings.TrimSpace(req.Model),
		Description: strings.TrimSpace(req.Description),
	}

	// Save to main config.yaml for backward compatibility
	if err := h.saveAgents(); err != nil {
		os.RemoveAll(agentDir)
		delete(h.cfg.Agents, name)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "persist agents failed"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status":      "created",
		"name":        name,
		"config_path": configPath,
		"soul_path":   soulPath,
		"agent_dir":   agentDir,
	})
}

// UpdateAgent updates an agent's configuration including per-agent files.
func (h *AgentsHandler) UpdateAgent(c *gin.Context) {
	name := strings.ToLower(c.Param("name"))

	var req struct {
		Enabled     *bool    `json:"enabled"`
		Model       *string  `json:"model"`
		Description *string  `json:"description"`
		Skills      []string `json:"skills,omitempty"`
		ToolGroups  []string `json:"tool_groups,omitempty"`
		Soul        *string  `json:"soul,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// Update per-agent config file (P0 fix)
	agentConfig, err := loadAgentConfig(name)
	if err != nil {
		// Create new config if not exists
		agentConfig = map[string]interface{}{
			"name": name,
		}
	}

	if req.Description != nil {
		agentConfig["description"] = strings.TrimSpace(*req.Description)
	}
	if req.Model != nil {
		agentConfig["model"] = strings.TrimSpace(*req.Model)
	}
	if req.Skills != nil {
		agentConfig["skills"] = req.Skills
	}
	if req.ToolGroups != nil {
		agentConfig["tool_groups"] = req.ToolGroups
	}

	if err := saveAgentConfig(name, agentConfig); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "save agent config failed"})
		return
	}

	// Update SOUL.md if provided
	if req.Soul != nil {
		if err := saveAgentSoul(name, *req.Soul); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "save agent soul failed"})
			return
		}
	}

	// Update in-memory config
	if h.cfg != nil && h.cfg.Agents != nil {
		agentCfg, ok := h.cfg.Agents[name]
		if !ok {
			agentCfg = config.AgentConfig{}
		}

		if req.Enabled != nil {
			agentCfg.Enabled = *req.Enabled
		}
		if req.Model != nil {
			agentCfg.Model = strings.TrimSpace(*req.Model)
		}
		if req.Description != nil {
			agentCfg.Description = strings.TrimSpace(*req.Description)
		}

		h.cfg.Agents[name] = agentCfg
		if err := h.saveAgents(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "persist agents failed"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

// DeleteAgent deletes an agent and its directory.
func (h *AgentsHandler) DeleteAgent(c *gin.Context) {
	name := strings.ToLower(c.Param("name"))

	// Delete agent directory (P0 fix)
	agentDir := getAgentDir(name)
	if _, err := os.Stat(agentDir); err == nil {
		if err := os.RemoveAll(agentDir); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "delete agent directory failed"})
			return
		}
	}

	// Delete from in-memory config
	if h.cfg != nil && h.cfg.Agents != nil {
		if _, ok := h.cfg.Agents[name]; ok {
			delete(h.cfg.Agents, name)
			if err := h.saveAgents(); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "persist agents failed"})
				return
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (h *AgentsHandler) saveAgents() error {
	if h == nil || h.cfg == nil {
		return nil
	}
	path := strings.TrimSpace(os.Getenv("GOCLAW_CONFIG_PATH"))
	if path == "" {
		path = "config.yaml"
	}
	data, err := yaml.Marshal(h.cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// getAgentDir returns the directory path for a specific agent.
func getAgentDir(name string) string {
	baseDir := strings.TrimSpace(os.Getenv("GOCLAW_HOME"))
	if baseDir == "" {
		baseDir = ".goclaw"
	}
	return filepath.Join(baseDir, "agents", strings.ToLower(name))
}

// loadAgentConfig loads per-agent config from file system.
func loadAgentConfig(name string) (map[string]interface{}, error) {
	configPath := filepath.Join(getAgentDir(name), "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// loadAgentSoul loads SOUL.md content for an agent.
func loadAgentSoul(name string) (string, error) {
	soulPath := filepath.Join(getAgentDir(name), "SOUL.md")
	data, err := os.ReadFile(soulPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// saveAgentConfig saves per-agent config to file system.
func saveAgentConfig(name string, cfg map[string]interface{}) error {
	agentDir := getAgentDir(name)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return err
	}

	configPath := filepath.Join(agentDir, "config.yaml")
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

// saveAgentSoul saves SOUL.md content for an agent.
func saveAgentSoul(name, content string) error {
	agentDir := getAgentDir(name)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return err
	}

	soulPath := filepath.Join(agentDir, "SOUL.md")
	return os.WriteFile(soulPath, []byte(content), 0644)
}

// CheckAgentName validates whether an agent name is available.
func (h *AgentsHandler) CheckAgentName(c *gin.Context) {
	name := strings.TrimSpace(c.Query("name"))
	if !isValidAgentName(name) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid agent name"})
		return
	}
	name = strings.ToLower(name)

	// Check both directory and in-memory config (P0 fix)
	available := true

	// Check if agent directory exists
	agentDir := getAgentDir(name)
	if _, err := os.Stat(agentDir); err == nil {
		available = false
	}

	// Check in-memory config
	if h.cfg != nil && h.cfg.Agents != nil {
		if _, exists := h.cfg.Agents[name]; exists {
			available = false
		}
	}

	c.JSON(http.StatusOK, gin.H{"name": name, "available": available})
}

// GetUserProfile returns a minimal profile payload for compatibility.
func (h *AgentsHandler) GetUserProfile(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"name": "default",
		"role": "user",
	})
}

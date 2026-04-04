// Agents handler exposes CRUD and helper routes for custom agents.
package handlers

import (
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/bookerbai/goclaw/internal/config"
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
	if h.cfg != nil {
		names := make([]string, 0, len(h.cfg.Agents))
		for name := range h.cfg.Agents {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			agentCfg := h.cfg.Agents[name]
			agents = append(agents, map[string]any{
				"name":        name,
				"enabled":     agentCfg.Enabled,
				"model":       agentCfg.Model,
				"description": agentCfg.Description,
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"agents": agents})
}

// GetAgent returns details for a single agent.
func (h *AgentsHandler) GetAgent(c *gin.Context) {
	name := c.Param("name")
	if h.cfg == nil || h.cfg.Agents == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}
	agentCfg, ok := h.cfg.Agents[name]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}
	c.JSON(http.StatusOK, map[string]any{
		"name":        name,
		"enabled":     agentCfg.Enabled,
		"model":       agentCfg.Model,
		"description": agentCfg.Description,
	})
}

// CreateAgent creates a new custom agent (in-memory config).
func (h *AgentsHandler) CreateAgent(c *gin.Context) {
	if h.cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "config unavailable"})
		return
	}
	if h.cfg.Agents == nil {
		h.cfg.Agents = map[string]config.AgentConfig{}
	}

	var req struct {
		Name        string `json:"name"`
		Enabled     *bool  `json:"enabled,omitempty"`
		Model       string `json:"model,omitempty"`
		Description string `json:"description,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	name := strings.TrimSpace(req.Name)
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
	h.cfg.Agents[name] = config.AgentConfig{
		Enabled:     enabled,
		Model:       strings.TrimSpace(req.Model),
		Description: strings.TrimSpace(req.Description),
	}
	c.JSON(http.StatusCreated, gin.H{"status": "created", "name": name})
}

// UpdateAgent updates an agent's configuration (in-memory only).
func (h *AgentsHandler) UpdateAgent(c *gin.Context) {
	name := c.Param("name")
	if h.cfg == nil || h.cfg.Agents == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}
	agentCfg, ok := h.cfg.Agents[name]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	var req struct {
		Enabled     *bool   `json:"enabled"`
		Model       *string `json:"model"`
		Description *string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
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

	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

// DeleteAgent deletes an agent.
func (h *AgentsHandler) DeleteAgent(c *gin.Context) {
	name := c.Param("name")
	if h.cfg == nil || h.cfg.Agents == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}
	if _, ok := h.cfg.Agents[name]; !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}
	delete(h.cfg.Agents, name)
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// CheckAgentName validates whether an agent name is available.
func (h *AgentsHandler) CheckAgentName(c *gin.Context) {
	name := strings.TrimSpace(c.Query("name"))
	if !isValidAgentName(name) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid agent name"})
		return
	}
	available := true
	if h.cfg != nil && h.cfg.Agents != nil {
		_, exists := h.cfg.Agents[name]
		available = !exists
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

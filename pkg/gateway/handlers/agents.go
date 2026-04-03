// Agents handler exposes GET /api/agents and PUT /api/agents/:name routes.
// This is a placeholder for custom agent management.
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/bookerbai/goclaw/internal/config"
)

// AgentsHandler handles custom agent CRUD (placeholder).
type AgentsHandler struct {
	cfg *config.AppConfig
}

// NewAgentsHandler creates an AgentsHandler.
func NewAgentsHandler(cfg *config.AppConfig) *AgentsHandler {
	return &AgentsHandler{cfg: cfg}
}

// ListAgents returns the list of configured agents.
func (h *AgentsHandler) ListAgents(c *gin.Context) {
	agents := []map[string]any{}
	if h.cfg != nil {
		for name, agentCfg := range h.cfg.Agents {
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

// UpdateAgent updates an agent's configuration (placeholder - in-memory only).
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
		agentCfg.Model = *req.Model
	}
	if req.Description != nil {
		agentCfg.Description = *req.Description
	}
	h.cfg.Agents[name] = agentCfg

	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

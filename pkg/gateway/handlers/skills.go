// Skills handler exposes GET /api/skills and PUT /api/skills/:name routes.
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/bookerbai/goclaw/internal/config"
	"github.com/bookerbai/goclaw/internal/skills"
)

// SkillsHandler handles skill listing and updates.
type SkillsHandler struct {
	cfg      *config.AppConfig
	registry *skills.Registry
}

// NewSkillsHandler creates a SkillsHandler.
func NewSkillsHandler(cfg *config.AppConfig, registry *skills.Registry) *SkillsHandler {
	return &SkillsHandler{cfg: cfg, registry: registry}
}

// SkillSummary is a JSON-friendly summary of a skill.
type SkillSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Enabled     bool     `json:"enabled"`
	Category    string   `json:"category,omitempty"`
	Tools       []string `json:"allowed_tools,omitempty"`
}

// ListSkills returns all registered skills.
func (h *SkillsHandler) ListSkills(c *gin.Context) {
	if h.registry == nil {
		c.JSON(http.StatusOK, gin.H{"skills": []SkillSummary{}})
		return
	}

	all := h.registry.List()
	summaries := make([]SkillSummary, 0, len(all))
	for _, sk := range all {
		summaries = append(summaries, SkillSummary{
			Name:        sk.Metadata.Name,
			Description: sk.Metadata.Description,
			Enabled:     sk.Metadata.Enabled,
			Category:    sk.Metadata.Category,
			Tools:       sk.Metadata.AllowedTools,
		})
	}
	c.JSON(http.StatusOK, gin.H{"skills": summaries})
}

// GetSkill returns details for a single skill.
func (h *SkillsHandler) GetSkill(c *gin.Context) {
	name := c.Param("name")
	if h.registry == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "skill not found"})
		return
	}
	sk := h.registry.GetByName(name)
	if sk == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "skill not found"})
		return
	}
	c.JSON(http.StatusOK, SkillSummary{
		Name:        sk.Metadata.Name,
		Description: sk.Metadata.Description,
		Enabled:     sk.Metadata.Enabled,
		Category:    sk.Metadata.Category,
		Tools:       sk.Metadata.AllowedTools,
	})
}

// UpdateSkill enables or disables a skill.
func (h *SkillsHandler) UpdateSkill(c *gin.Context) {
	name := c.Param("name")
	if h.registry == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "skill not found"})
		return
	}
	sk := h.registry.GetByName(name)
	if sk == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "skill not found"})
		return
	}

	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if req.Enabled != nil {
		sk.Metadata.Enabled = *req.Enabled
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

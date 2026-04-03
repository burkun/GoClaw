// Package handlers contains HTTP handler implementations for the GoClaw gateway.
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/bookerbai/goclaw/internal/config"
)

// ModelsHandler serves the /api/models endpoint.
type ModelsHandler struct {
	cfg *config.AppConfig
}

// NewModelsHandler creates a handler that reads model info from cfg.
func NewModelsHandler(cfg *config.AppConfig) *ModelsHandler {
	return &ModelsHandler{cfg: cfg}
}

// ModelResponse is the wire format for a single model entry.
type ModelResponse struct {
	// ID is the unique model name used in configuration and API requests.
	ID string `json:"id"`
	// Model is the actual provider-level model identifier (e.g. "gpt-4o").
	Model string `json:"model"`
	// DisplayName is a human-readable label suitable for UI rendering.
	DisplayName string `json:"display_name,omitempty"`
	// Description is an optional prose description of the model.
	Description string `json:"description,omitempty"`
	// Capabilities holds feature flags for this model.
	Capabilities ModelCapabilities `json:"capabilities"`
}

// ModelCapabilities declares optional features supported by a model.
type ModelCapabilities struct {
	// SupportsThinking indicates the model supports extended chain-of-thought mode.
	SupportsThinking bool `json:"supports_thinking"`
	// SupportsReasoningEffort indicates the model supports reasoning effort hints.
	SupportsReasoningEffort bool `json:"supports_reasoning_effort"`
	// SupportsVision indicates the model can process image inputs.
	SupportsVision bool `json:"supports_vision"`
}

// ModelsListResponse is the top-level response envelope for GET /api/models.
type ModelsListResponse struct {
	Models []ModelResponse `json:"models"`
}

// ListModels handles GET /api/models.
// It reads the model configuration list and returns each model's public metadata.
// Sensitive fields (API keys, base URLs, internal routing config) are excluded.
func (h *ModelsHandler) ListModels(c *gin.Context) {
	models := make([]ModelResponse, 0)
	if h.cfg != nil {
		models = make([]ModelResponse, 0, len(h.cfg.Models))
		for _, model := range h.cfg.Models {
			models = append(models, ModelResponse{
				ID:          model.Name,
				Model:       model.Model,
				DisplayName: model.DisplayName,
				Description: model.Description,
				Capabilities: ModelCapabilities{
					SupportsThinking:        model.SupportsThinking,
					SupportsReasoningEffort: model.SupportsReasoningEffort,
					SupportsVision:          model.SupportsVision,
				},
			})
		}
	}

	c.JSON(http.StatusOK, ModelsListResponse{Models: models})
}

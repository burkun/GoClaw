// Package handlers contains HTTP handler implementations for the GoClaw gateway.
package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"goclaw/internal/config"
	"goclaw/internal/models"
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
	// Name is an alias for ID, for frontend compatibility.
	Name string `json:"name"`
	// Model is the actual provider-level model identifier (e.g. "gpt-4o").
	Model string `json:"model"`
	// DisplayName is a human-readable label suitable for UI rendering.
	DisplayName string `json:"display_name,omitempty"`
	// Description is an optional prose description of the model.
	Description string `json:"description,omitempty"`
	// UseResponsesAPI indicates whether this model opts into Responses API.
	UseResponsesAPI *bool `json:"use_responses_api,omitempty"`
	// OutputVersion is the configured structured output version.
	OutputVersion string `json:"output_version,omitempty"`
	// HasAPIBase indicates whether api_base/base_url override is configured.
	HasAPIBase bool `json:"has_api_base"`
	// HasGeminiAPIKey indicates whether gemini_api_key is configured.
	HasGeminiAPIKey bool `json:"has_gemini_api_key"`
	// SupportsThinking is at the top level for frontend compatibility.
	SupportsThinking bool `json:"supports_thinking"`
	// SupportsReasoningEffort is at the top level for frontend compatibility.
	SupportsReasoningEffort bool `json:"supports_reasoning_effort"`
	// SupportsVision is at the top level for frontend compatibility.
	SupportsVision bool `json:"supports_vision"`
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

// ModelValidateResponse is the response for POST /api/models/:id/validate.
type ModelValidateResponse struct {
	Valid   bool   `json:"valid"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
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
				ID:                      model.Name,
				Name:                    model.Name,
				Model:                   model.Model,
				DisplayName:             model.DisplayName,
				Description:             model.Description,
				UseResponsesAPI:         model.UseResponsesAPI,
				OutputVersion:           model.OutputVersion,
				HasAPIBase:              model.APIBase != "" || model.BaseURL != "",
				HasGeminiAPIKey:         model.GeminiAPIKey != "",
				SupportsThinking:        model.SupportsThinking,
				SupportsReasoningEffort: model.SupportsReasoningEffort,
				SupportsVision:          model.SupportsVision,
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

// GetModel handles GET /api/models/:id.
// Returns detailed configuration for a specific model.
func (h *ModelsHandler) GetModel(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model id required"})
		return
	}

	if h.cfg == nil {
		NewServiceUnavailableError("config unavailable").Render(c, http.StatusServiceUnavailable)
		return
	}

	modelCfg := h.cfg.GetModelConfig(id)
	if modelCfg == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "model not found"})
		return
	}

	c.JSON(http.StatusOK, ModelResponse{
		ID:              modelCfg.Name,
		Model:           modelCfg.Model,
		DisplayName:     modelCfg.DisplayName,
		Description:     modelCfg.Description,
		UseResponsesAPI: modelCfg.UseResponsesAPI,
		OutputVersion:   modelCfg.OutputVersion,
		HasAPIBase:      modelCfg.APIBase != "" || modelCfg.BaseURL != "",
		HasGeminiAPIKey: modelCfg.GeminiAPIKey != "",
		Capabilities: ModelCapabilities{
			SupportsThinking:        modelCfg.SupportsThinking,
			SupportsReasoningEffort: modelCfg.SupportsReasoningEffort,
			SupportsVision:          modelCfg.SupportsVision,
		},
	})
}

// ValidateModel handles POST /api/models/:id/validate.
// Tests whether the model can be instantiated successfully.
func (h *ModelsHandler) ValidateModel(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model id required"})
		return
	}

	if h.cfg == nil {
		NewServiceUnavailableError("config unavailable").Render(c, http.StatusServiceUnavailable)
		return
	}

	modelCfg := h.cfg.GetModelConfig(id)
	if modelCfg == nil {
		c.JSON(http.StatusNotFound, ModelValidateResponse{
			Valid:   false,
			Message: "model not found",
		})
		return
	}

	// Attempt to create the model to validate configuration
	_, err := models.CreateChatModel(context.Background(), h.cfg, models.CreateRequest{
		ModelName: id,
	})
	if err != nil {
		c.JSON(http.StatusOK, ModelValidateResponse{
			Valid:   false,
			Message: "model validation failed",
			Error:   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, ModelValidateResponse{
		Valid:   true,
		Message: "model validated successfully",
	})
}

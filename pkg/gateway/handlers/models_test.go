package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bookerbai/goclaw/internal/config"
)

func TestListModels_Empty(t *testing.T) {
	h := NewModelsHandler(&config.AppConfig{Models: []config.ModelConfig{}})

	req := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)

	h.ListModels(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp ModelsListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if len(resp.Models) != 0 {
		t.Fatalf("expected empty models list, got %d", len(resp.Models))
	}
}

func TestListModels_Mapping(t *testing.T) {
	useResponsesAPI := true
	h := NewModelsHandler(&config.AppConfig{Models: []config.ModelConfig{
		{
			Name:                    "gpt-4o",
			Model:                   "gpt-4o-2024-08-06",
			DisplayName:             "GPT-4o",
			Description:             "OpenAI model",
			SupportsThinking:        true,
			SupportsReasoningEffort: true,
			SupportsVision:          true,
			UseResponsesAPI:         &useResponsesAPI,
			OutputVersion:           "responses/v1",
			APIBase:                 "https://api.example.com/v1",
			GeminiAPIKey:            "secret-gemini-key",
		},
	}})

	req := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)

	h.ListModels(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp ModelsListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if len(resp.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(resp.Models))
	}

	m := resp.Models[0]
	if m.ID != "gpt-4o" || m.Model != "gpt-4o-2024-08-06" {
		t.Fatalf("unexpected model mapping: %+v", m)
	}
	if !m.Capabilities.SupportsThinking || !m.Capabilities.SupportsReasoningEffort || !m.Capabilities.SupportsVision {
		t.Fatalf("unexpected capabilities mapping: %+v", m.Capabilities)
	}
	if m.UseResponsesAPI == nil || *m.UseResponsesAPI != true {
		t.Fatalf("unexpected use_responses_api mapping: %+v", m.UseResponsesAPI)
	}
	if m.OutputVersion != "responses/v1" {
		t.Fatalf("unexpected output_version mapping: %s", m.OutputVersion)
	}
	if !m.HasAPIBase {
		t.Fatalf("expected has_api_base=true")
	}
	if !m.HasGeminiAPIKey {
		t.Fatalf("expected has_gemini_api_key=true")
	}
}

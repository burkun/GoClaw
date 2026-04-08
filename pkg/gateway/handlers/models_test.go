package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

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

func TestGetModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewModelsHandler(&config.AppConfig{Models: []config.ModelConfig{
		{Name: "gpt-4", Model: "gpt-4-turbo"},
	}})

	router := gin.New()
	router.GET("/models/:id", h.GetModel)

	req := httptest.NewRequest(http.MethodGet, "/models/gpt-4", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestGetModel_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewModelsHandler(&config.AppConfig{Models: []config.ModelConfig{}})

	router := gin.New()
	router.GET("/models/:id", h.GetModel)

	req := httptest.NewRequest(http.MethodGet, "/models/nonexistent", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestGetModel_MissingID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewModelsHandler(&config.AppConfig{})

	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = httptest.NewRequest(http.MethodGet, "/models/", nil)
	h.GetModel(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestGetModel_NilConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewModelsHandler(nil)

	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Params = gin.Params{{Key: "id", Value: "gpt-4"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/models/gpt-4", nil)
	h.GetModel(c)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
}

func TestValidateModel_MissingID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewModelsHandler(nil)

	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = httptest.NewRequest(http.MethodPost, "/models//validate", nil)
	h.ValidateModel(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestValidateModel_NilConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewModelsHandler(nil)

	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Params = gin.Params{{Key: "id", Value: "gpt-4"}}
	c.Request = httptest.NewRequest(http.MethodPost, "/models/gpt-4/validate", strings.NewReader("{}"))
	h.ValidateModel(c)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
}

func TestValidateModel_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewModelsHandler(&config.AppConfig{Models: []config.ModelConfig{}})

	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Params = gin.Params{{Key: "id", Value: "nonexistent"}}
	c.Request = httptest.NewRequest(http.MethodPost, "/models/nonexistent/validate", strings.NewReader("{}"))
	h.ValidateModel(c)

	// Model not found returns 404
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}

	var resp ModelValidateResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if resp.Valid {
		t.Fatal("expected valid=false for not found model")
	}
}

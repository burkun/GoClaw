package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/bookerbai/goclaw/internal/config"
)

func TestLangGraphHandler_CreateThread(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	router := gin.New()
	router.POST("/threads", h.CreateThread)

	req := httptest.NewRequest(http.MethodPost, "/threads", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rr.Code, rr.Body.String())
	}

	// Verify response contains thread_id.
	if !strings.Contains(rr.Body.String(), "thread_id") {
		t.Errorf("expected response to contain thread_id, got: %s", rr.Body.String())
	}
}

func TestLangGraphHandler_GetThread(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	router := gin.New()
	router.GET("/threads/:thread_id", h.GetThread)

	req := httptest.NewRequest(http.MethodGet, "/threads/test-thread-123", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	if !strings.Contains(rr.Body.String(), "test-thread-123") {
		t.Errorf("expected response to contain thread_id, got: %s", rr.Body.String())
	}
}

func TestLangGraphHandler_SearchThreads(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	router := gin.New()
	router.POST("/threads/search", h.SearchThreads)

	req := httptest.NewRequest(http.MethodPost, "/threads/search", strings.NewReader(`{"limit": 10}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestLangGraphHandler_GetAssistant(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	router := gin.New()
	router.GET("/assistants/:assistant_id", h.GetAssistant)

	req := httptest.NewRequest(http.MethodGet, "/assistants/lead_agent", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	if !strings.Contains(rr.Body.String(), "lead_agent") {
		t.Errorf("expected response to contain lead_agent, got: %s", rr.Body.String())
	}
}

func TestLangGraphHandler_GetThreadState(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	router := gin.New()
	router.GET("/threads/:thread_id/state", h.GetThreadState)

	req := httptest.NewRequest(http.MethodGet, "/threads/test-thread/state", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	// Verify response contains values.
	if !strings.Contains(rr.Body.String(), "values") {
		t.Errorf("expected response to contain values, got: %s", rr.Body.String())
	}
}

func TestLangGraphHandler_UpdateThreadState(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	router := gin.New()
	router.PATCH("/threads/:thread_id/state", h.UpdateThreadState)

	body := `{"values": {"title": "Test Thread"}}`
	req := httptest.NewRequest(http.MethodPatch, "/threads/test-thread/state", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	if !strings.Contains(rr.Body.String(), "checkpoint_id") {
		t.Errorf("expected response to contain checkpoint_id, got: %s", rr.Body.String())
	}
}

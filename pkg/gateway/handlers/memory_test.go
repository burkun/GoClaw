package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bookerbai/goclaw/internal/config"
)

func TestGetMemory_UsesConfiguredStoragePath(t *testing.T) {
	tmp := t.TempDir()
	memPath := filepath.Join(tmp, "custom-memory.json")
	content := `{"version":"2.0","facts":[{"id":"f1","content":"c","category":"pref","confidence":0.9,"createdAt":"now","source":"t1"}]}`
	if err := os.WriteFile(memPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write memory file failed: %v", err)
	}

	h := NewMemoryHandler(&config.AppConfig{Memory: config.MemoryConfig{StoragePath: memPath}})
	req := httptest.NewRequest(http.MethodGet, "/api/memory", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)

	h.GetMemory(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp MemoryResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if resp.Version != "2.0" || len(resp.Facts) != 1 {
		t.Fatalf("unexpected memory response: %+v", resp)
	}
}

func TestGetMemory_FallbackDefaultPath(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	if err := os.WriteFile("memory.json", []byte(`{"version":"1.1","facts":[]}`), 0o644); err != nil {
		t.Fatalf("write memory file failed: %v", err)
	}

	h := NewMemoryHandler(&config.AppConfig{})
	req := httptest.NewRequest(http.MethodGet, "/api/memory", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)

	h.GetMemory(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp MemoryResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if resp.Version != "1.1" {
		t.Fatalf("expected version 1.1, got %s", resp.Version)
	}
}

func TestGetMemory_FileNotExistReturnsEmpty(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	h := NewMemoryHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/memory", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)

	h.GetMemory(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp MemoryResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if len(resp.Facts) != 0 || resp.Version != "1.0" {
		t.Fatalf("unexpected empty response: %+v", resp)
	}
}

func TestGetMemory_CacheRefreshOnMtimeChange(t *testing.T) {
	tmp := t.TempDir()
	memPath := filepath.Join(tmp, "memory.json")
	if err := os.WriteFile(memPath, []byte(`{"version":"1.0","facts":[]}`), 0o644); err != nil {
		t.Fatalf("write memory file failed: %v", err)
	}

	h := NewMemoryHandler(&config.AppConfig{Memory: config.MemoryConfig{StoragePath: memPath}})

	req1 := httptest.NewRequest(http.MethodGet, "/api/memory", nil)
	rr1 := httptest.NewRecorder()
	c1, _ := newGinContext(rr1, req1, nil)
	h.GetMemory(c1)

	if rr1.Code != http.StatusOK {
		t.Fatalf("expected first call 200, got %d", rr1.Code)
	}

	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(memPath, []byte(`{"version":"2.0","facts":[]}`), 0o644); err != nil {
		t.Fatalf("rewrite memory file failed: %v", err)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/memory", nil)
	rr2 := httptest.NewRecorder()
	c2, _ := newGinContext(rr2, req2, nil)
	h.GetMemory(c2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("expected second call 200, got %d", rr2.Code)
	}
	var resp MemoryResponse
	if err := json.Unmarshal(rr2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if resp.Version != "2.0" {
		t.Fatalf("expected refreshed version 2.0, got %s", resp.Version)
	}
}

func TestGetMemory_RejectsLegacyStringFactsSchema(t *testing.T) {
	tmp := t.TempDir()
	memPath := filepath.Join(tmp, "memory.json")
	content := `{"version":"1.0","lastUpdated":"2026-01-01T00:00:00Z","facts":["User prefers Go","Works on AgentClaw"]}`
	if err := os.WriteFile(memPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write memory file failed: %v", err)
	}

	h := NewMemoryHandler(&config.AppConfig{Memory: config.MemoryConfig{StoragePath: memPath}})
	req := httptest.NewRequest(http.MethodGet, "/api/memory", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)

	h.GetMemory(c)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for legacy schema, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestCreateFact(t *testing.T) {
	tmp := t.TempDir()
	memPath := filepath.Join(tmp, "memory.json")
	h := NewMemoryHandler(&config.AppConfig{Memory: config.MemoryConfig{StoragePath: memPath}})

	body := `{"content": "User likes Go programming", "category": "preference", "confidence": 0.9}`
	req := httptest.NewRequest(http.MethodPost, "/api/memory/facts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)

	h.CreateFact(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestCreateFact_InvalidConfidence(t *testing.T) {
	h := NewMemoryHandler(nil)

	body := `{"content": "test", "confidence": 1.5}`
	req := httptest.NewRequest(http.MethodPost, "/api/memory/facts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)

	h.CreateFact(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestCreateFact_EmptyContent(t *testing.T) {
	h := NewMemoryHandler(nil)

	body := `{"content": ""}`
	req := httptest.NewRequest(http.MethodPost, "/api/memory/facts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)

	h.CreateFact(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDeleteFact(t *testing.T) {
	tmp := t.TempDir()
	memPath := filepath.Join(tmp, "memory.json")
	content := `{"version":"1.0","facts":[{"id":"fact-1","content":"test","category":"ctx","confidence":0.5,"createdAt":"now","source":"manual"}]}`
	if err := os.WriteFile(memPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write memory file failed: %v", err)
	}

	h := NewMemoryHandler(&config.AppConfig{Memory: config.MemoryConfig{StoragePath: memPath}})
	req := httptest.NewRequest(http.MethodDelete, "/api/memory/facts/fact-1", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"fact_id": "fact-1"})

	h.DeleteFact(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDeleteFact_NotFound(t *testing.T) {
	tmp := t.TempDir()
	memPath := filepath.Join(tmp, "memory.json")
	os.WriteFile(memPath, []byte(`{"version":"1.0","facts":[]}`), 0o644)

	h := NewMemoryHandler(&config.AppConfig{Memory: config.MemoryConfig{StoragePath: memPath}})
	req := httptest.NewRequest(http.MethodDelete, "/api/memory/facts/nonexistent", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"fact_id": "nonexistent"})

	h.DeleteFact(c)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestPatchFact(t *testing.T) {
	tmp := t.TempDir()
	memPath := filepath.Join(tmp, "memory.json")
	content := `{"version":"1.0","facts":[{"id":"fact-2","content":"old content","category":"ctx","confidence":0.5,"createdAt":"now","source":"manual"}]}`
	if err := os.WriteFile(memPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write memory file failed: %v", err)
	}

	h := NewMemoryHandler(&config.AppConfig{Memory: config.MemoryConfig{StoragePath: memPath}})
	body := `{"content": "new content", "confidence": 0.8}`
	req := httptest.NewRequest(http.MethodPatch, "/api/memory/facts/fact-2", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"fact_id": "fact-2"})

	h.PatchFact(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPatchFact_NotFound(t *testing.T) {
	tmp := t.TempDir()
	memPath := filepath.Join(tmp, "memory.json")
	os.WriteFile(memPath, []byte(`{"version":"1.0","facts":[]}`), 0o644)

	h := NewMemoryHandler(&config.AppConfig{Memory: config.MemoryConfig{StoragePath: memPath}})
	body := `{"content": "new content"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/memory/facts/nonexistent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"fact_id": "nonexistent"})

	h.PatchFact(c)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestPatchFact_InvalidConfidence(t *testing.T) {
	h := NewMemoryHandler(nil)
	body := `{"confidence": 2.0}`
	req := httptest.NewRequest(http.MethodPatch, "/api/memory/facts/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"fact_id": "test"})

	h.PatchFact(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestClearMemory(t *testing.T) {
	tmp := t.TempDir()
	memPath := filepath.Join(tmp, "memory.json")
	os.WriteFile(memPath, []byte(`{"version":"1.0","facts":[{"id":"1","content":"test"}]}`), 0o644)

	h := NewMemoryHandler(&config.AppConfig{Memory: config.MemoryConfig{StoragePath: memPath}})
	req := httptest.NewRequest(http.MethodDelete, "/api/memory", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)

	h.ClearMemory(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestReloadMemory(t *testing.T) {
	tmp := t.TempDir()
	memPath := filepath.Join(tmp, "memory.json")
	os.WriteFile(memPath, []byte(`{"version":"1.0","facts":[]}`), 0o644)

	h := NewMemoryHandler(&config.AppConfig{Memory: config.MemoryConfig{StoragePath: memPath}})
	req := httptest.NewRequest(http.MethodPost, "/api/memory/reload", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)

	h.ReloadMemory(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestExportMemory(t *testing.T) {
	tmp := t.TempDir()
	memPath := filepath.Join(tmp, "memory.json")
	os.WriteFile(memPath, []byte(`{"version":"1.0","facts":[]}`), 0o644)

	h := NewMemoryHandler(&config.AppConfig{Memory: config.MemoryConfig{StoragePath: memPath}})
	req := httptest.NewRequest(http.MethodGet, "/api/memory/export", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)

	h.ExportMemory(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestImportMemory(t *testing.T) {
	tmp := t.TempDir()
	memPath := filepath.Join(tmp, "memory.json")
	h := NewMemoryHandler(&config.AppConfig{Memory: config.MemoryConfig{StoragePath: memPath}})

	body := `{"version":"2.0","facts":[{"id":"imp-1","content":"imported fact","category":"ctx","confidence":0.9,"createdAt":"now","source":"import"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/memory/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)

	h.ImportMemory(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestGetMemoryConfig(t *testing.T) {
	h := NewMemoryHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/memory/config", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)

	h.GetMemoryConfig(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestGetMemoryStatus(t *testing.T) {
	tmp := t.TempDir()
	memPath := filepath.Join(tmp, "memory.json")
	os.WriteFile(memPath, []byte(`{"version":"1.0","facts":[]}`), 0o644)

	h := NewMemoryHandler(&config.AppConfig{Memory: config.MemoryConfig{StoragePath: memPath}})
	req := httptest.NewRequest(http.MethodGet, "/api/memory/status", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)

	h.GetMemoryStatus(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestRegisterMemoryRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	h := NewMemoryHandler(nil)
	RegisterMemoryRoutes(api, h)

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/memory"},
		{http.MethodPost, "/api/memory/reload"},
		{http.MethodDelete, "/api/memory"},
		{http.MethodPost, "/api/memory/facts"},
		{http.MethodGet, "/api/memory/export"},
		{http.MethodPost, "/api/memory/import"},
		{http.MethodGet, "/api/memory/config"},
		{http.MethodGet, "/api/memory/status"},
	}
	for _, tc := range routes {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if tc.method == http.MethodPost || tc.method == http.MethodDelete {
			req.Header.Set("Content-Type", "application/json")
		}
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code == http.StatusNotFound {
			t.Errorf("route not registered: %s %s", tc.method, tc.path)
		}
	}
}

func TestCreateFact_InvalidJSON(t *testing.T) {
	h := NewMemoryHandler(nil)

	req := httptest.NewRequest(http.MethodPost, "/api/memory/facts", strings.NewReader("invalid"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)

	h.CreateFact(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPatchFact_InvalidJSON(t *testing.T) {
	h := NewMemoryHandler(nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/memory/facts/test", strings.NewReader("invalid"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"fact_id": "test"})

	h.PatchFact(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPatchFact_EmptyContent(t *testing.T) {
	h := NewMemoryHandler(nil)

	body := `{"content": ""}`
	req := httptest.NewRequest(http.MethodPatch, "/api/memory/facts/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"fact_id": "test"})

	h.PatchFact(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestImportMemory_InvalidJSON(t *testing.T) {
	h := NewMemoryHandler(nil)

	req := httptest.NewRequest(http.MethodPost, "/api/memory/import", strings.NewReader("invalid"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)

	h.ImportMemory(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

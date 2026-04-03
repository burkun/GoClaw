package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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

	if err := os.MkdirAll(filepath.Join(".goclaw"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(".goclaw", "memory.json"), []byte(`{"version":"1.1","facts":[]}`), 0o644); err != nil {
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

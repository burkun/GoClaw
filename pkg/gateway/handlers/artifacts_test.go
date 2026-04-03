package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactsHandler_GetArtifact_Success(t *testing.T) {
	tmp := t.TempDir()
	threadDir := filepath.Join(tmp, "thread-001", "user-data", "outputs")
	_ = os.MkdirAll(threadDir, 0o755)
	artifactPath := filepath.Join(threadDir, "report.txt")
	_ = os.WriteFile(artifactPath, []byte("hello artifact"), 0o644)

	h := NewArtifactsHandler(nil, tmp)
	req := httptest.NewRequest(http.MethodGet, "/api/threads/thread-001/artifacts/outputs/report.txt", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-001", "path": "outputs/report.txt"})

	h.GetArtifact(ctx)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "hello artifact" {
		t.Errorf("unexpected body: %q", rr.Body.String())
	}
}

func TestArtifactsHandler_GetArtifact_NotFound(t *testing.T) {
	tmp := t.TempDir()
	h := NewArtifactsHandler(nil, tmp)
	req := httptest.NewRequest(http.MethodGet, "/api/threads/thread-001/artifacts/outputs/missing.txt", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-001", "path": "outputs/missing.txt"})

	h.GetArtifact(ctx)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestArtifactsHandler_GetArtifact_PathTraversal(t *testing.T) {
	tmp := t.TempDir()
	h := NewArtifactsHandler(nil, tmp)
	req := httptest.NewRequest(http.MethodGet, "/api/threads/thread-001/artifacts/../../../etc/passwd", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-001", "path": "../../../etc/passwd"})

	h.GetArtifact(ctx)

	if rr.Code == http.StatusOK {
		t.Error("expected non-200 for path traversal")
	}
}

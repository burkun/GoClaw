package handlers

import (
	"archive/zip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestArtifactsHandler_GetArtifact_FromSkillArchive(t *testing.T) {
	tmp := t.TempDir()
	threadDir := filepath.Join(tmp, "thread-001", "user-data", "outputs")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(threadDir, "demo.skill")
	if err := writeArtifactSkillArchive(archivePath, "SKILL.md", "# Demo"); err != nil {
		t.Fatal(err)
	}

	h := NewArtifactsHandler(nil, tmp)
	req := httptest.NewRequest(http.MethodGet, "/api/threads/thread-001/artifacts/outputs/demo.skill/SKILL.md", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-001", "path": "outputs/demo.skill/SKILL.md"})

	h.GetArtifact(ctx)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "# Demo") {
		t.Fatalf("expected extracted content")
	}
}

func TestArtifactsHandler_GetArtifact_DownloadParam(t *testing.T) {
	tmp := t.TempDir()
	threadDir := filepath.Join(tmp, "thread-001", "user-data", "outputs")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(threadDir, "report.txt")
	if err := os.WriteFile(artifactPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := NewArtifactsHandler(nil, tmp)
	req := httptest.NewRequest(http.MethodGet, "/api/threads/thread-001/artifacts/outputs/report.txt?download=true", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-001", "path": "outputs/report.txt"})

	h.GetArtifact(ctx)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if cd := rr.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Fatalf("expected attachment disposition, got %q", cd)
	}
}

func TestArtifactsHandler_GetArtifact_ListDirectory(t *testing.T) {
	tmp := t.TempDir()
	threadDir := filepath.Join(tmp, "thread-001", "user-data", "outputs")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(threadDir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := NewArtifactsHandler(nil, tmp)
	req := httptest.NewRequest(http.MethodGet, "/api/threads/thread-001/artifacts/outputs?list=true", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-001", "path": "outputs"})

	h.GetArtifact(ctx)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "a.txt") {
		t.Fatalf("expected list response to include a.txt, got %s", rr.Body.String())
	}
}

func writeArtifactSkillArchive(path, innerName, content string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	w, err := zw.Create(innerName)
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(content)); err != nil {
		return err
	}
	return zw.Close()
}

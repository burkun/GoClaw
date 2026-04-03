package uploads

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bookerbai/goclaw/internal/middleware"
)

func TestUploadsMiddleware_Before_ListsFiles(t *testing.T) {
	tmp := t.TempDir()
	uploadsDir := filepath.Join(tmp, "uploads")
	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadsDir, "file1.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadsDir, "file2.pdf"), []byte("b"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	mw := New()
	state := &middleware.State{
		ThreadID: "t1",
		Extra:    map[string]any{"uploads_path": uploadsDir},
	}
	if err := mw.Before(context.Background(), state); err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	files, ok := state.Extra["uploads"].([]string)
	if !ok {
		t.Fatalf("uploads not []string: %T", state.Extra["uploads"])
	}
	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d", len(files))
	}
}

func TestUploadsMiddleware_Before_EmptyDir(t *testing.T) {
	tmp := t.TempDir()
	uploadsDir := filepath.Join(tmp, "uploads")
	_ = os.MkdirAll(uploadsDir, 0o755)

	mw := New()
	state := &middleware.State{
		ThreadID: "t2",
		Extra:    map[string]any{"uploads_path": uploadsDir},
	}
	_ = mw.Before(context.Background(), state)

	files := state.Extra["uploads"].([]string)
	if len(files) != 0 {
		t.Errorf("expected empty slice, got %v", files)
	}
}

func TestUploadsMiddleware_Before_NoUploadsPath(t *testing.T) {
	mw := New()
	state := &middleware.State{ThreadID: "t3", Extra: map[string]any{}}
	if err := mw.Before(context.Background(), state); err != nil {
		t.Errorf("expected no error when uploads_path missing, got %v", err)
	}
}

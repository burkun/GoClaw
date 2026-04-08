package uploads

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bookerbai/goclaw/internal/middleware"
)

func TestUploadsMiddleware_Name(t *testing.T) {
	mw := New()
	if mw.Name() != "UploadsMiddleware" {
		t.Errorf("expected name 'UploadsMiddleware', got %s", mw.Name())
	}
}

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
	if err := mw.BeforeModel(context.Background(), state); err != nil {
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
	_ = mw.BeforeModel(context.Background(), state)

	files := state.Extra["uploads"].([]string)
	if len(files) != 0 {
		t.Errorf("expected empty slice, got %v", files)
	}
}

func TestUploadsMiddleware_Before_NoUploadsPath(t *testing.T) {
	mw := New()
	state := &middleware.State{ThreadID: "t3", Extra: map[string]any{}}
	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Errorf("expected no error when uploads_path missing, got %v", err)
	}
}

func TestUploadsMiddleware_Before_NonExistentDir(t *testing.T) {
	mw := New()
	state := &middleware.State{
		ThreadID: "t4",
		Extra:    map[string]any{"uploads_path": "/nonexistent/path"},
	}
	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Errorf("expected no error for nonexistent dir, got %v", err)
	}
	files, ok := state.Extra["uploads"].([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", state.Extra["uploads"])
	}
	if len(files) != 0 {
		t.Errorf("expected empty slice, got %v", files)
	}
}

func TestUploadsMiddleware_Before_WithHumanMessage(t *testing.T) {
	tmp := t.TempDir()
	uploadsDir := filepath.Join(tmp, "uploads")
	_ = os.MkdirAll(uploadsDir, 0o755)
	_ = os.WriteFile(filepath.Join(uploadsDir, "test.txt"), []byte("a"), 0o644)

	mw := New()
	state := &middleware.State{
		ThreadID: "t5",
		Extra:    map[string]any{"uploads_path": uploadsDir},
		Messages: []map[string]any{
			{"role": "human", "content": "hello"},
		},
	}
	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	content, _ := state.Messages[0]["content"].(string)
	if len(content) < 10 {
		t.Errorf("expected content to have uploaded_files block, got %s", content)
	}
}

func TestUploadsMiddleware_Before_WithUserMessage(t *testing.T) {
	tmp := t.TempDir()
	uploadsDir := filepath.Join(tmp, "uploads")
	_ = os.MkdirAll(uploadsDir, 0o755)
	_ = os.WriteFile(filepath.Join(uploadsDir, "test.txt"), []byte("a"), 0o644)

	mw := New()
	state := &middleware.State{
		ThreadID: "t6",
		Extra:    map[string]any{"uploads_path": uploadsDir},
		Messages: []map[string]any{
			{"role": "system", "content": "system prompt"},
			{"role": "user", "content": "user message"},
		},
	}
	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	content, _ := state.Messages[1]["content"].(string)
	if len(content) < 10 {
		t.Errorf("expected content to have uploaded_files block, got %s", content)
	}
}

func TestUploadsMiddleware_Before_NoHumanMessage(t *testing.T) {
	tmp := t.TempDir()
	uploadsDir := filepath.Join(tmp, "uploads")
	_ = os.MkdirAll(uploadsDir, 0o755)
	_ = os.WriteFile(filepath.Join(uploadsDir, "test.txt"), []byte("a"), 0o644)

	mw := New()
	state := &middleware.State{
		ThreadID: "t7",
		Extra:    map[string]any{"uploads_path": uploadsDir},
		Messages: []map[string]any{
			{"role": "system", "content": "system prompt"},
		},
	}
	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	// No panic, no error - success
}

func TestUploadsMiddleware_Before_EmptyUploadsPath(t *testing.T) {
	mw := New()
	state := &middleware.State{
		ThreadID: "t8",
		Extra:    map[string]any{"uploads_path": ""},
	}
	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Errorf("expected no error for empty uploads_path, got %v", err)
	}
}

func TestUploadsMiddleware_Before_UploadsPathNotString(t *testing.T) {
	mw := New()
	state := &middleware.State{
		ThreadID: "t9",
		Extra:    map[string]any{"uploads_path": 123},
	}
	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Errorf("expected no error for non-string uploads_path, got %v", err)
	}
}

func TestUploadsMiddleware_Before_SkipsDirectories(t *testing.T) {
	tmp := t.TempDir()
	uploadsDir := filepath.Join(tmp, "uploads")
	_ = os.MkdirAll(uploadsDir, 0o755)
	_ = os.WriteFile(filepath.Join(uploadsDir, "file.txt"), []byte("a"), 0o644)
	_ = os.MkdirAll(filepath.Join(uploadsDir, "subdir"), 0o755)

	mw := New()
	state := &middleware.State{
		ThreadID: "t10",
		Extra:    map[string]any{"uploads_path": uploadsDir},
	}
	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	files := state.Extra["uploads"].([]string)
	if len(files) != 1 {
		t.Errorf("expected 1 file (subdir should be skipped), got %d", len(files))
	}
}

func TestInjectUploadedFilesToMessage(t *testing.T) {
	messages := []map[string]any{
		{"role": "system", "content": "system"},
		{"role": "human", "content": "hello"},
	}
	files := []string{"/mnt/user-data/uploads/file1.txt", "/mnt/user-data/uploads/file2.pdf"}

	injectUploadedFilesToMessage(messages, files)

	content, _ := messages[1]["content"].(string)
	if len(content) < 10 {
		t.Errorf("expected injected content, got %s", content)
	}
}

func TestInjectUploadedFilesToMessage_NoHumanMessage(t *testing.T) {
	messages := []map[string]any{
		{"role": "system", "content": "system"},
	}
	files := []string{"/mnt/user-data/uploads/file1.txt"}

	// Should not panic
	injectUploadedFilesToMessage(messages, files)

	content, _ := messages[0]["content"].(string)
	if content != "system" {
		t.Errorf("system message should not be modified")
	}
}

func TestInjectUploadedFilesToMessage_EmptyFiles(t *testing.T) {
	messages := []map[string]any{
		{"role": "human", "content": "hello"},
	}
	files := []string{}

	// Should still work
	injectUploadedFilesToMessage(messages, files)

	content, _ := messages[0]["content"].(string)
	// Empty files list should still inject the block
	if content == "hello" {
		t.Errorf("expected content to be modified even with empty files")
	}
}

func TestUploadsMiddleware_AfterModel(t *testing.T) {
	mw := New()
	err := mw.AfterModel(context.Background(), &middleware.State{}, &middleware.Response{})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

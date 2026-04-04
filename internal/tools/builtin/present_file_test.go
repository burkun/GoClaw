package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPresentFileTool_Execute_Success(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	outputs := filepath.Join(tmp, "outputs")
	tool := NewPresentFileTool("thread-1", outputs)

	res, err := tool.Execute(context.Background(), `{"path":"`+src+`","filename":"final.txt"}`)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(res), &payload); err != nil {
		t.Fatalf("invalid result json: %v", err)
	}
	if payload["artifact_url"] == "" {
		t.Fatalf("missing artifact_url")
	}
	if _, err := os.Stat(filepath.Join(outputs, "final.txt")); err != nil {
		t.Fatalf("expected copied file: %v", err)
	}
}

func TestPresentFileTool_Execute_Validation(t *testing.T) {
	tool := NewPresentFileTool("thread-1", t.TempDir())
	if _, err := tool.Execute(context.Background(), `{"path":""}`); err == nil {
		t.Fatalf("expected path required error")
	}
	if _, err := tool.Execute(context.Background(), `{"path":"/not/exist"}`); err == nil {
		t.Fatalf("expected file not found error")
	}
}

func TestSanitizeFilename(t *testing.T) {
	got := sanitizeFilename("../a:b?.txt")
	if strings.Contains(got, "..") || strings.Contains(got, ":") || strings.Contains(got, "?") {
		t.Fatalf("unsafe filename not sanitized: %q", got)
	}
}

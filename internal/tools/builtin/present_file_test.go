package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPresentFileTool_Name tests the Name method
func TestPresentFileTool_Name(t *testing.T) {
	tool := NewPresentFileTool("", "")
	if tool.Name() != "present_files" {
		t.Errorf("expected name 'present_files', got %q", tool.Name())
	}
}

// TestPresentFileTool_Description tests the Description method
func TestPresentFileTool_Description(t *testing.T) {
	tool := NewPresentFileTool("", "")
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

// TestPresentFileTool_InputSchema tests the InputSchema method
func TestPresentFileTool_InputSchema(t *testing.T) {
	tool := NewPresentFileTool("", "")
	schema := tool.InputSchema()
	if len(schema) == 0 {
		t.Error("expected non-empty input schema")
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Errorf("input schema is not valid JSON: %v", err)
	}
}

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

func TestPresentFileTool_Execute_InvalidJSON(t *testing.T) {
	tool := NewPresentFileTool("", t.TempDir())
	_, err := tool.Execute(context.Background(), `{invalid json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestPresentFileTool_Execute_Directory(t *testing.T) {
	tool := NewPresentFileTool("", t.TempDir())
	_, err := tool.Execute(context.Background(), `{"path":"/tmp"}`)
	if err == nil {
		t.Error("expected error for directory")
	}
}

func TestPresentFileTool_Execute_WithDescription(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	outputs := filepath.Join(tmp, "outputs")
	tool := NewPresentFileTool("thread-1", outputs)

	res, err := tool.Execute(context.Background(), `{"path":"`+src+`","description":"A test file"}`)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(res), &payload); err != nil {
		t.Fatalf("invalid result json: %v", err)
	}
	if payload["description"] != "A test file" {
		t.Errorf("expected description 'A test file', got %v", payload["description"])
	}
}

func TestPresentFileTool_Execute_CustomFilename(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "original.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	outputs := filepath.Join(tmp, "outputs")
	tool := NewPresentFileTool("thread-1", outputs)

	res, err := tool.Execute(context.Background(), `{"path":"`+src+`","filename":"custom_name.txt"}`)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(res), &payload); err != nil {
		t.Fatalf("invalid result json: %v", err)
	}
	if payload["filename"] != "custom_name.txt" {
		t.Errorf("expected filename 'custom_name.txt', got %v", payload["filename"])
	}
}

func TestPresentFileTool_Execute_SuccessField(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	outputs := filepath.Join(tmp, "outputs")
	tool := NewPresentFileTool("thread-1", outputs)

	res, err := tool.Execute(context.Background(), `{"path":"`+src+`"}`)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(res), &payload); err != nil {
		t.Fatalf("invalid result json: %v", err)
	}
	if payload["success"] != true {
		t.Error("expected success to be true")
	}
	if payload["size_bytes"] == nil {
		t.Error("expected size_bytes to be set")
	}
}

func TestNewPresentFileTool(t *testing.T) {
	tool := NewPresentFileTool("thread-123", "/outputs")
	if tool.ThreadID != "thread-123" {
		t.Errorf("expected ThreadID 'thread-123', got %q", tool.ThreadID)
	}
	if tool.OutputsPath != "/outputs" {
		t.Errorf("expected OutputsPath '/outputs', got %q", tool.OutputsPath)
	}
}

func TestSanitizeFilename_EdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		nonEmpty bool
	}{
		{"", true},  // empty -> "artifact"
		{".", true}, // dot -> "artifact"
		{"../path/file.txt", true},
		{"normal.txt", true},
		{"file<>:\"|?*.txt", true},
	}

	for _, tt := range tests {
		got := sanitizeFilename(tt.input)
		if (got != "") != tt.nonEmpty {
			t.Errorf("sanitizeFilename(%q) = %q, want non-empty: %v", tt.input, got, tt.nonEmpty)
		}
		// Should not contain unsafe chars
		unsafe := []string{"/", "\\", "..", ":", "*", "?", "\"", "<", ">", "|"}
		for _, ch := range unsafe {
			if strings.Contains(got, ch) {
				t.Errorf("sanitizeFilename(%q) contains unsafe char %q: got %q", tt.input, ch, got)
			}
		}
	}
}

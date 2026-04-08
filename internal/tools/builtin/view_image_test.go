package builtin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestViewImageTool_Name tests the Name method
func TestViewImageTool_Name(t *testing.T) {
	tool := NewViewImageTool()
	if tool.Name() != "view_image" {
		t.Errorf("expected name 'view_image', got %q", tool.Name())
	}
}

// TestViewImageTool_Description tests the Description method
func TestViewImageTool_Description(t *testing.T) {
	tool := NewViewImageTool()
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

// TestViewImageTool_InputSchema tests the InputSchema method
func TestViewImageTool_InputSchema(t *testing.T) {
	tool := NewViewImageTool()
	schema := tool.InputSchema()
	if len(schema) == 0 {
		t.Error("expected non-empty input schema")
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Errorf("input schema is not valid JSON: %v", err)
	}
}

func TestViewImageTool_Execute_Success(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "a.png")
	if err := os.WriteFile(p, []byte{0x89, 'P', 'N', 'G'}, 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewViewImageTool()
	res, err := tool.Execute(context.Background(), `{"path":"`+p+`"}`)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(res), &payload); err != nil {
		t.Fatalf("invalid result json: %v", err)
	}
	if payload["mime_type"] != "image/png" {
		t.Fatalf("unexpected mime type: %v", payload["mime_type"])
	}
	stored := tool.GetViewedImages()[p]
	if stored.Base64 == "" {
		t.Fatalf("expected base64 stored")
	}
	if _, err := base64.StdEncoding.DecodeString(stored.Base64); err != nil {
		t.Fatalf("invalid base64 stored: %v", err)
	}
}

func TestViewImageTool_Execute_Validation(t *testing.T) {
	tool := NewViewImageTool()
	if _, err := tool.Execute(context.Background(), `{"path":""}`); err == nil {
		t.Fatalf("expected path required error")
	}
	if _, err := tool.Execute(context.Background(), `{"path":"/not/found.png"}`); err == nil {
		t.Fatalf("expected file not found error")
	}
}

func TestIsImageMIME(t *testing.T) {
	if !isImageMIME("image/png") || isImageMIME("application/json") {
		t.Fatalf("isImageMIME mismatch")
	}
}

func TestGuessMIMETypeFromPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"image.png", "image/png"},
		{"image.jpg", "image/jpeg"},
		{"image.jpeg", "image/jpeg"},
		{"image.gif", "image/gif"},
		{"image.webp", "image/webp"},
		{"image.svg", "image/svg+xml"},
		{"image.bmp", "image/bmp"},
		{"image.ico", "image/x-icon"},
		{"image.PNG", "image/png"}, // case insensitive
		{"image.JPG", "image/jpeg"},
		{"file.txt", "application/octet-stream"},
		{"file.unknown", "application/octet-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := guessMIMETypeFromPath(tt.path)
			if got != tt.expected {
				t.Errorf("guessMIMETypeFromPath(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

func TestViewImageTool_ClearViewedImages(t *testing.T) {
	tool := NewViewImageTool()

	// Create a test image
	tmp := t.TempDir()
	p := filepath.Join(tmp, "test.png")
	if err := os.WriteFile(p, []byte{0x89, 'P', 'N', 'G'}, 0o644); err != nil {
		t.Fatal(err)
	}

	// View an image
	_, err := tool.Execute(context.Background(), `{"path":"`+p+`"}`)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	// Verify image is stored
	if len(tool.GetViewedImages()) != 1 {
		t.Error("expected 1 viewed image")
	}

	// Clear viewed images
	tool.ClearViewedImages()

	// Verify cleared
	if len(tool.GetViewedImages()) != 0 {
		t.Error("expected 0 viewed images after clear")
	}
}

func TestViewImageTool_Execute_InvalidJSON(t *testing.T) {
	tool := NewViewImageTool()
	_, err := tool.Execute(context.Background(), `{invalid json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestViewImageTool_Execute_Directory(t *testing.T) {
	tool := NewViewImageTool()
	tmp := t.TempDir()
	_, err := tool.Execute(context.Background(), `{"path":"`+tmp+`"}`)
	if err == nil {
		t.Error("expected error for directory")
	}
}

func TestViewImageTool_Execute_FileTooLarge(t *testing.T) {
	tool := NewViewImageTool()
	tmp := t.TempDir()

	// Create a large file (simulated by creating a small file and checking the logic)
	// The actual size check happens at runtime, so we just test the path exists
	p := filepath.Join(tmp, "large.png")
	data := make([]byte, 21*1024*1024) // 21MB
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Skip("could not create large test file")
	}

	_, err := tool.Execute(context.Background(), `{"path":"`+p+`"}`)
	if err == nil {
		t.Error("expected error for file too large")
	}
}

func TestViewImageTool_Execute_UnsupportedFormat(t *testing.T) {
	tool := NewViewImageTool()
	tmp := t.TempDir()

	// Create a file with unsupported extension
	p := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(p, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := tool.Execute(context.Background(), `{"path":"`+p+`"}`)
	if err == nil {
		t.Error("expected error for unsupported format")
	}
}

func TestNewViewImageTool(t *testing.T) {
	tool := NewViewImageTool()
	if tool == nil {
		t.Error("expected non-nil tool")
	}
	if tool.ViewedImages == nil {
		t.Error("expected initialized ViewedImages map")
	}
}

func TestViewImageTool_Execute_RelativePath(t *testing.T) {
	tool := NewViewImageTool()

	// Save current dir
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)

	tmp := t.TempDir()
	os.Chdir(tmp)

	// Create test file
	if err := os.WriteFile("test.png", []byte{0x89, 'P', 'N', 'G'}, 0o644); err != nil {
		t.Fatal(err)
	}

	// Execute with relative path
	_, err := tool.Execute(context.Background(), `{"path":"test.png"}`)
	if err != nil {
		t.Errorf("unexpected error for relative path: %v", err)
	}
}

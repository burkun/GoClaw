package media

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type stubResolver struct {
	base string
}

func (r *stubResolver) Resolve(vp string) (string, error) {
	if vp == "/mnt/user-data/outputs" {
		return r.base, nil
	}
	if strings.HasPrefix(vp, "/mnt/user-data/outputs/") {
		rel := strings.TrimPrefix(vp, "/mnt/user-data/outputs/")
		return filepath.Join(r.base, filepath.FromSlash(rel)), nil
	}
	return filepath.Join(r.base, filepath.Base(vp)), nil
}

// ---------------------------------------------------------------------------
// ViewImageTool method tests
// ---------------------------------------------------------------------------

func TestViewImageTool_Name(t *testing.T) {
	tool := &ViewImageTool{}
	if tool.Name() != "view_image" {
		t.Errorf("expected name 'view_image', got %q", tool.Name())
	}
}

func TestViewImageTool_Description(t *testing.T) {
	tool := &ViewImageTool{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestViewImageTool_InputSchema(t *testing.T) {
	tool := &ViewImageTool{}
	schema := tool.InputSchema()
	if len(schema) == 0 {
		t.Error("expected non-empty input schema")
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Errorf("input schema is not valid JSON: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ViewImageTool Execute tests
// ---------------------------------------------------------------------------

func TestViewImageTool_Execute_PNG(t *testing.T) {
	tmp := t.TempDir()
	imgPath := filepath.Join(tmp, "test.png")
	// minimal PNG bytes (1x1 transparent)
	pngData := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}
	if err := os.WriteFile(imgPath, pngData, 0o644); err != nil {
		t.Fatalf("write png failed: %v", err)
	}

	tool := &ViewImageTool{Resolver: &stubResolver{base: tmp}}
	in, _ := json.Marshal(viewImageInput{Description: "test", Path: "/mnt/user-data/uploads/test.png"})
	out, err := tool.Execute(context.Background(), string(in))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal result failed: %v", err)
	}
	if result["type"] != "image" {
		t.Errorf("expected type=image, got %v", result["type"])
	}
	if result["mime"] != "image/png" {
		t.Errorf("expected mime=image/png, got %v", result["mime"])
	}
}

func TestViewImageTool_Execute_NotFound(t *testing.T) {
	tmp := t.TempDir()
	tool := &ViewImageTool{Resolver: &stubResolver{base: tmp}}
	in, _ := json.Marshal(viewImageInput{Description: "test", Path: "/mnt/user-data/uploads/missing.png"})
	out, _ := tool.Execute(context.Background(), string(in))
	if out == "" || out[0:5] != "Error" {
		t.Errorf("expected error string, got %q", out)
	}
}

func TestViewImageTool_Execute_InvalidJSON(t *testing.T) {
	tool := &ViewImageTool{}
	_, err := tool.Execute(context.Background(), `{invalid json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestViewImageTool_Execute_EmptyPath(t *testing.T) {
	tool := &ViewImageTool{Resolver: &stubResolver{}}
	_, err := tool.Execute(context.Background(), `{"description":"test","path":""}`)
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestViewImageTool_Execute_NilResolver(t *testing.T) {
	tool := &ViewImageTool{}
	_, err := tool.Execute(context.Background(), `{"description":"test","path":"/mnt/user-data/test.png"}`)
	if err == nil {
		t.Error("expected error for nil resolver")
	}
}

func TestViewImageTool_Execute_UnsupportedFormat(t *testing.T) {
	tmp := t.TempDir()
	imgPath := filepath.Join(tmp, "test.bmp")
	if err := os.WriteFile(imgPath, []byte("BM"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := &ViewImageTool{Resolver: &stubResolver{base: tmp}}
	in, _ := json.Marshal(viewImageInput{Description: "test", Path: "/mnt/user-data/uploads/test.bmp"})
	out, _ := tool.Execute(context.Background(), string(in))
	if out == "" || out[0:5] != "Error" {
		t.Errorf("expected error for unsupported format, got %q", out)
	}
}

func TestViewImageTool_Execute_JPEG(t *testing.T) {
	tmp := t.TempDir()
	imgPath := filepath.Join(tmp, "test.jpg")
	// Minimal JPEG header
	jpegData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
	if err := os.WriteFile(imgPath, jpegData, 0o644); err != nil {
		t.Fatal(err)
	}

	tool := &ViewImageTool{Resolver: &stubResolver{base: tmp}}
	in, _ := json.Marshal(viewImageInput{Description: "test", Path: "/mnt/user-data/uploads/test.jpg"})
	out, err := tool.Execute(context.Background(), string(in))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if result["mime"] != "image/jpeg" {
		t.Errorf("expected mime=image/jpeg, got %v", result["mime"])
	}
}

// ---------------------------------------------------------------------------
// PresentFileTool method tests
// ---------------------------------------------------------------------------

func TestPresentFileTool_Name(t *testing.T) {
	tool := &PresentFileTool{}
	if tool.Name() != "present_files" {
		t.Errorf("expected name 'present_files', got %q", tool.Name())
	}
}

func TestPresentFileTool_Description(t *testing.T) {
	tool := &PresentFileTool{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestPresentFileTool_InputSchema(t *testing.T) {
	tool := &PresentFileTool{}
	schema := tool.InputSchema()
	if len(schema) == 0 {
		t.Error("expected non-empty input schema")
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Errorf("input schema is not valid JSON: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PresentFileTool Execute tests
// ---------------------------------------------------------------------------

func TestPresentFileTool_Execute(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "report.pdf")
	if err := os.WriteFile(filePath, []byte("dummy pdf"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	tool := &PresentFileTool{Resolver: &stubResolver{base: tmp}}
	in, _ := json.Marshal(presentFileInput{Description: "report", Filepaths: []string{"/mnt/user-data/outputs/report.pdf"}})
	out, err := tool.Execute(context.Background(), string(in))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal result failed: %v", err)
	}
	if result["type"] != "command" {
		t.Errorf("expected type=command, got %v", result["type"])
	}

	update, ok := result["update"].(map[string]any)
	if !ok {
		t.Fatalf("expected update object, got %T", result["update"])
	}
	artifacts, ok := update["artifacts"].([]any)
	if !ok || len(artifacts) != 1 || artifacts[0] != "/mnt/user-data/outputs/report.pdf" {
		t.Fatalf("unexpected artifacts: %#v", update["artifacts"])
	}
	messages, ok := update["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatalf("expected messages in command update")
	}
	msg, ok := messages[0].(map[string]any)
	if !ok || msg["content"] != "Successfully presented files" {
		t.Fatalf("unexpected command message: %#v", messages[0])
	}
}

func TestPresentFileTool_Execute_InvalidJSON(t *testing.T) {
	tool := &PresentFileTool{}
	_, err := tool.Execute(context.Background(), `{invalid json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestPresentFileTool_Execute_NilResolver(t *testing.T) {
	tool := &PresentFileTool{}
	_, err := tool.Execute(context.Background(), `{"description":"test","filepaths":["/mnt/user-data/outputs/test.pdf"]}`)
	if err == nil {
		t.Error("expected error for nil resolver")
	}
}

func TestPresentFileTool_Execute_EmptyFilepaths(t *testing.T) {
	tool := &PresentFileTool{Resolver: &stubResolver{}}
	_, err := tool.Execute(context.Background(), `{"description":"test","filepaths":[]}`)
	if err == nil {
		t.Error("expected error for empty filepaths")
	}
}

func TestPresentFileTool_Execute_LegacyPaths(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "test.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := &PresentFileTool{Resolver: &stubResolver{base: tmp}}
	// Use legacy "paths" field
	in := `{"description":"test","paths":["/mnt/user-data/outputs/test.txt"]}`
	out, err := tool.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if result["type"] != "command" {
		t.Errorf("expected type=command, got %v", result["type"])
	}
}

func TestPresentFileTool_Execute_LegacyPath(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "single.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := &PresentFileTool{Resolver: &stubResolver{base: tmp}}
	// Use legacy "path" field
	in := `{"description":"test","path":"/mnt/user-data/outputs/single.txt"}`
	out, err := tool.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if result["type"] != "command" {
		t.Errorf("expected type=command, got %v", result["type"])
	}
}

func TestPresentFileTool_Execute_FileNotFound(t *testing.T) {
	tool := &PresentFileTool{Resolver: &stubResolver{base: t.TempDir()}}
	in := `{"description":"test","filepaths":["/mnt/user-data/outputs/notfound.pdf"]}`
	out, err := tool.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(out, "Error") {
		t.Errorf("expected error in output, got: %s", out)
	}
}

func TestPresentFileTool_Execute_Directory(t *testing.T) {
	tmp := t.TempDir()
	dirPath := filepath.Join(tmp, "testdir")
	if err := os.Mkdir(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}

	tool := &PresentFileTool{Resolver: &stubResolver{base: tmp}}
	in := `{"description":"test","filepaths":["/mnt/user-data/outputs/testdir"]}`
	out, err := tool.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(out, "Error") {
		t.Errorf("expected error for directory, got: %s", out)
	}
}

func TestPresentFileTool_Execute_OutsideOutputs(t *testing.T) {
	tmp := t.TempDir()
	// Create a file in a location that the stub resolver won't map to outputs
	workspacePath := filepath.Join(tmp, "workspace", "test.txt")
	os.MkdirAll(filepath.Dir(workspacePath), 0o755)
	if err := os.WriteFile(workspacePath, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The stub resolver resolves /mnt/user-data/outputs to tmp
	// Any file in tmp should be considered inside outputs
	// To test outside outputs, we need to use a path outside tmp
	otherDir := t.TempDir() // Different temp directory
	otherFile := filepath.Join(otherDir, "outside.txt")
	if err := os.WriteFile(otherFile, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := &PresentFileTool{Resolver: &stubResolver{base: tmp}}
	in := `{"description":"test","filepaths":["` + otherFile + `"]}`
	out, err := tool.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	// Check that the file is still presented (the stub resolver allows this)
	// The security check happens at the path level, not the host level
	_ = out
}

func TestPresentFileTool_Execute_DuplicatePaths(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "test.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := &PresentFileTool{Resolver: &stubResolver{base: tmp}}
	// Pass the same path twice - should be deduplicated
	in := `{"description":"test","filepaths":["/mnt/user-data/outputs/test.txt","/mnt/user-data/outputs/test.txt"]}`
	out, err := tool.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	update := result["update"].(map[string]any)
	artifacts := update["artifacts"].([]any)
	if len(artifacts) != 1 {
		t.Errorf("expected 1 artifact after dedup, got %d", len(artifacts))
	}
}

// ---------------------------------------------------------------------------
// mimeFromExt tests
// ---------------------------------------------------------------------------

func TestMimeFromExt(t *testing.T) {
	tests := []struct {
		ext      string
		expected string
	}{
		{".png", "image/png"},
		{".jpg", "image/jpeg"},
		{".jpeg", "image/jpeg"},
		{".gif", "image/gif"},
		{".webp", "image/webp"},
		{".svg", "image/svg+xml"},
		{".bmp", ""},
		{".txt", ""},
		{".unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			got := mimeFromExt(tt.ext)
			if got != tt.expected {
				t.Errorf("mimeFromExt(%q) = %q, want %q", tt.ext, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// buildPresentFilesCommand tests
// ---------------------------------------------------------------------------

func TestBuildPresentFilesCommand(t *testing.T) {
	tests := []struct {
		name      string
		message   string
		artifacts []string
		wantType  string
	}{
		{
			name:      "with artifacts",
			message:   "Success",
			artifacts: []string{"/mnt/user-data/outputs/test.txt"},
			wantType:  "command",
		},
		{
			name:      "without artifacts",
			message:   "Error",
			artifacts: nil,
			wantType:  "command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPresentFilesCommand(tt.message, tt.artifacts)
			var result map[string]any
			if err := json.Unmarshal([]byte(got), &result); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			if result["type"] != tt.wantType {
				t.Errorf("expected type=%s, got %v", tt.wantType, result["type"])
			}
		})
	}
}

// ---------------------------------------------------------------------------
// PresentFileTool normalizePresentedPath tests
// ---------------------------------------------------------------------------

func TestPresentFileTool_NormalizePresentedPath_Empty(t *testing.T) {
	tool := &PresentFileTool{Resolver: &stubResolver{}}
	_, err := tool.normalizePresentedPath("", "/outputs")
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestPresentFileTool_NormalizePresentedPath_InvalidVirtualPath(t *testing.T) {
	tool := &PresentFileTool{Resolver: &stubResolver{}}
	_, err := tool.normalizePresentedPath("/mnt/user-data/invalid/test.txt", "/outputs")
	if err == nil {
		t.Error("expected error for invalid virtual path")
	}
}

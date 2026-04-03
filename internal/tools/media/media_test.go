package media

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type stubResolver struct {
	base string
}

func (r *stubResolver) Resolve(vp string) (string, error) {
	return filepath.Join(r.base, filepath.Base(vp)), nil
}

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

func TestPresentFileTool_Execute(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "report.pdf")
	if err := os.WriteFile(filePath, []byte("dummy pdf"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	tool := &PresentFileTool{Resolver: &stubResolver{base: tmp}}
	in, _ := json.Marshal(presentFileInput{Description: "report", Path: "/mnt/user-data/outputs/report.pdf", Title: "Monthly Report"})
	out, err := tool.Execute(context.Background(), string(in))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal result failed: %v", err)
	}
	if result["type"] != "artifact" {
		t.Errorf("expected type=artifact, got %v", result["type"])
	}
	if result["title"] != "Monthly Report" {
		t.Errorf("expected title='Monthly Report', got %v", result["title"])
	}
}

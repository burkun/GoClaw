package builtin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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

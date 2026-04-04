package builtin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ViewImageTool reads an image file and returns base64-encoded data.
type ViewImageTool struct {
	// ViewedImages is a map to store viewed images for injection.
	ViewedImages map[string]ViewedImageData
}

// ViewedImageData holds base64 image data and MIME type.
type ViewedImageData struct {
	Base64   string `json:"base64"`
	MIMEType string `json:"mime_type"`
}

// NewViewImageTool creates a ViewImageTool.
func NewViewImageTool() *ViewImageTool {
	return &ViewImageTool{
		ViewedImages: make(map[string]ViewedImageData),
	}
}

// Name returns the tool name.
func (t *ViewImageTool) Name() string { return "view_image" }

// Description returns the tool description.
func (t *ViewImageTool) Description() string {
	return "View an image file. Reads the image and stores it for multimodal model injection."
}

// InputSchema returns the JSON schema for the tool input.
func (t *ViewImageTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Path to the image file"}
  },
  "required": ["path"]
}`)
}

type viewImageInput struct {
	Path string `json:"path"`
}

// Execute runs the tool.
func (t *ViewImageTool) Execute(ctx context.Context, input string) (string, error) {
	var in viewImageInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("view_image: invalid input: %w", err)
	}

	if strings.TrimSpace(in.Path) == "" {
		return "", fmt.Errorf("view_image: path is required")
	}

	imagePath := in.Path
	if !filepath.IsAbs(imagePath) {
		imagePath = filepath.Clean(imagePath)
	}

	// Verify file exists.
	info, err := os.Stat(imagePath)
	if err != nil {
		return "", fmt.Errorf("view_image: file not found: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("view_image: cannot view a directory")
	}

	// Check file size (limit to 20MB).
	const maxSize = 20 * 1024 * 1024
	if info.Size() > maxSize {
		return "", fmt.Errorf("view_image: file too large (max 20MB)")
	}

	// Determine MIME type.
	mimeType := guessMIMETypeFromPath(imagePath)
	if !isImageMIME(mimeType) {
		return "", fmt.Errorf("view_image: unsupported image format")
	}

	// Read and encode file.
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("view_image: read failed: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(data)

	// Store for injection.
	t.ViewedImages[imagePath] = ViewedImageData{
		Base64:   encoded,
		MIMEType: mimeType,
	}

	result := map[string]any{
		"success":    true,
		"path":       imagePath,
		"mime_type":  mimeType,
		"size_bytes": info.Size(),
		"message":    "Image loaded and ready for viewing. The image will be included in the next model interaction.",
	}

	out, _ := json.Marshal(result)
	return string(out), nil
}

// GetViewedImages returns the stored viewed images.
func (t *ViewImageTool) GetViewedImages() map[string]ViewedImageData {
	return t.ViewedImages
}

// ClearViewedImages clears stored images after injection.
func (t *ViewImageTool) ClearViewedImages() {
	t.ViewedImages = make(map[string]ViewedImageData)
}

func guessMIMETypeFromPath(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	case strings.HasSuffix(lower, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(lower, ".bmp"):
		return "image/bmp"
	case strings.HasSuffix(lower, ".ico"):
		return "image/x-icon"
	default:
		return "application/octet-stream"
	}
}

func isImageMIME(mime string) bool {
	return strings.HasPrefix(mime, "image/")
}

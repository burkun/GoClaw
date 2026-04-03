// Package media implements tools for handling media files like images.
package media

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tools "github.com/bookerbai/goclaw/internal/tools"
)

// PathResolver translates virtual /mnt/user-data/* paths to host paths.
type PathResolver interface {
	Resolve(virtualPath string) (string, error)
}

// ---------------------------------------------------------------------------
// ViewImageTool
// ---------------------------------------------------------------------------

// ViewImageTool encodes an image file as base64 for multimodal model input.
// Implements tools.Tool.
type ViewImageTool struct {
	Resolver PathResolver
}

type viewImageInput struct {
	Description string `json:"description"`
	Path        string `json:"path"`
}

func (t *ViewImageTool) Name() string { return "view_image" }

func (t *ViewImageTool) Description() string {
	return `Read an image file and return its base64-encoded content for model viewing.
Path must be an absolute virtual path under /mnt/user-data/.`
}

func (t *ViewImageTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["description", "path"],
  "properties": {
    "description": {"type": "string", "description": "Explain why you are viewing this image."},
    "path":        {"type": "string", "description": "Absolute virtual path to the image file."}
  }
}`)
}

// Execute reads and base64-encodes the image.
func (t *ViewImageTool) Execute(_ context.Context, input string) (string, error) {
	var in viewImageInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("view_image: invalid input JSON: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return "", fmt.Errorf("view_image: path is required")
	}
	if t.Resolver == nil {
		return "", fmt.Errorf("view_image: resolver is required")
	}

	hostPath, err := t.Resolver.Resolve(in.Path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err), nil
	}

	data, err := os.ReadFile(hostPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("Error: file not found: %s", in.Path), nil
		}
		if os.IsPermission(err) {
			return fmt.Sprintf("Error: permission denied: %s", in.Path), nil
		}
		return fmt.Sprintf("Error: %v", err), nil
	}

	ext := strings.ToLower(filepath.Ext(in.Path))
	mime := mimeFromExt(ext)
	if mime == "" {
		return fmt.Sprintf("Error: unsupported image format: %s", ext), nil
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	result := map[string]any{
		"type": "image",
		"data": encoded,
		"mime": mime,
		"path": in.Path,
	}
	b, _ := json.Marshal(result)
	return string(b), nil
}

func mimeFromExt(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	default:
		return ""
	}
}

var _ tools.Tool = (*ViewImageTool)(nil)

// ---------------------------------------------------------------------------
// PresentFileTool
// ---------------------------------------------------------------------------

// PresentFileTool returns an artifact reference for a file to be displayed to the user.
// Implements tools.Tool.
type PresentFileTool struct {
	Resolver PathResolver
}

type presentFileInput struct {
	Description string `json:"description"`
	Path        string `json:"path"`
	Title       string `json:"title,omitempty"`
}

func (t *PresentFileTool) Name() string { return "present_file" }

func (t *PresentFileTool) Description() string {
	return `Present a file to the user as a downloadable artifact.
Path must be an absolute virtual path under /mnt/user-data/.
This does NOT read the file contents; it only returns a reference for frontend rendering.`
}

func (t *PresentFileTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["description", "path"],
  "properties": {
    "description": {"type": "string", "description": "Explain what this file is."},
    "path":        {"type": "string", "description": "Absolute virtual path to the file."},
    "title":       {"type": "string", "description": "Optional display title for the artifact."}
  }
}`)
}

// Execute returns an artifact reference JSON.
func (t *PresentFileTool) Execute(_ context.Context, input string) (string, error) {
	var in presentFileInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("present_file: invalid input JSON: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return "", fmt.Errorf("present_file: path is required")
	}
	if t.Resolver == nil {
		return "", fmt.Errorf("present_file: resolver is required")
	}

	hostPath, err := t.Resolver.Resolve(in.Path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err), nil
	}

	info, err := os.Stat(hostPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("Error: file not found: %s", in.Path), nil
		}
		return fmt.Sprintf("Error: %v", err), nil
	}

	title := in.Title
	if title == "" {
		title = filepath.Base(in.Path)
	}

	result := map[string]any{
		"type":  "artifact",
		"path":  in.Path,
		"title": title,
		"size":  info.Size(),
	}
	b, _ := json.Marshal(result)
	return string(b), nil
}

var _ tools.Tool = (*PresentFileTool)(nil)

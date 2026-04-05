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

// PresentFileTool returns artifact references for files to be displayed to the user.
// Implements tools.Tool.
type PresentFileTool struct {
	Resolver PathResolver
}

type presentFileInput struct {
	Description string   `json:"description"`
	Filepaths   []string `json:"filepaths,omitempty"` // DeerFlow-compatible input
	Path        string   `json:"path,omitempty"`      // Legacy single-file input
	Paths       []string `json:"paths,omitempty"`     // Legacy multi-file input
}

func (t *PresentFileTool) Name() string { return "present_files" }

func (t *PresentFileTool) Description() string {
	return `Make files visible to the user via artifacts state updates.
Only files under /mnt/user-data/outputs are allowed.
Use "filepaths" (preferred), or legacy "paths"/"path" for compatibility.`
}

func (t *PresentFileTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["description", "filepaths"],
  "properties": {
    "description": {"type": "string", "description": "Explain what files are being presented."},
    "filepaths":   {"type": "array", "items": {"type": "string"}, "description": "Absolute paths of files to present. Only /mnt/user-data/outputs is allowed."},
    "paths":       {"type": "array", "items": {"type": "string"}, "description": "Legacy alias for filepaths."},
    "path":        {"type": "string", "description": "Legacy single-file alias for filepaths."}
  }
}`)
}

// Execute returns a DeerFlow-style command payload.
// The middleware adapter consumes this payload and applies state updates via reducers.
func (t *PresentFileTool) Execute(_ context.Context, input string) (string, error) {
	var in presentFileInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("present_files: invalid input JSON: %w", err)
	}
	if t.Resolver == nil {
		return "", fmt.Errorf("present_files: resolver is required")
	}

	filepaths := make([]string, 0, len(in.Filepaths)+len(in.Paths)+1)
	filepaths = append(filepaths, in.Filepaths...)
	if len(filepaths) == 0 {
		filepaths = append(filepaths, in.Paths...)
	}
	if len(filepaths) == 0 && strings.TrimSpace(in.Path) != "" {
		filepaths = append(filepaths, strings.TrimSpace(in.Path))
	}
	if len(filepaths) == 0 {
		return "", fmt.Errorf("present_files: at least one filepath is required")
	}

	outputsRootHost, err := t.Resolver.Resolve("/mnt/user-data/outputs")
	if err != nil {
		return buildPresentFilesCommand("Error: thread outputs path is not available", nil), nil
	}
	outputsRootHost, err = filepath.Abs(outputsRootHost)
	if err != nil {
		return buildPresentFilesCommand("Error: failed to resolve outputs root", nil), nil
	}

	normalized := make([]string, 0, len(filepaths))
	seen := make(map[string]bool)
	for _, raw := range filepaths {
		path, normalizeErr := t.normalizePresentedPath(raw, outputsRootHost)
		if normalizeErr != nil {
			return buildPresentFilesCommand("Error: "+normalizeErr.Error(), nil), nil
		}
		if !seen[path] {
			seen[path] = true
			normalized = append(normalized, path)
		}
	}

	if len(normalized) == 0 {
		return buildPresentFilesCommand("Error: no valid files to present", nil), nil
	}

	return buildPresentFilesCommand("Successfully presented files", normalized), nil
}

func (t *PresentFileTool) normalizePresentedPath(inputPath, outputsRootHost string) (string, error) {
	inputPath = strings.TrimSpace(inputPath)
	if inputPath == "" {
		return "", fmt.Errorf("empty filepath")
	}

	var hostPath string
	if strings.HasPrefix(inputPath, "/mnt/user-data/") {
		resolved, err := t.Resolver.Resolve(inputPath)
		if err != nil {
			return "", fmt.Errorf("invalid virtual path: %s", inputPath)
		}
		hostPath = resolved
	} else {
		hostPath = inputPath
	}

	hostPath, err := filepath.Abs(hostPath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %s", inputPath)
	}

	info, err := os.Stat(hostPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file not found: %s", inputPath)
		}
		return "", fmt.Errorf("cannot access file: %s", inputPath)
	}
	if info.IsDir() {
		return "", fmt.Errorf("only files can be presented: %s", inputPath)
	}

	rel, err := filepath.Rel(outputsRootHost, hostPath)
	if err != nil {
		return "", fmt.Errorf("failed to normalize path: %s", inputPath)
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("only files in /mnt/user-data/outputs can be presented: %s", inputPath)
	}

	return "/mnt/user-data/outputs/" + filepath.ToSlash(rel), nil
}

func buildPresentFilesCommand(message string, artifacts []string) string {
	update := map[string]any{
		"messages": []map[string]any{{
			"type":    "tool",
			"content": message,
		}},
	}
	if len(artifacts) > 0 {
		update["artifacts"] = artifacts
	}

	result := map[string]any{
		"type":   "command",
		"update": update,
	}
	b, _ := json.Marshal(result)
	return string(b)
}

var _ tools.Tool = (*PresentFileTool)(nil)

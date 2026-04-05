package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// PresentFileTool marks a file as a final artifact for download.
type PresentFileTool struct {
	ThreadID    string
	OutputsPath string
}

// NewPresentFileTool creates a PresentFileTool.
func NewPresentFileTool(threadID, outputsPath string) *PresentFileTool {
	return &PresentFileTool{ThreadID: threadID, OutputsPath: outputsPath}
}

// Name returns the tool name.
func (t *PresentFileTool) Name() string { return "present_files" }

// Description returns the tool description.
func (t *PresentFileTool) Description() string {
	return "Present a file as a downloadable artifact. Copies the file to the outputs directory and returns the download URL."
}

// InputSchema returns the JSON schema for the tool input.
func (t *PresentFileTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Path to the source file"},
    "filename": {"type": "string", "description": "Optional filename for the artifact (defaults to source filename)"},
    "description": {"type": "string", "description": "Optional description of the artifact"}
  },
  "required": ["path"]
}`)
}

type presentFileInput struct {
	Path        string `json:"path"`
	Filename    string `json:"filename,omitempty"`
	Description string `json:"description,omitempty"`
}

// Execute runs the tool.
func (t *PresentFileTool) Execute(ctx context.Context, input string) (string, error) {
	var in presentFileInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("present_file: invalid input: %w", err)
	}

	if strings.TrimSpace(in.Path) == "" {
		return "", fmt.Errorf("present_file: path is required")
	}

	sourcePath := in.Path
	if !filepath.IsAbs(sourcePath) {
		sourcePath = filepath.Clean(sourcePath)
	}

	// Verify source exists.
	info, err := os.Stat(sourcePath)
	if err != nil {
		return "", fmt.Errorf("present_file: source file not found: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("present_file: cannot present a directory")
	}

	// Determine output filename.
	outputFilename := in.Filename
	if outputFilename == "" {
		outputFilename = filepath.Base(sourcePath)
	}
	outputFilename = sanitizeFilename(outputFilename)

	// Ensure outputs directory exists.
	if err := os.MkdirAll(t.OutputsPath, 0755); err != nil {
		return "", fmt.Errorf("present_file: create outputs dir: %w", err)
	}

	destPath := filepath.Join(t.OutputsPath, outputFilename)

	// Copy file.
	if err := copyFile(sourcePath, destPath); err != nil {
		return "", fmt.Errorf("present_file: copy failed: %w", err)
	}

	// Generate artifact URL.
	artifactURL := fmt.Sprintf("/api/threads/%s/artifacts/%s", t.ThreadID, outputFilename)

	result := map[string]any{
		"success":      true,
		"artifact_url": artifactURL,
		"filename":     outputFilename,
		"size_bytes":   info.Size(),
	}
	if in.Description != "" {
		result["description"] = in.Description
	}

	out, _ := json.Marshal(result)
	return string(out), nil
}

func sanitizeFilename(name string) string {
	// Remove path components.
	name = filepath.Base(name)

	// Replace unsafe characters.
	unsafe := []string{"/", "\\", "..", ":", "*", "?", "\"", "<", ">", "|"}
	for _, ch := range unsafe {
		name = strings.ReplaceAll(name, ch, "_")
	}

	if name == "" || name == "." {
		name = "artifact"
	}

	return name
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return out.Sync()
}

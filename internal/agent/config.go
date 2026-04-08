package agent

import (
	"github.com/cloudwego/eino/schema"
)

// UploadedFile represents a file uploaded to the agent.
type UploadedFile struct {
	Name        string `json:"name"`
	VirtualPath string `json:"virtual_path"`
	MIMEType    string `json:"mime_type,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
}

// ViewedImageData represents image data viewed by the agent.
type ViewedImageData struct {
	Base64   string `json:"base64"`
	MIMEType string `json:"mime_type"`
}

// ThreadDataState holds data paths for a thread.
type ThreadDataState struct {
	WorkspacePath string `json:"workspace_path,omitempty"`
	UploadsPath   string `json:"uploads_path,omitempty"`
	OutputsPath   string `json:"outputs_path,omitempty"`
}

// SandboxState holds sandbox-specific state.
type SandboxState struct {
	SandboxID string `json:"sandbox_id,omitempty"`
}

// ThreadState represents the full state of a conversation thread.
type ThreadState struct {
	Messages      []*schema.Message          `json:"messages"`
	Sandbox       *SandboxState              `json:"sandbox,omitempty"`
	ThreadData    *ThreadDataState           `json:"thread_data,omitempty"`
	Title         string                     `json:"title,omitempty"`
	Artifacts     []string                   `json:"artifacts,omitempty"`
	Todos         []map[string]any           `json:"todos,omitempty"`
	UploadedFiles []UploadedFile             `json:"uploaded_files,omitempty"`
	ViewedImages  map[string]ViewedImageData `json:"viewed_images,omitempty"`
}

// RunConfig holds configuration for a single agent run.
type RunConfig struct {
	ThreadID               string
	ModelName              string
	ThinkingEnabled        bool
	IsPlanMode             bool
	SubagentEnabled        bool
	MaxConcurrentSubagents int
	CheckpointID           string
	AgentName              string
	// RunID is set by the gateway and used to tag events for consistent tracking.
	RunID string
	// AvailableSkills is an optional set of skill names to make available.
	// If nil, all enabled skills are available.
	AvailableSkills map[string]bool
}

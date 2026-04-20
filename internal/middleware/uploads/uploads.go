// Package uploads implements UploadsMiddleware which scans the uploads directory
// and injects the list of uploaded files into state.Extra["uploads"] and into
// the last human message as <uploaded_files> XML block.
package uploads

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"goclaw/internal/middleware"
)

// UploadsMiddleware injects uploaded file list into state.Extra["uploads"].
// Depends on ThreadDataMiddleware having run first to populate state.Extra["uploads_path"].
type UploadsMiddleware struct {
	middleware.MiddlewareWrapper
}

// New creates an UploadsMiddleware.
func New() *UploadsMiddleware {
	return &UploadsMiddleware{}
}

// Name implements middleware.Middleware.
func (m *UploadsMiddleware) Name() string {
	return "UploadsMiddleware"
}

// BeforeModel scans uploads directory and populates state.Extra["uploads"],
// and injects <uploaded_files> XML block into the last human message.
func (m *UploadsMiddleware) BeforeModel(_ context.Context, state *middleware.State) error {
	uploadsPath, ok := state.Extra["uploads_path"].(string)
	if !ok || uploadsPath == "" {
		return nil
	}

	entries, err := os.ReadDir(uploadsPath)
	if err != nil {
		if os.IsNotExist(err) {
			state.Extra["uploads"] = []string{}
			return nil
		}
		return err
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, filepath.Join("/mnt/user-data/uploads", e.Name()))
		}
	}
	if files == nil {
		files = []string{}
	}
	state.Extra["uploads"] = files

	// Inject <uploaded_files> XML block into the last human message.
	if len(files) > 0 {
		injectUploadedFilesToMessage(state.Messages, files)
	}
	return nil
}

// injectUploadedFilesToMessage finds the last human message and prepends
// the <uploaded_files> XML block to its content.
func injectUploadedFilesToMessage(messages []map[string]any, files []string) {
	// Find the last human message.
	var lastHumanIdx = -1
	for i := len(messages) - 1; i >= 0; i-- {
		role, _ := messages[i]["role"].(string)
		if role == "user" || role == "human" {
			lastHumanIdx = i
			break
		}
	}
	if lastHumanIdx == -1 {
		return
	}

	// Build the <uploaded_files> XML block.
	var sb strings.Builder
	sb.WriteString("<uploaded_files>\n")
	for _, f := range files {
		sb.WriteString(fmt.Sprintf("  <file path=\"%s\" />\n", f))
	}
	sb.WriteString("</uploaded_files>\n\n")
	filesMsg := sb.String()

	// Prepend to the message content.
	msg := messages[lastHumanIdx]
	content, _ := msg["content"].(string)
	msg["content"] = filesMsg + content
}

// AfterModel is a no-op.
func (m *UploadsMiddleware) AfterModel(_ context.Context, _ *middleware.State, _ *middleware.Response) error {
	return nil
}

var _ middleware.Middleware = (*UploadsMiddleware)(nil)

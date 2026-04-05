package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/bookerbai/goclaw/internal/config"
	"github.com/bookerbai/goclaw/internal/tools/conversion"
)

// UploadsHandler serves POST /api/threads/:thread_id/uploads.
type UploadsHandler struct {
	cfg *config.AppConfig
}

// NewUploadsHandler creates an UploadsHandler backed by cfg.
func NewUploadsHandler(cfg *config.AppConfig) *UploadsHandler {
	return &UploadsHandler{cfg: cfg}
}

// UploadedFileInfo describes a single successfully uploaded file.
type UploadedFileInfo struct {
	// Filename is the sanitised on-disk name of the saved file.
	Filename string `json:"filename"`
	// Size is the file size in bytes.
	Size int64 `json:"size"`
	// VirtualPath is the in-sandbox path visible to the agent
	// (e.g. /mnt/user-data/uploads/<filename>).
	VirtualPath string `json:"virtual_path"`
	// HostPath is the real filesystem path on the host
	// (e.g. .goclaw/threads/<thread_id>/user-data/uploads/<filename>).
	HostPath string `json:"host_path"`
	// ConvertedToMarkdown indicates if the file was converted to Markdown.
	ConvertedToMarkdown bool `json:"converted_to_markdown,omitempty"`
	// MarkdownPath is the virtual path to the converted Markdown file.
	MarkdownPath string `json:"markdown_path,omitempty"`
}

// UploadResponse is returned on a successful upload request.
type UploadResponse struct {
	Success bool               `json:"success"`
	Files   []UploadedFileInfo `json:"files"`
	Message string             `json:"message"`
}

// unsafeFilenameRe rejects filenames with path separators or null bytes.
var unsafeFilenameRe = regexp.MustCompile(`[/\\` + "\x00" + `]`)

var threadIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func validateThreadID(threadID string) error {
	if !threadIDRe.MatchString(threadID) {
		return fmt.Errorf("invalid thread_id")
	}
	return nil
}

// sanitiseFilename strips directory components and checks for unsafe characters.
// Returns an error when the resulting name is empty or contains dangerous sequences.
func sanitiseFilename(name string) (string, error) {
	// Strip any directory path the client might have sent.
	base := filepath.Base(name)
	// Reject names that are only dots (e.g. "." or "..").
	if strings.Trim(base, ".") == "" {
		return "", fmt.Errorf("unsafe filename: %q", name)
	}
	if unsafeFilenameRe.MatchString(base) {
		return "", fmt.Errorf("unsafe filename: %q", name)
	}
	return base, nil
}

// UploadFiles handles POST /api/threads/:thread_id/uploads.
//
// Accepts a multipart/form-data request with one or more files under the
// field name "files".  Each file is written to the thread's uploads directory.
//
// Virtual path mapping (mirrors DeerFlow):
//
//	host:    .goclaw/threads/<thread_id>/user-data/uploads/<filename>
//	sandbox: /mnt/user-data/uploads/<filename>
func (h *UploadsHandler) UploadFiles(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "thread_id is required"})
		return
	}

	if err := validateThreadID(threadID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	form, err := c.MultipartForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to parse multipart form: " + err.Error()})
		return
	}

	files := form.File["files"]
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no files provided"})
		return
	}

	uploadsDir := filepath.Join(".goclaw", "threads", threadID, "user-data", "uploads")
	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create uploads directory"})
		return
	}

	var uploaded []UploadedFileInfo
	converter := conversion.DefaultConverter()
	ctx := context.Background()

	for _, fh := range files {
		safeName, err := sanitiseFilename(fh.Filename)
		if err != nil {
			continue
		}

		src, err := fh.Open()
		if err != nil {
			continue
		}

		hostPath := filepath.Join(uploadsDir, safeName)
		dst, err := os.OpenFile(hostPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			src.Close()
			continue
		}

		written, copyErr := io.Copy(dst, src)
		closeErr := dst.Close()
		src.Close()
		if copyErr != nil || closeErr != nil {
			_ = os.Remove(hostPath)
			continue
		}

		_ = os.Chmod(hostPath, 0o666)
		virtualPath := "/mnt/user-data/uploads/" + safeName

		info := UploadedFileInfo{
			Filename:    safeName,
			Size:        written,
			VirtualPath: virtualPath,
			HostPath:    hostPath,
		}

		// Try to convert document to Markdown
		if converter.NeedsConversion(safeName) {
			mdContent, convErr := converter.ConvertToMarkdown(ctx, hostPath)
			if convErr == nil && strings.TrimSpace(mdContent) != "" {
				// Save converted Markdown file
				mdFilename := safeName + ".md"
				mdHostPath := filepath.Join(uploadsDir, mdFilename)
				if writeErr := os.WriteFile(mdHostPath, []byte(mdContent), 0o644); writeErr == nil {
					info.ConvertedToMarkdown = true
					info.MarkdownPath = virtualPath + ".md"
				}
			}
		}

		uploaded = append(uploaded, info)
	}

	c.JSON(http.StatusOK, UploadResponse{
		Success: true,
		Files:   uploaded,
		Message: fmt.Sprintf("Successfully uploaded %d file(s)", len(uploaded)),
	})
}

// ListUploadedFiles handles GET /api/threads/:thread_id/uploads/list.
// Returns a list of all files in the thread's uploads directory.
func (h *UploadsHandler) ListUploadedFiles(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "thread_id is required"})
		return
	}

	if err := validateThreadID(threadID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	uploadsDir := filepath.Join(".goclaw", "threads", threadID, "user-data", "uploads")
	entries, err := os.ReadDir(uploadsDir)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusOK, gin.H{"files": []interface{}{}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read uploads directory"})
		return
	}

	var files []map[string]interface{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, map[string]interface{}{
			"filename":     entry.Name(),
			"size":         info.Size(),
			"virtual_path": "/mnt/user-data/uploads/" + entry.Name(),
			"modified_at":  info.ModTime().Unix(),
		})
	}

	c.JSON(http.StatusOK, gin.H{"files": files, "count": len(files)})
}

// DeleteUploadedFile handles DELETE /api/threads/:thread_id/uploads/:filename.
// Removes a specific file from the thread's uploads directory.
func (h *UploadsHandler) DeleteUploadedFile(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "thread_id is required"})
		return
	}

	if err := validateThreadID(threadID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	filename := c.Param("filename")
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "filename is required"})
		return
	}

	safeName, err := sanitiseFilename(filename)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	uploadsDir := filepath.Join(".goclaw", "threads", threadID, "user-data", "uploads")
	hostPath := filepath.Join(uploadsDir, safeName)

	// Also delete converted markdown if exists
	mdPath := hostPath + ".md"
	_ = os.Remove(mdPath)

	if err := os.Remove(hostPath); err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete file"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"filename": safeName,
		"message":  "File deleted successfully",
	})
}

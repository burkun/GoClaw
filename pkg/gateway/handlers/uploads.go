package handlers

import (
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/bookerbai/goclaw/internal/config"
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
}

// UploadResponse is returned on a successful upload request.
type UploadResponse struct {
	Success bool               `json:"success"`
	Files   []UploadedFileInfo `json:"files"`
	Message string             `json:"message"`
}

// unsafeFilenameRe rejects filenames with path separators or null bytes.
var unsafeFilenameRe = regexp.MustCompile(`[/\\` + "\x00" + `]`)

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

	// TODO: Validate threadID against a UUID / slug format to prevent directory traversal.

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

	// TODO: Resolve the host-side uploads directory from config or a paths helper:
	//   uploadsDir := filepath.Join(h.cfg.DataDir, "threads", threadID, "user-data", "uploads")
	//   os.MkdirAll(uploadsDir, 0o755)

	var uploaded []UploadedFileInfo

	for _, fh := range files {
		safeName, err := sanitiseFilename(fh.Filename)
		if err != nil {
			// Skip files with unsafe names rather than aborting the whole request.
			// TODO: log warning with file name and thread ID.
			continue
		}

		// TODO: Open fh via fh.Open(), read content, write to uploadsDir/safeName.
		//   If the active sandbox is non-local (Docker), also push the bytes into
		//   the sandbox via sandboxProvider.Get(threadID).UpdateFile(virtualPath, content).

		// TODO: Set world-writable permissions on the saved file so the Docker
		//   sandbox runtime (different UID) can overwrite it if needed:
		//   os.Chmod(hostPath, 0o666)

		virtualPath := "/mnt/user-data/uploads/" + safeName
		hostPath := fmt.Sprintf(".goclaw/threads/%s/user-data/uploads/%s", threadID, safeName)

		uploaded = append(uploaded, UploadedFileInfo{
			Filename:    safeName,
			Size:        fh.Size,
			VirtualPath: virtualPath,
			HostPath:    hostPath,
		})
	}

	c.JSON(http.StatusOK, UploadResponse{
		Success: true,
		Files:   uploaded,
		Message: fmt.Sprintf("Successfully uploaded %d file(s)", len(uploaded)),
	})
}

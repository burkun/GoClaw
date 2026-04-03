// Artifacts handler exposes GET /api/threads/:thread_id/artifacts/:path
// for downloading files produced by the agent.
package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/bookerbai/goclaw/internal/config"
)

// ArtifactsHandler serves artifact downloads.
type ArtifactsHandler struct {
	cfg     *config.AppConfig
	baseDir string
}

// NewArtifactsHandler creates an ArtifactsHandler.
// baseDir defaults to ".goclaw/threads" if empty.
func NewArtifactsHandler(cfg *config.AppConfig, baseDir string) *ArtifactsHandler {
	if baseDir == "" {
		baseDir = ".goclaw/threads"
	}
	return &ArtifactsHandler{cfg: cfg, baseDir: baseDir}
}

// GetArtifact streams a file from the thread's user-data directory.
// Path param "path" is the virtual suffix after /mnt/user-data/.
func (h *ArtifactsHandler) GetArtifact(c *gin.Context) {
	threadID := c.Param("thread_id")
	artifactPath := c.Param("path")

	if err := validateThreadID(threadID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid thread_id"})
		return
	}

	// Reject path traversal.
	if strings.Contains(artifactPath, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}

	// Map virtual path to host path.
	hostPath := filepath.Join(h.baseDir, threadID, "user-data", artifactPath)
	absPath, err := filepath.Abs(hostPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}

	// Verify within baseDir.
	absBase, _ := filepath.Abs(h.baseDir)
	if !strings.HasPrefix(absPath, absBase) {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "artifact not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if info.IsDir() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is a directory"})
		return
	}

	c.File(absPath)
}

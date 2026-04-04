// Artifacts handler exposes GET /api/threads/:thread_id/artifacts/:path
// for downloading files produced by the agent.
package handlers

import (
	"archive/zip"
	"io"
	"mime"
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
	artifactPath := strings.TrimPrefix(c.Param("path"), "/")
	download := strings.EqualFold(c.Query("download"), "true")
	listDir := strings.EqualFold(c.Query("list"), "true")

	if err := validateThreadID(threadID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid thread_id"})
		return
	}

	// Reject path traversal.
	if strings.Contains(artifactPath, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}

	if strings.Contains(artifactPath, ".skill/") {
		h.serveSkillArchiveEntry(c, threadID, artifactPath, download)
		return
	}

	absPath, err := h.resolveArtifactHostPath(threadID, artifactPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
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
		if !listDir {
			c.JSON(http.StatusBadRequest, gin.H{"error": "path is a directory"})
			return
		}
		entries, err := os.ReadDir(absPath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		items := make([]map[string]any, 0, len(entries))
		for _, e := range entries {
			fi, _ := e.Info()
			item := map[string]any{"name": e.Name(), "is_dir": e.IsDir()}
			if fi != nil {
				item["size"] = fi.Size()
				item["modified"] = fi.ModTime().Unix()
			}
			items = append(items, item)
		}
		c.JSON(http.StatusOK, gin.H{"path": artifactPath, "items": items})
		return
	}

	if download {
		c.FileAttachment(absPath, filepath.Base(absPath))
		return
	}
	c.File(absPath)
}

func (h *ArtifactsHandler) resolveArtifactHostPath(threadID, artifactPath string) (string, error) {
	hostPath := filepath.Join(h.baseDir, threadID, "user-data", artifactPath)
	absPath, err := filepath.Abs(hostPath)
	if err != nil {
		return "", err
	}
	absBase, err := filepath.Abs(h.baseDir)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absPath, absBase) {
		return "", os.ErrPermission
	}
	return absPath, nil
}

func (h *ArtifactsHandler) serveSkillArchiveEntry(c *gin.Context, threadID, artifactPath string, download bool) {
	idx := strings.Index(artifactPath, ".skill/")
	if idx <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid .skill path"})
		return
	}

	archiveRel := artifactPath[:idx+len(".skill")]
	innerPath := strings.TrimPrefix(artifactPath[idx+len(".skill/"):], "/")
	if innerPath == "" || strings.Contains(innerPath, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid skill internal path"})
		return
	}

	archiveAbs, err := h.resolveArtifactHostPath(threadID, archiveRel)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}
	if _, err := os.Stat(archiveAbs); err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "artifact not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	data, err := extractFileFromSkillArchive(archiveAbs, innerPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "artifact not found"})
		return
	}

	contentType := detectContentTypeFromName(innerPath)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Header("Content-Type", contentType)

	if download || isActiveContentType(contentType) {
		c.Header("Content-Disposition", "attachment; filename="+filepath.Base(innerPath))
	}
	c.Data(http.StatusOK, contentType, data)
}

func extractFileFromSkillArchive(archivePath, innerPath string) ([]byte, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	cleanWanted := filepath.ToSlash(filepath.Clean(innerPath))
	cleanWanted = strings.TrimPrefix(cleanWanted, "/")

	for _, f := range zr.File {
		name := filepath.ToSlash(filepath.Clean(f.Name))
		name = strings.TrimPrefix(name, "/")
		if name != cleanWanted {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, 20*1024*1024))
	}
	return nil, os.ErrNotExist
}

func detectContentTypeFromName(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return ""
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return ""
}

func isActiveContentType(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.HasPrefix(ct, "text/html") || strings.HasPrefix(ct, "image/svg+xml")
}

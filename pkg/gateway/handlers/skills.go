// Skills handler exposes GET /api/skills and PUT /api/skills/:name routes.
package handlers

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/bookerbai/goclaw/internal/config"
	"github.com/bookerbai/goclaw/internal/skills"
)

// SkillsHandler handles skill listing and updates.
type SkillsHandler struct {
	cfg      *config.AppConfig
	registry *skills.Registry
}

// NewSkillsHandler creates a SkillsHandler.
func NewSkillsHandler(cfg *config.AppConfig, registry *skills.Registry) *SkillsHandler {
	return &SkillsHandler{cfg: cfg, registry: registry}
}

// SkillSummary is a JSON-friendly summary of a skill.
type SkillSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Enabled     bool     `json:"enabled"`
	Category    string   `json:"category,omitempty"`
	Tools       []string `json:"allowed_tools,omitempty"`
}

// ListSkills returns all registered skills.
func (h *SkillsHandler) ListSkills(c *gin.Context) {
	if h.registry == nil {
		c.JSON(http.StatusOK, gin.H{"skills": []SkillSummary{}})
		return
	}

	all := h.registry.List()
	summaries := make([]SkillSummary, 0, len(all))
	for _, sk := range all {
		summaries = append(summaries, SkillSummary{
			Name:        sk.Metadata.Name,
			Description: sk.Metadata.Description,
			Enabled:     sk.Metadata.Enabled,
			Category:    sk.Metadata.Category,
			Tools:       sk.Metadata.AllowedTools,
		})
	}
	c.JSON(http.StatusOK, gin.H{"skills": summaries})
}

// GetSkill returns details for a single skill.
func (h *SkillsHandler) GetSkill(c *gin.Context) {
	name := c.Param("name")
	if h.registry == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "skill not found"})
		return
	}
	sk := h.registry.GetByName(name)
	if sk == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "skill not found"})
		return
	}
	c.JSON(http.StatusOK, SkillSummary{
		Name:        sk.Metadata.Name,
		Description: sk.Metadata.Description,
		Enabled:     sk.Metadata.Enabled,
		Category:    sk.Metadata.Category,
		Tools:       sk.Metadata.AllowedTools,
	})
}

// UpdateSkill enables or disables a skill.
func (h *SkillsHandler) UpdateSkill(c *gin.Context) {
	name := c.Param("name")
	if h.registry == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "skill not found"})
		return
	}
	sk := h.registry.GetByName(name)
	if sk == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "skill not found"})
		return
	}

	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if req.Enabled != nil {
		sk.Metadata.Enabled = *req.Enabled
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

// SkillInstallRequest is the payload for POST /api/skills/install.
type SkillInstallRequest struct {
	ThreadID string `json:"thread_id"`
	Path     string `json:"path"`
}

// InstallSkill installs a .skill archive from a thread artifact path.
func (h *SkillsHandler) InstallSkill(c *gin.Context) {
	var req SkillInstallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if err := validateThreadID(strings.TrimSpace(req.ThreadID)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid thread_id"})
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
		return
	}

	archivePath, err := resolveThreadArtifactPath(req.ThreadID, req.Path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !strings.HasSuffix(strings.ToLower(archivePath), ".skill") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path must point to a .skill archive"})
		return
	}
	if _, err := os.Stat(archivePath); err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "skill archive not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	skillName, err := readSkillNameFromArchive(archivePath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	extCfg, err := h.loadExtensions()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if st, ok := extCfg.Skills[skillName]; ok && st.Enabled {
		c.JSON(http.StatusConflict, gin.H{"error": "skill already exists"})
		return
	}

	skillsRoot := "skills"
	if h.cfg != nil && strings.TrimSpace(h.cfg.Skills.Path) != "" {
		skillsRoot = strings.TrimSpace(h.cfg.Skills.Path)
	}
	targetDir := filepath.Join(skillsRoot, "custom", skillName)
	if _, err := os.Stat(targetDir); err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "skill already exists"})
		return
	}
	if err := safeExtractSkillArchive(archivePath, targetDir); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if extCfg.Skills == nil {
		extCfg.Skills = map[string]config.SkillStateConfig{}
	}
	extCfg.Skills[skillName] = config.SkillStateConfig{Enabled: true}
	if err := h.saveExtensions(extCfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"skill_name": skillName,
		"message":    "skill installed",
	})
}

func resolveThreadArtifactPath(threadID, pathValue string) (string, error) {
	p := strings.TrimSpace(pathValue)
	if p == "" {
		return "", fmt.Errorf("path is required")
	}
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "mnt/user-data/")
	p = strings.TrimPrefix(p, "/mnt/user-data/")

	if strings.Contains(p, "..") {
		return "", fmt.Errorf("invalid path")
	}

	baseDir := filepath.Join(".goclaw", "threads", threadID, "user-data")
	candidate := filepath.Join(baseDir, p)
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("invalid path")
	}
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("invalid path")
	}
	if !strings.HasPrefix(absCandidate, absBase) {
		return "", fmt.Errorf("invalid path")
	}
	return absCandidate, nil
}

func readSkillNameFromArchive(archivePath string) (string, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", fmt.Errorf("invalid skill archive")
	}
	defer zr.Close()

	for _, f := range zr.File {
		if strings.EqualFold(filepath.Base(f.Name), "SKILL.md") {
			rc, err := f.Open()
			if err != nil {
				return "", fmt.Errorf("invalid skill archive")
			}
			data, err := io.ReadAll(io.LimitReader(rc, 2*1024*1024))
			rc.Close()
			if err != nil {
				return "", fmt.Errorf("invalid skill archive")
			}
			meta, _, err := skills.ParseSkillMarkdown(string(data))
			if err != nil {
				return "", fmt.Errorf("invalid SKILL.md metadata")
			}
			if strings.TrimSpace(meta.Name) != "" {
				return strings.TrimSpace(meta.Name), nil
			}
		}
	}

	base := strings.TrimSuffix(filepath.Base(archivePath), filepath.Ext(archivePath))
	base = strings.TrimSpace(base)
	if base == "" {
		return "", fmt.Errorf("invalid skill archive")
	}
	return base, nil
}

func safeExtractSkillArchive(archivePath, targetDir string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("invalid skill archive")
	}
	defer zr.Close()

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}

	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return err
	}

	for _, f := range zr.File {
		cleanName := filepath.Clean(f.Name)
		if cleanName == "." || strings.HasPrefix(cleanName, "..") {
			return fmt.Errorf("invalid archive path")
		}
		dstPath := filepath.Join(targetDir, cleanName)
		absDst, err := filepath.Abs(dstPath)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(absDst, absTarget) {
			return fmt.Errorf("invalid archive path")
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(absDst, 0o755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(absDst), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(absDst)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, io.LimitReader(rc, 20*1024*1024)); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		out.Close()
		rc.Close()
	}

	return nil
}

func (h *SkillsHandler) extensionsPath() string {
	path := config.DefaultExtensionsConfigPath
	if h.cfg != nil && strings.TrimSpace(h.cfg.ExtensionsRef.ConfigPath) != "" {
		path = strings.TrimSpace(h.cfg.ExtensionsRef.ConfigPath)
	}
	return path
}

func (h *SkillsHandler) loadExtensions() (*config.ExtensionsConfig, error) {
	path := h.extensionsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &config.ExtensionsConfig{MCPServers: map[string]config.MCPServerConfig{}, Skills: map[string]config.SkillStateConfig{}}, nil
		}
		return nil, err
	}
	var ext config.ExtensionsConfig
	if err := json.Unmarshal(data, &ext); err != nil {
		return nil, err
	}
	if ext.MCPServers == nil {
		ext.MCPServers = map[string]config.MCPServerConfig{}
	}
	if ext.Skills == nil {
		ext.Skills = map[string]config.SkillStateConfig{}
	}
	return &ext, nil
}

func (h *SkillsHandler) saveExtensions(ext *config.ExtensionsConfig) error {
	path := h.extensionsPath()
	data, err := json.MarshalIndent(ext, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

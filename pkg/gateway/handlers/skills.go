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
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"

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
		extCfg, err := h.loadExtensions()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if extCfg.Skills == nil {
			extCfg.Skills = map[string]config.SkillStateConfig{}
		}
		extCfg.Skills[name] = config.SkillStateConfig{Enabled: *req.Enabled}
		if err := h.saveExtensions(extCfg); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if h.cfg != nil {
			h.cfg.Extensions.Skills = extCfg.Skills
		}
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

	meta, err := readSkillMetadataFromArchive(archivePath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	skillName := meta.Name

	extCfg, err := h.loadExtensions()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
	if !isPathWithinBase(absCandidate, absBase) {
		return "", fmt.Errorf("invalid path")
	}
	return absCandidate, nil
}

func isPathWithinBase(candidate, base string) bool {
	rel, err := filepath.Rel(base, candidate)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

const maxSkillArchiveTotalBytes int64 = 512 * 1024 * 1024

var allowedSkillFrontmatterKeys = map[string]struct{}{
	"name":          {},
	"description":   {},
	"license":       {},
	"allowed-tools": {},
	"metadata":      {},
	"compatibility": {},
	"version":       {},
	"author":        {},
}

var skillNamePattern = regexp.MustCompile(`^[a-z0-9-]+$`)

func readSkillMetadataFromArchive(archivePath string) (skills.SkillMetadata, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return skills.SkillMetadata{}, fmt.Errorf("invalid skill archive")
	}
	defer zr.Close()

	hasVisibleEntry := false
	for _, f := range zr.File {
		cleanName := filepath.Clean(f.Name)
		if cleanName == "." || strings.HasPrefix(cleanName, "..") {
			return skills.SkillMetadata{}, fmt.Errorf("invalid archive path")
		}
		if !strings.HasPrefix(cleanName, "__MACOSX") && !strings.HasPrefix(filepath.Base(cleanName), ".") {
			hasVisibleEntry = true
		}
		if strings.EqualFold(filepath.Base(cleanName), "SKILL.md") {
			rc, err := f.Open()
			if err != nil {
				return skills.SkillMetadata{}, fmt.Errorf("invalid skill archive")
			}
			data, err := io.ReadAll(io.LimitReader(rc, 2*1024*1024))
			rc.Close()
			if err != nil {
				return skills.SkillMetadata{}, fmt.Errorf("invalid skill archive")
			}
			return parseAndValidateSkillMarkdown(string(data))
		}
	}

	if !hasVisibleEntry {
		return skills.SkillMetadata{}, fmt.Errorf("skill archive is empty")
	}
	return skills.SkillMetadata{}, fmt.Errorf("SKILL.md not found")
}

func parseAndValidateSkillMarkdown(content string) (skills.SkillMetadata, error) {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "---\n") {
		return skills.SkillMetadata{}, fmt.Errorf("no YAML frontmatter found")
	}

	rest := strings.TrimPrefix(trimmed, "---\n")
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return skills.SkillMetadata{}, fmt.Errorf("invalid frontmatter format")
	}
	fmText := rest[:idx]

	var fm map[string]any
	if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
		return skills.SkillMetadata{}, fmt.Errorf("invalid YAML in frontmatter")
	}
	if fm == nil {
		return skills.SkillMetadata{}, fmt.Errorf("frontmatter must be a YAML dictionary")
	}

	for k := range fm {
		if _, ok := allowedSkillFrontmatterKeys[k]; !ok {
			return skills.SkillMetadata{}, fmt.Errorf("unexpected key in SKILL.md frontmatter: %s", k)
		}
	}

	meta, _, err := skills.ParseSkillMarkdown(trimmed)
	if err != nil {
		return skills.SkillMetadata{}, fmt.Errorf("invalid SKILL.md metadata")
	}
	meta.Name = strings.TrimSpace(meta.Name)
	meta.Description = strings.TrimSpace(meta.Description)

	if meta.Name == "" {
		return skills.SkillMetadata{}, fmt.Errorf("missing 'name' in frontmatter")
	}
	if meta.Description == "" {
		return skills.SkillMetadata{}, fmt.Errorf("missing 'description' in frontmatter")
	}
	if err := validateSkillName(meta.Name); err != nil {
		return skills.SkillMetadata{}, err
	}
	if err := validateSkillDescription(meta.Description); err != nil {
		return skills.SkillMetadata{}, err
	}
	return meta, nil
}

func validateSkillName(name string) error {
	if len(name) > 64 {
		return fmt.Errorf("name is too long (%d characters). maximum is 64 characters", len(name))
	}
	if !skillNamePattern.MatchString(name) {
		return fmt.Errorf("name '%s' should be hyphen-case", name)
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") || strings.Contains(name, "--") {
		return fmt.Errorf("name '%s' cannot start/end with hyphen or contain consecutive hyphens", name)
	}
	return nil
}

func validateSkillDescription(description string) error {
	if strings.Contains(description, "<") || strings.Contains(description, ">") {
		return fmt.Errorf("description cannot contain angle brackets")
	}
	if len(description) > 1024 {
		return fmt.Errorf("description is too long (%d characters). maximum is 1024 characters", len(description))
	}
	return nil
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

	var totalExpected int64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		totalExpected += int64(f.UncompressedSize64)
		if totalExpected > maxSkillArchiveTotalBytes {
			return fmt.Errorf("skill archive is too large or appears highly compressed")
		}
	}

	var totalWritten int64
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
		if !isPathWithinBase(absDst, absTarget) {
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

		buf := make([]byte, 64*1024)
		for {
			n, readErr := rc.Read(buf)
			if n > 0 {
				totalWritten += int64(n)
				if totalWritten > maxSkillArchiveTotalBytes {
					out.Close()
					rc.Close()
					return fmt.Errorf("skill archive is too large or appears highly compressed")
				}
				if _, wErr := out.Write(buf[:n]); wErr != nil {
					out.Close()
					rc.Close()
					return wErr
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				out.Close()
				rc.Close()
				return readErr
			}
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

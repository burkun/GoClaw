package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SkillLoadLevel indicates how much of a skill has been loaded.
type SkillLoadLevel int

const (
	// LoadLevelMetadata indicates only frontmatter metadata has been loaded.
	LoadLevelMetadata SkillLoadLevel = iota
	// LoadLevelBody indicates the SKILL.md body has been loaded.
	LoadLevelBody
	// LoadLevelFull indicates all resources have been loaded.
	LoadLevelFull
)

// ParseSkillMarkdown parses SKILL.md content and returns frontmatter metadata.
// Frontmatter format:
// ---
// name: xxx
// description: xxx
// version: x.y.z
// allowed-tools: [a, b]
// ---
func ParseSkillMarkdown(content string) (SkillMetadata, string, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return SkillMetadata{}, "", nil
	}

	if !strings.HasPrefix(trimmed, "---\n") {
		return SkillMetadata{}, content, nil
	}

	rest := strings.TrimPrefix(trimmed, "---\n")
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return SkillMetadata{}, "", fmt.Errorf("skills parser: invalid frontmatter, missing closing ---")
	}

	fm := rest[:idx]
	body := strings.TrimLeft(strings.TrimPrefix(rest[idx:], "\n---"), "\n")

	var meta SkillMetadata
	if err := yaml.Unmarshal([]byte(fm), &meta); err != nil {
		return SkillMetadata{}, "", fmt.Errorf("skills parser: parse frontmatter failed: %w", err)
	}
	meta.Name = strings.TrimSpace(meta.Name)
	meta.Description = strings.TrimSpace(meta.Description)
	meta.Version = strings.TrimSpace(meta.Version)
	meta.License = strings.TrimSpace(meta.License)
	for i := range meta.AllowedTools {
		meta.AllowedTools[i] = strings.TrimSpace(meta.AllowedTools[i])
	}

	return meta, body, nil
}

// LoadBody loads the SKILL.md body for a skill that was loaded with only metadata.
// This implements the second layer of progressive loading.
func (s *Skill) LoadBody() (string, error) {
	if s.FilePath == "" {
		return "", fmt.Errorf("skill %s: no file path set", s.Metadata.Name)
	}

	data, err := os.ReadFile(s.FilePath)
	if err != nil {
		return "", fmt.Errorf("skill %s: read body failed: %w", s.Metadata.Name, err)
	}

	_, body, err := ParseSkillMarkdown(string(data))
	if err != nil {
		return "", fmt.Errorf("skill %s: parse body failed: %w", s.Metadata.Name, err)
	}

	return body, nil
}

// LoadResource loads an external resource file relative to the skill directory.
// This implements the third layer of progressive loading.
func (s *Skill) LoadResource(relativePath string) ([]byte, error) {
	if s.Dir == "" {
		return nil, fmt.Errorf("skill %s: no directory set", s.Metadata.Name)
	}

	fullPath := filepath.Join(s.Dir, relativePath)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("skill %s: read resource %s failed: %w", s.Metadata.Name, relativePath, err)
	}

	return data, nil
}

// GetContainerPath returns the container-mounted path for a skill resource.
// Used when the skill needs to reference files from within a sandbox container.
func (s *Skill) GetContainerPath(containerSkillsRoot string, relativePath string) string {
	if s.RelativePath == "" {
		return filepath.Join(containerSkillsRoot, s.Metadata.Name, relativePath)
	}
	return filepath.Join(containerSkillsRoot, s.RelativePath, relativePath)
}

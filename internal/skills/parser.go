package skills

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
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
	for i := range meta.AllowedTools {
		meta.AllowedTools[i] = strings.TrimSpace(meta.AllowedTools[i])
	}

	return meta, body, nil
}

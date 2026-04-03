package skills

import "testing"

func TestParseSkillMarkdown_WithFrontmatter(t *testing.T) {
	content := `---
name: test-skill
description: demo skill
version: 1.0.0
allowed-tools:
  - bash
  - read
---
# Skill Body
Hello`

	meta, body, err := ParseSkillMarkdown(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.Name != "test-skill" || meta.Version != "1.0.0" {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
	if len(meta.AllowedTools) != 2 || meta.AllowedTools[0] != "bash" {
		t.Fatalf("unexpected allowed tools: %+v", meta.AllowedTools)
	}
	if body == "" {
		t.Fatalf("expected non-empty body")
	}
}

func TestParseSkillMarkdown_NoFrontmatter(t *testing.T) {
	meta, body, err := ParseSkillMarkdown("# Skill\nNo frontmatter")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.Name != "" {
		t.Fatalf("expected empty metadata, got %+v", meta)
	}
	if body == "" {
		t.Fatalf("expected original body")
	}
}

package skills

import (
	"os"
	"path/filepath"
	"testing"

	"goclaw/internal/config"
)

func TestLoaderLoad_FilterEnabledAndFallbackName(t *testing.T) {
	tmp := t.TempDir()
	pubA := filepath.Join(tmp, "public", "skill-a")
	pubB := filepath.Join(tmp, "public", "skill-b")
	customC := filepath.Join(tmp, "custom", "skill-c")
	for _, p := range []string{pubA, pubB, customC} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
	}

	if err := os.WriteFile(filepath.Join(pubA, "SKILL.md"), []byte(`---
name: alpha
---
body`), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pubB, "SKILL.md"), []byte(`---
name: beta
---
body`), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(customC, "SKILL.md"), []byte(`# no frontmatter`), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	loader := NewLoader()
	skills, err := loader.Load(tmp, config.ExtensionsConfig{Skills: map[string]config.SkillStateConfig{
		"beta": {Enabled: false},
	}})
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}
	if skills[0].Metadata.Name != "alpha" {
		t.Fatalf("unexpected first skill: %+v", skills[0].Metadata)
	}
	if skills[1].Metadata.Name != "skill-c" {
		t.Fatalf("expected fallback name skill-c, got %s", skills[1].Metadata.Name)
	}
}

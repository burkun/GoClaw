package skills

import (
	"context"

	"github.com/bookerbai/goclaw/internal/config"
)

// SkillMetadata is parsed from SKILL.md frontmatter.
// Fields align with DeerFlow's skill metadata structure.
type SkillMetadata struct {
	// Name is the unique identifier for the skill.
	Name string `yaml:"name"`

	// Description is a brief summary of what the skill does.
	Description string `yaml:"description"`

	// Version is the semantic version of the skill.
	Version string `yaml:"version,omitempty"`

	// License is the SPDX license identifier.
	License string `yaml:"license,omitempty"`

	// AllowedTools restricts which tools this skill can use.
	AllowedTools []string `yaml:"allowed-tools,omitempty"`

	// Author is the creator/maintainer of the skill (P2 alignment).
	Author string `yaml:"author,omitempty"`

	// Compatibility specifies runtime requirements (P2 alignment).
	// Example: "goclaw>=1.0.0", "deerflow>=2.0.0"
	Compatibility string `yaml:"compatibility,omitempty"`

	// Metadata holds additional custom metadata fields (P2 alignment).
	// This is a flexible key-value store for skill-specific configuration.
	Metadata map[string]any `yaml:"metadata,omitempty"`

	// Enabled is a runtime state indicating whether the skill is active.
	Enabled bool `yaml:"enabled"`

	// Category is an optional classification for grouping skills.
	Category string `yaml:"category,omitempty"`
}

// Plugin defines the minimal lifecycle hooks for a skill plugin.
type Plugin interface {
	Name() string
	OnLoad(ctx context.Context, cfg *config.AppConfig) error
	OnUnload(ctx context.Context) error
	OnConfigReload(cfg *config.AppConfig) error
}

// Skill is a loaded skill entry.
type Skill struct {
	Metadata     SkillMetadata
	Dir          string
	FilePath     string
	RelativePath string // relative path from skills root directory
	Plugin       Plugin
}

// NoopPlugin is the default plugin implementation used for metadata-only skills.
type NoopPlugin struct {
	name string
}

func NewNoopPlugin(name string) *NoopPlugin {
	return &NoopPlugin{name: name}
}

func (p *NoopPlugin) Name() string { return p.name }

func (p *NoopPlugin) OnLoad(_ context.Context, _ *config.AppConfig) error { return nil }

func (p *NoopPlugin) OnUnload(_ context.Context) error { return nil }

func (p *NoopPlugin) OnConfigReload(_ *config.AppConfig) error { return nil }

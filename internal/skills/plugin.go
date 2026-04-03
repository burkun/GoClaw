package skills

import (
	"context"

	"github.com/bookerbai/goclaw/internal/config"
)

// SkillMetadata is parsed from SKILL.md frontmatter.
type SkillMetadata struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	Version      string   `yaml:"version"`
	AllowedTools []string `yaml:"allowed-tools"`
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
	Metadata SkillMetadata
	Dir      string
	FilePath string
	Plugin   Plugin
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

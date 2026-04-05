package skills

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/bookerbai/goclaw/internal/config"
)

// Registry stores loaded skills and dispatches plugin lifecycle hooks.
type Registry struct {
	mu     sync.RWMutex
	skills map[string]*Skill
}

func NewRegistry() *Registry {
	return &Registry{skills: make(map[string]*Skill)}
}

func (r *Registry) Register(skill *Skill) error {
	if skill == nil {
		return fmt.Errorf("skills registry: nil skill")
	}
	name := strings.TrimSpace(skill.Metadata.Name)
	if name == "" {
		return fmt.Errorf("skills registry: skill name is required")
	}
	if skill.Plugin == nil {
		skill.Plugin = NewNoopPlugin(name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.skills[name]; exists {
		return fmt.Errorf("skills registry: duplicate skill %q", name)
	}
	r.skills[name] = skill
	return nil
}

func (r *Registry) List() []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.skills))
	for name := range r.skills {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]*Skill, 0, len(names))
	for _, name := range names {
		out = append(out, r.skills[name])
	}
	return out
}

// GetByName returns the skill with the given name, or nil if not found.
func (r *Registry) GetByName(name string) *Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.skills[name]
}

// AllowedToolSet returns the union of non-empty allowed-tools declared by skills.
// If no skill declares allowed-tools, it returns nil (means no restriction).
func (r *Registry) AllowedToolSet() map[string]struct{} {
	allowed := make(map[string]struct{})
	for _, skill := range r.List() {
		for _, tool := range skill.Metadata.AllowedTools {
			name := strings.TrimSpace(tool)
			if name == "" {
				continue
			}
			allowed[name] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return nil
	}
	return allowed
}

func (r *Registry) OnLoad(ctx context.Context, cfg *config.AppConfig) error {
	for _, skill := range r.List() {
		if err := skill.Plugin.OnLoad(ctx, cfg); err != nil {
			return fmt.Errorf("skills registry: on_load %s failed: %w", skill.Metadata.Name, err)
		}
	}
	return nil
}

func (r *Registry) OnUnload(ctx context.Context) error {
	for _, skill := range r.List() {
		if err := skill.Plugin.OnUnload(ctx); err != nil {
			return fmt.Errorf("skills registry: on_unload %s failed: %w", skill.Metadata.Name, err)
		}
	}
	return nil
}

func (r *Registry) OnConfigReload(cfg *config.AppConfig) error {
	for _, skill := range r.List() {
		if err := skill.Plugin.OnConfigReload(cfg); err != nil {
			return fmt.Errorf("skills registry: on_config_reload %s failed: %w", skill.Metadata.Name, err)
		}
	}
	return nil
}

// GetSkillsPromptSection returns a formatted string for system prompt injection.
// It includes the name and description of all enabled skills.
// If availableSkills is not nil, only skills in the set are included.
// This mirrors DeerFlow's get_skills_prompt_section() functionality.
func (r *Registry) GetSkillsPromptSection(availableSkills map[string]bool) string {
	skills := r.List()
	if len(skills) == 0 {
		return ""
	}

	// Filter by availableSkills if specified
	if availableSkills != nil {
		filtered := make([]*Skill, 0, len(skills))
		for _, skill := range skills {
			if availableSkills[skill.Metadata.Name] {
				filtered = append(filtered, skill)
			}
		}
		skills = filtered
	}

	if len(skills) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Available Skills\n\n")
	sb.WriteString("You have access to the following skills. Each skill provides specialized capabilities:\n\n")

	for _, skill := range skills {
		sb.WriteString(fmt.Sprintf("- **%s**", skill.Metadata.Name))
		if skill.Metadata.Description != "" {
			sb.WriteString(": ")
			sb.WriteString(skill.Metadata.Description)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\nWhen a user request matches a skill's purpose, you should leverage that skill's capabilities.\n")
	return sb.String()
}

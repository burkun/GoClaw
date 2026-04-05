package builtins

import (
	"github.com/bookerbai/goclaw/internal/config"
)

// BuiltinSubagents is the registry of built-in subagent configurations.
var BuiltinSubagents = map[string]config.SubagentTypeConfig{
	"general-purpose": GeneralPurposeConfig,
	"bash":            BashAgentConfig,
}

// Get returns a builtin subagent config by name, or false if not found.
func Get(name string) (config.SubagentTypeConfig, bool) {
	cfg, ok := BuiltinSubagents[name]
	return cfg, ok
}

// List returns all builtin subagent configurations.
func List() []config.SubagentTypeConfig {
	out := make([]config.SubagentTypeConfig, 0, len(BuiltinSubagents))
	for _, cfg := range BuiltinSubagents {
		out = append(out, cfg)
	}
	return out
}

// Names returns all builtin subagent names.
func Names() []string {
	out := make([]string, 0, len(BuiltinSubagents))
	for name := range BuiltinSubagents {
		out = append(out, name)
	}
	return out
}

// GetEffectiveConfig returns the effective config for a subagent type,
// merging builtin defaults with config.yaml overrides.
func GetEffectiveConfig(name string, appCfg *config.AppConfig) config.SubagentTypeConfig {
	// Start with builtin default (or empty if not builtin)
	result, isBuiltin := BuiltinSubagents[name]
	if !isBuiltin {
		result = config.SubagentTypeConfig{Enabled: true}
	}

	// Apply config.yaml overrides
	if appCfg != nil && appCfg.Subagents.Types != nil {
		if override, ok := appCfg.Subagents.Types[name]; ok {
			// Merge: override takes precedence.
			// 注意：enabled 为 bool 无法区分“未配置”和“显式 false”，因此采用启发式：
			// - enabled=true 总是覆盖为 true
			// - enabled=false 且没有其它覆盖字段时，视为显式禁用
			if override.Enabled {
				result.Enabled = true
			} else if !hasOtherOverrideFields(override) {
				result.Enabled = false
			}
			if override.Description != "" {
				result.Description = override.Description
			}
			if override.Model != "" {
				result.Model = override.Model
			}
			if override.TimeoutSecs > 0 {
				result.TimeoutSecs = override.TimeoutSecs
			}
			if override.SystemPrompt != "" {
				result.SystemPrompt = override.SystemPrompt
			}
			if override.MaxTurns > 0 {
				result.MaxTurns = override.MaxTurns
			}
			if len(override.AllowedTools) > 0 {
				result.AllowedTools = override.AllowedTools
			}
			if len(override.DisallowedTools) > 0 {
				result.DisallowedTools = override.DisallowedTools
			}
		}
	}

	return result
}

// GetAvailableNames returns subagent names that should be exposed to the runtime.
// This respects sandbox configuration (e.g., hiding bash agent if host bash is disabled).
func GetAvailableNames(appCfg *config.AppConfig) []string {
	names := Names()

	// Check if host bash is allowed
	if appCfg != nil && !appCfg.Sandbox.AllowHostBash {
		// Filter out bash agent
		filtered := make([]string, 0, len(names))
		for _, name := range names {
			if name != "bash" {
				filtered = append(filtered, name)
			}
		}
		return filtered
	}

	return names
}

func hasOtherOverrideFields(override config.SubagentTypeConfig) bool {
	return override.Description != "" ||
		override.Model != "" ||
		override.TimeoutSecs > 0 ||
		override.SystemPrompt != "" ||
		override.MaxTurns > 0 ||
		len(override.AllowedTools) > 0 ||
		len(override.DisallowedTools) > 0
}

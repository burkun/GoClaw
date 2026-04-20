package builtins

import (
	"sort"
	"testing"

	"goclaw/internal/config"
)

func TestBuiltinSubagents_ContainsExpected(t *testing.T) {
	expected := []string{"general-purpose", "bash"}
	for _, name := range expected {
		if _, ok := BuiltinSubagents[name]; !ok {
			t.Errorf("expected builtin subagent %q", name)
		}
	}
}

func TestGet_Found(t *testing.T) {
	cfg, ok := Get("general-purpose")
	if !ok {
		t.Fatal("expected to find general-purpose subagent")
	}
	if !cfg.Enabled {
		t.Error("expected general-purpose to be enabled")
	}
	if cfg.Description == "" {
		t.Error("expected non-empty description")
	}
	if cfg.SystemPrompt == "" {
		t.Error("expected non-empty system prompt")
	}
}

func TestGet_NotFound(t *testing.T) {
	cfg, ok := Get("nonexistent")
	if ok {
		t.Error("expected not to find nonexistent subagent")
	}
	if cfg.Enabled {
		t.Error("expected disabled for nonexistent subagent")
	}
}

func TestList(t *testing.T) {
	list := List()
	if len(list) < 2 {
		t.Errorf("expected at least 2 builtin subagents, got %d", len(list))
	}

	// Check that all are enabled
	for _, cfg := range list {
		if !cfg.Enabled {
			t.Error("expected all builtin subagents to be enabled")
		}
	}
}

func TestNames(t *testing.T) {
	names := Names()
	if len(names) < 2 {
		t.Errorf("expected at least 2 names, got %d", len(names))
	}

	// Sort for deterministic comparison
	sort.Strings(names)

	found := make(map[string]bool)
	for _, name := range names {
		found[name] = true
	}

	if !found["general-purpose"] {
		t.Error("expected general-purpose in names")
	}
	if !found["bash"] {
		t.Error("expected bash in names")
	}
}

func TestGetEffectiveConfig_Builtin(t *testing.T) {
	cfg := GetEffectiveConfig("general-purpose", nil)

	if !cfg.Enabled {
		t.Error("expected general-purpose to be enabled")
	}
	if cfg.Model != "inherit" {
		t.Errorf("expected model=inherit, got %q", cfg.Model)
	}
	if cfg.MaxTurns != 50 {
		t.Errorf("expected MaxTurns=50, got %d", cfg.MaxTurns)
	}
}

func TestGetEffectiveConfig_NonBuiltin(t *testing.T) {
	cfg := GetEffectiveConfig("custom-agent", nil)

	// Non-builtin should have default config
	if !cfg.Enabled {
		t.Error("expected non-builtin to be enabled by default")
	}
}

func TestGetEffectiveConfig_WithOverride(t *testing.T) {
	appCfg := &config.AppConfig{
		Subagents: config.SubagentsConfig{
			Types: map[string]config.SubagentTypeConfig{
				"general-purpose": {
					Enabled:      false,
					MaxTurns:     100,
					SystemPrompt: "custom prompt",
				},
			},
		},
	}

	cfg := GetEffectiveConfig("general-purpose", appCfg)

	// Override should take precedence
	if cfg.MaxTurns != 100 {
		t.Errorf("expected MaxTurns=100, got %d", cfg.MaxTurns)
	}
	if cfg.SystemPrompt != "custom prompt" {
		t.Errorf("expected custom prompt, got %q", cfg.SystemPrompt)
	}
}

func TestGetEffectiveConfig_OverrideEnabledTrue(t *testing.T) {
	appCfg := &config.AppConfig{
		Subagents: config.SubagentsConfig{
			Types: map[string]config.SubagentTypeConfig{
				"general-purpose": {Enabled: true},
			},
		},
	}

	cfg := GetEffectiveConfig("general-purpose", appCfg)
	if !cfg.Enabled {
		t.Error("expected enabled=true with override")
	}
}

func TestGetEffectiveConfig_OverrideEnabledFalse(t *testing.T) {
	appCfg := &config.AppConfig{
		Subagents: config.SubagentsConfig{
			Types: map[string]config.SubagentTypeConfig{
				"general-purpose": {Enabled: false},
			},
		},
	}

	cfg := GetEffectiveConfig("general-purpose", appCfg)
	if cfg.Enabled {
		t.Error("expected enabled=false with override false")
	}
}

func TestGetEffectiveConfig_OverrideEnabledFalseWithOtherFields(t *testing.T) {
	appCfg := &config.AppConfig{
		Subagents: config.SubagentsConfig{
			Types: map[string]config.SubagentTypeConfig{
				"general-purpose": {
					Enabled:  false,
					MaxTurns: 10, // other field present
				},
			},
		},
	}

	cfg := GetEffectiveConfig("general-purpose", appCfg)
	// When other fields are present, enabled=false should be treated as intentional
	// but due to the logic, enabled=true from builtin takes precedence
	if !cfg.Enabled {
		t.Error("expected enabled=true (builtin default takes precedence when other fields present)")
	}
	if cfg.MaxTurns != 10 {
		t.Errorf("expected MaxTurns=10, got %d", cfg.MaxTurns)
	}
}

func TestGetAvailableNames_AllAllowed(t *testing.T) {
	appCfg := &config.AppConfig{
		Sandbox: config.SandboxConfig{
			AllowHostBash: true,
		},
	}

	names := GetAvailableNames(appCfg)

	// Should include bash since AllowHostBash=true
	found := make(map[string]bool)
	for _, name := range names {
		found[name] = true
	}

	if !found["bash"] {
		t.Error("expected bash to be available when AllowHostBash=true")
	}
}

func TestGetAvailableNames_BashFiltered(t *testing.T) {
	appCfg := &config.AppConfig{
		Sandbox: config.SandboxConfig{
			AllowHostBash: false,
		},
	}

	names := GetAvailableNames(appCfg)

	// Should NOT include bash since AllowHostBash=false
	for _, name := range names {
		if name == "bash" {
			t.Error("expected bash to be filtered out when AllowHostBash=false")
		}
	}
}

func TestGetAvailableNames_NilConfig(t *testing.T) {
	names := GetAvailableNames(nil)

	// Should return all names when config is nil
	if len(names) < 2 {
		t.Errorf("expected at least 2 names, got %d", len(names))
	}
}

func TestGeneralPurposeConfig(t *testing.T) {
	cfg := GeneralPurposeConfig

	if !cfg.Enabled {
		t.Error("expected Enabled=true")
	}
	if cfg.Model != "inherit" {
		t.Errorf("expected Model=inherit, got %q", cfg.Model)
	}
	if cfg.MaxTurns != 50 {
		t.Errorf("expected MaxTurns=50, got %d", cfg.MaxTurns)
	}
	if len(cfg.AllowedTools) != 0 {
		t.Errorf("expected no allowed tools restriction, got %v", cfg.AllowedTools)
	}
	if len(cfg.DisallowedTools) == 0 {
		t.Error("expected some disallowed tools")
	}
}

func TestBashAgentConfig(t *testing.T) {
	cfg := BashAgentConfig

	if !cfg.Enabled {
		t.Error("expected Enabled=true")
	}
	if cfg.Model != "inherit" {
		t.Errorf("expected Model=inherit, got %q", cfg.Model)
	}
	if cfg.MaxTurns != 30 {
		t.Errorf("expected MaxTurns=30, got %d", cfg.MaxTurns)
	}
	if len(cfg.AllowedTools) == 0 {
		t.Error("expected some allowed tools")
	}
	if len(cfg.DisallowedTools) == 0 {
		t.Error("expected some disallowed tools")
	}
}

func TestHasOtherOverrideFields(t *testing.T) {
	tests := []struct {
		name     string
		cfg      config.SubagentTypeConfig
		hasOther bool
	}{
		{"empty", config.SubagentTypeConfig{}, false},
		{"only enabled", config.SubagentTypeConfig{Enabled: true}, false},
		{"with description", config.SubagentTypeConfig{Description: "test"}, true},
		{"with model", config.SubagentTypeConfig{Model: "gpt-4"}, true},
		{"with timeout", config.SubagentTypeConfig{TimeoutSecs: 60}, true},
		{"with system prompt", config.SubagentTypeConfig{SystemPrompt: "test"}, true},
		{"with max turns", config.SubagentTypeConfig{MaxTurns: 10}, true},
		{"with allowed tools", config.SubagentTypeConfig{AllowedTools: []string{"bash"}}, true},
		{"with disallowed tools", config.SubagentTypeConfig{DisallowedTools: []string{"task"}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasOtherOverrideFields(tt.cfg)
			if result != tt.hasOther {
				t.Errorf("hasOtherOverrideFields(%v) = %v, want %v", tt.cfg, result, tt.hasOther)
			}
		})
	}
}

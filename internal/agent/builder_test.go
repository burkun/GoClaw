package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	lctool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"goclaw/internal/agentconfig"
	"goclaw/internal/config"
	skillsruntime "goclaw/internal/skills"
)

// --- Mock Types ---

// mockTool implements the tool.BaseTool interface for testing
type mockTool struct {
	name        string
	description string
	infoErr     error
}

func (m *mockTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	if m.infoErr != nil {
		return nil, m.infoErr
	}
	return &schema.ToolInfo{
		Name: m.name,
		Desc: m.description,
	}, nil
}

func (m *mockTool) InvokableRun(_ context.Context, _ string, _ ...lctool.Option) (string, error) {
	return "ok", nil
}

// --- Tests for filterToolsByAllowed ---

func TestFilterToolsByAllowed_EmptyAllowed(t *testing.T) {
	tools := []lctool.BaseTool{
		&mockTool{name: "tool1"},
		&mockTool{name: "tool2"},
	}

	filtered, err := filterToolsByAllowed(context.Background(), tools, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(filtered))
	}
}

func TestFilterToolsByAllowed_EmptyAllowedMap(t *testing.T) {
	tools := []lctool.BaseTool{
		&mockTool{name: "tool1"},
		&mockTool{name: "tool2"},
	}

	filtered, err := filterToolsByAllowed(context.Background(), tools, map[string]struct{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(filtered))
	}
}

func TestFilterToolsByAllowed_WithMatches(t *testing.T) {
	tools := []lctool.BaseTool{
		&mockTool{name: "read_file"},
		&mockTool{name: "write_file"},
		&mockTool{name: "bash"},
	}

	allowed := map[string]struct{}{
		"read_file": {},
		"write_file": {},
	}

	filtered, err := filterToolsByAllowed(context.Background(), tools, allowed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(filtered))
	}
}

func TestFilterToolsByAllowed_NoMatches(t *testing.T) {
	tools := []lctool.BaseTool{
		&mockTool{name: "read_file"},
	}

	allowed := map[string]struct{}{
		"nonexistent": {},
	}

	_, err := filterToolsByAllowed(context.Background(), tools, allowed)
	if err == nil {
		t.Fatal("expected error when no tools match")
	}
}

func TestFilterToolsByAllowed_ToolInfoError(t *testing.T) {
	tools := []lctool.BaseTool{
		&mockTool{name: "tool1", infoErr: errors.New("info error")},
	}

	allowed := map[string]struct{}{
		"tool1": {},
	}

	_, err := filterToolsByAllowed(context.Background(), tools, allowed)
	if err == nil {
		t.Fatal("expected error when tool.Info fails")
	}
}

// --- Tests for filterSkillsByName ---

func TestFilterSkillsByName(t *testing.T) {
	skills := []*skillsruntime.Skill{
		{
			Metadata: skillsruntime.SkillMetadata{
				Name:         "file-ops",
				AllowedTools: []string{"read_file", "write_file"},
			},
		},
		{
			Metadata: skillsruntime.SkillMetadata{
				Name:         "shell-ops",
				AllowedTools: []string{"bash", "ssh"},
			},
		},
		{
			Metadata: skillsruntime.SkillMetadata{
				Name:         "unused",
				AllowedTools: []string{"unused_tool"},
			},
		},
	}

	skillNames := []string{"file-ops"}
	allowed := filterSkillsByName(skills, skillNames)

	if len(allowed) != 2 {
		t.Fatalf("expected 2 allowed tools, got %d", len(allowed))
	}
	if _, ok := allowed["read_file"]; !ok {
		t.Error("expected read_file to be allowed")
	}
	if _, ok := allowed["write_file"]; !ok {
		t.Error("expected write_file to be allowed")
	}
}

func TestFilterSkillsByName_CaseInsensitive(t *testing.T) {
	skills := []*skillsruntime.Skill{
		{
			Metadata: skillsruntime.SkillMetadata{
				Name:         "File-Ops",
				AllowedTools: []string{"read_file"},
			},
		},
	}

	skillNames := []string{"file-ops"}
	allowed := filterSkillsByName(skills, skillNames)

	if len(allowed) != 1 {
		t.Fatalf("expected 1 allowed tool, got %d", len(allowed))
	}
}

func TestFilterSkillsByName_NoMatch(t *testing.T) {
	skills := []*skillsruntime.Skill{
		{
			Metadata: skillsruntime.SkillMetadata{
				Name:         "file-ops",
				AllowedTools: []string{"read_file"},
			},
		},
	}

	skillNames := []string{"nonexistent"}
	allowed := filterSkillsByName(skills, skillNames)

	if len(allowed) != 0 {
		t.Fatalf("expected 0 allowed tools, got %d", len(allowed))
	}
}

func TestFilterSkillsByName_EmptyInput(t *testing.T) {
	skills := []*skillsruntime.Skill{
		{
			Metadata: skillsruntime.SkillMetadata{
				Name:         "file-ops",
				AllowedTools: []string{"read_file"},
			},
		},
	}

	allowed := filterSkillsByName(skills, nil)
	if len(allowed) != 0 {
		t.Fatalf("expected 0 allowed tools for nil input, got %d", len(allowed))
	}

	allowed = filterSkillsByName(skills, []string{})
	if len(allowed) != 0 {
		t.Fatalf("expected 0 allowed tools for empty input, got %d", len(allowed))
	}
}

// --- Tests for filterToolsByToolGroups ---

func TestFilterToolsByToolGroups(t *testing.T) {
	appCfg := &config.AppConfig{
		ToolGroups: []config.ToolGroupConfig{
			{Name: "core"},
			{Name: "advanced"},
		},
		Tools: []config.ToolConfig{
			{Name: "read_file", Group: "core"},
			{Name: "write_file", Group: "core"},
			{Name: "bash", Group: "advanced"},
			{Name: "ssh", Group: ""},
		},
	}

	tools := []lctool.BaseTool{
		&mockTool{name: "read_file"},
		&mockTool{name: "write_file"},
		&mockTool{name: "bash"},
		&mockTool{name: "ssh"},
	}

	filtered, err := filterToolsByToolGroups(context.Background(), tools, []string{"core"}, appCfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(filtered))
	}
}

func TestFilterToolsByToolGroups_EmptyGroups(t *testing.T) {
	tools := []lctool.BaseTool{
		&mockTool{name: "tool1"},
	}

	filtered, err := filterToolsByToolGroups(context.Background(), tools, nil, &config.AppConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(filtered))
	}
}

func TestFilterToolsByToolGroups_InvalidGroup(t *testing.T) {
	appCfg := &config.AppConfig{
		ToolGroups: []config.ToolGroupConfig{{Name: "core"}},
		Tools:      []config.ToolConfig{{Name: "tool1", Group: "core"}},
	}

	tools := []lctool.BaseTool{
		&mockTool{name: "tool1"},
	}

	filtered, err := filterToolsByToolGroups(context.Background(), tools, []string{"nonexistent"}, appCfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(filtered) != 0 {
		t.Fatalf("expected 0 tools for nonexistent group, got %d", len(filtered))
	}
}

// --- Tests for buildSystemPrompt ---

func TestBuildSystemPrompt_BaseOnly(t *testing.T) {
	prompt := buildSystemPrompt("", nil, nil)
	if prompt != "You are GoClaw lead agent." {
		t.Fatalf("unexpected base prompt: %s", prompt)
	}
}

func TestBuildSystemPrompt_WithSoul(t *testing.T) {
	soul := "I am a helpful assistant."
	prompt := buildSystemPrompt(soul, nil, nil)

	if !contains(t, prompt, "You are GoClaw lead agent.") {
		t.Error("expected base prompt in output")
	}
	if !contains(t, prompt, "<agent_soul>") {
		t.Error("expected soul tag in output")
	}
	if !contains(t, prompt, soul) {
		t.Error("expected soul content in output")
	}
}

func TestBuildSystemPrompt_WithSkills(t *testing.T) {
	reg := skillsruntime.NewRegistry()
	reg.Register(&skillsruntime.Skill{
		Metadata: skillsruntime.SkillMetadata{
			Name:        "test-skill",
			Description: "A test skill",
			Enabled:     true,
		},
	})

	prompt := buildSystemPrompt("", reg, nil)
	if !contains(t, prompt, "Available Skills") {
		t.Error("expected skills section in prompt")
	}
	if !contains(t, prompt, "test-skill") {
		t.Error("expected skill name in prompt")
	}
}

func TestBuildSystemPrompt_WithFilteredSkills(t *testing.T) {
	reg := skillsruntime.NewRegistry()
	reg.Register(&skillsruntime.Skill{
		Metadata: skillsruntime.SkillMetadata{
			Name:        "skill-a",
			Description: "Skill A",
		},
	})
	reg.Register(&skillsruntime.Skill{
		Metadata: skillsruntime.SkillMetadata{
			Name:        "skill-b",
			Description: "Skill B",
		},
	})

	// Only include skill-a
	availableSkills := map[string]bool{"skill-a": true}
	prompt := buildSystemPrompt("", reg, availableSkills)

	if !contains(t, prompt, "skill-a") {
		t.Error("expected skill-a in prompt")
	}
	if contains(t, prompt, "skill-b") {
		t.Error("skill-b should not be in filtered prompt")
	}
}

func TestBuildSystemPrompt_NoMatchingSkills(t *testing.T) {
	reg := skillsruntime.NewRegistry()
	reg.Register(&skillsruntime.Skill{
		Metadata: skillsruntime.SkillMetadata{
			Name:        "skill-a",
			Description: "Skill A",
		},
	})

	// Filter with non-matching skill
	availableSkills := map[string]bool{"nonexistent": true}
	prompt := buildSystemPrompt("", reg, availableSkills)

	// Should just return base prompt without skills section
	if prompt != "You are GoClaw lead agent." {
		t.Errorf("expected base prompt only, got: %s", prompt)
	}
}

func TestBuildSystemPrompt_Complete(t *testing.T) {
	reg := skillsruntime.NewRegistry()
	reg.Register(&skillsruntime.Skill{
		Metadata: skillsruntime.SkillMetadata{
			Name:        "test-skill",
			Description: "A test skill",
		},
	})

	soul := "Custom agent behavior."
	prompt := buildSystemPrompt(soul, reg, nil)

	if !contains(t, prompt, "You are GoClaw lead agent.") {
		t.Error("expected base prompt")
	}
	if !contains(t, prompt, "Available Skills") {
		t.Error("expected skills section")
	}
	if !contains(t, prompt, "<agent_soul>") {
		t.Error("expected soul section")
	}
}

// --- Tests for buildSandboxProvider ---

func TestBuildSandboxProvider_LocalDefault(t *testing.T) {
	appCfg := &config.AppConfig{}
	provider := buildSandboxProvider(appCfg)

	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestBuildSandboxProvider_WithLocalConfig(t *testing.T) {
	appCfg := &config.AppConfig{
		Skills: config.SkillsConfig{Path: "/test/skills"},
		Sandbox: config.SandboxConfig{
			Use: "local",
		},
	}
	provider := buildSandboxProvider(appCfg)

	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestBuildSandboxProvider_NilConfig(t *testing.T) {
	provider := buildSandboxProvider(nil)
	if provider == nil {
		t.Fatal("expected non-nil provider even with nil config")
	}
}

func TestBuildSandboxProvider_DockerFallback(t *testing.T) {
	// This test uses strict_docker=false so it falls back to local
	appCfg := &config.AppConfig{
		Sandbox: config.SandboxConfig{
			Use:          "docker",
			StrictDocker: false,
		},
	}

	// Should not panic and should return local provider (docker likely not available in test)
	provider := buildSandboxProvider(appCfg)
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestBuildSandboxProvider_DockerConfig(t *testing.T) {
	appCfg := &config.AppConfig{
		Sandbox: config.SandboxConfig{
			Use:             "docker",
			StrictDocker:    false, // Allow fallback for test
			Image:           "custom-image:latest",
			ContainerPrefix: "test-",
			IdleTimeout:     300,
			Environment:     map[string]string{"KEY": "value"},
			Mounts: []config.VolumeMountConfig{
				{HostPath: "/host", ContainerPath: "/container", ReadOnly: true},
			},
		},
	}

	// Should not panic and should return provider
	provider := buildSandboxProvider(appCfg)
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

// --- Tests for buildMiddlewares ---

func TestBuildMiddlewares_EmptyConfig(t *testing.T) {
	// Save and restore original getAppConfig
	oldGetAppConfig := getAppConfig
	defer func() { getAppConfig = oldGetAppConfig }()

	getAppConfig = func() (*config.AppConfig, error) {
		return &config.AppConfig{}, nil
	}

	mws := buildMiddlewares(RunConfig{})
	// May be nil or empty depending on implementation
	_ = mws
}

func TestBuildMiddlewares_WithConfig(t *testing.T) {
	oldGetAppConfig := getAppConfig
	defer func() { getAppConfig = oldGetAppConfig }()

	getAppConfig = func() (*config.AppConfig, error) {
		return &config.AppConfig{
			Skills: config.SkillsConfig{Path: "/test/skills"},
		}, nil
	}

	cfg := RunConfig{
		AgentName: "test-agent",
	}
	mws := buildMiddlewares(cfg)
	_ = mws
}

func TestBuildMiddlewares_GetConfigError(t *testing.T) {
	oldGetAppConfig := getAppConfig
	defer func() { getAppConfig = oldGetAppConfig }()

	getAppConfig = func() (*config.AppConfig, error) {
		return nil, errors.New("config error")
	}

	// Should handle error gracefully without panic
	mws := buildMiddlewares(RunConfig{})
	_ = mws
}

// --- Helper functions ---

func contains(t *testing.T, s, substr string) bool {
	t.Helper()
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(s[:len(substr)] == substr) ||
		(s[len(s)-len(substr):] == substr) ||
		len(s) > len(substr) && containsInternal(s, substr))
}

func containsInternal(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// --- Integration Tests for Build Functions ---

func TestBuildSystemPrompt_EmptyRegistry(t *testing.T) {
	reg := skillsruntime.NewRegistry()
	prompt := buildSystemPrompt("", reg, nil)

	if prompt != "You are GoClaw lead agent." {
		t.Errorf("expected base prompt only for empty registry, got: %s", prompt)
	}
}

func TestFilterSkillsByName_WhitespaceHandling(t *testing.T) {
	skills := []*skillsruntime.Skill{
		{
			Metadata: skillsruntime.SkillMetadata{
				Name:         "skill",
				AllowedTools: []string{"  tool1  ", "tool2"},
			},
		},
	}

	skillNames := []string{"skill"}
	allowed := filterSkillsByName(skills, skillNames)

	// Whitespace-trimmed tool names
	if len(allowed) != 2 {
		t.Fatalf("expected 2 allowed tools, got %d", len(allowed))
	}
	if _, ok := allowed["tool1"]; !ok {
		t.Error("expected tool1 (whitespace trimmed) to be allowed")
	}
}

func TestFilterToolsByAllowed_Integration(t *testing.T) {
	// Test the complete flow: skills -> allowed tools -> filtered tools
	skills := []*skillsruntime.Skill{
		{
			Metadata: skillsruntime.SkillMetadata{
				Name:         "file-ops",
				AllowedTools: []string{"read_file", "write_file"},
			},
		},
	}

	tools := []lctool.BaseTool{
		&mockTool{name: "read_file"},
		&mockTool{name: "write_file"},
		&mockTool{name: "bash"},
	}

	allowed := filterSkillsByName(skills, []string{"file-ops"})
	filtered, err := filterToolsByAllowed(context.Background(), tools, allowed)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered tools, got %d", len(filtered))
	}
}

func TestCheckpointStorePath(t *testing.T) {
	// Test the helper function indirectly through newCheckPointStore
	cp := &config.CheckpointerConfig{Type: "sqlite", ConnectionString: ""}
	path := checkpointStorePath(cp, "sqlite")
	if path == "" {
		t.Error("expected non-empty path for sqlite with empty connection string")
	}

	// Test with explicit path
	cp2 := &config.CheckpointerConfig{Type: "sqlite", ConnectionString: "/custom/path.db"}
	path2 := checkpointStorePath(cp2, "sqlite")
	if path2 != "/custom/path.db" {
		t.Errorf("expected explicit path, got: %s", path2)
	}

	// Test postgres
	cp3 := &config.CheckpointerConfig{Type: "postgres", ConnectionString: "postgres://user:pass@host/db"}
	path3 := checkpointStorePath(cp3, "postgres")
	if path3 != "postgres://user:pass@host/db" {
		t.Errorf("expected postgres connection string, got: %s", path3)
	}
}

func TestSandboxConfig_Build(t *testing.T) {
	// Test sandbox config building with various options
	appCfg := &config.AppConfig{
		Sandbox: config.SandboxConfig{
			Use:             "local",
			IdleTimeout:     600,
			Image:           "test-image",
			ContainerPrefix: "test-prefix",
			Environment:     map[string]string{"FOO": "bar"},
			Mounts: []config.VolumeMountConfig{
				{HostPath: "/a", ContainerPath: "/b", ReadOnly: true},
				{HostPath: "/c", ContainerPath: "/d", ReadOnly: false},
			},
		},
		Skills: config.SkillsConfig{Path: "/skills"},
	}

	provider := buildSandboxProvider(appCfg)
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}

	// Verify the provider was created - in test environment it will be local provider
	_ = provider
}

// --- Tests for AgentConfig Integration ---

func TestAgentConfig_Loader(t *testing.T) {
	// Test that agentconfig loader can be used
	loader := agentconfig.DefaultLoader
	_ = loader
}

// --- Table-driven tests for complex scenarios ---

type filterToolsTestCase struct {
	name         string
	tools        []string
	allowed      map[string]struct{}
	expectCount  int
	expectError  bool
}

func TestFilterToolsByAllowed_TableDriven(t *testing.T) {
	tests := []filterToolsTestCase{
		{
			name:        "all allowed",
			tools:       []string{"a", "b", "c"},
			allowed:     map[string]struct{}{"a": {}, "b": {}, "c": {}},
			expectCount: 3,
			expectError: false,
		},
		{
			name:        "partial allowed",
			tools:       []string{"a", "b", "c"},
			allowed:     map[string]struct{}{"a": {}, "c": {}},
			expectCount: 2,
			expectError: false,
		},
		{
			name:        "none allowed",
			tools:       []string{"a", "b"},
			allowed:     map[string]struct{}{"x": {}, "y": {}},
			expectCount: 0,
			expectError: true,
		},
		{
			name:        "empty allowed",
			tools:       []string{"a", "b"},
			allowed:     map[string]struct{}{},
			expectCount: 2,
			expectError: false,
		},
		{
			name:        "nil allowed",
			tools:       []string{"a", "b"},
			allowed:     nil,
			expectCount: 2,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := make([]lctool.BaseTool, 0, len(tt.tools))
			for _, name := range tt.tools {
				tools = append(tools, &mockTool{name: name})
			}

			filtered, err := filterToolsByAllowed(context.Background(), tools, tt.allowed)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(filtered) != tt.expectCount {
				t.Errorf("expected %d tools, got %d", tt.expectCount, len(filtered))
			}
		})
	}
}

type skillFilterTestCase struct {
	name           string
	skills         []*skillsruntime.Skill
	skillNames     []string
	expectedTools  int
}

func TestFilterSkillsByName_TableDriven(t *testing.T) {
	tests := []skillFilterTestCase{
		{
			name: "single skill",
			skills: []*skillsruntime.Skill{
				{Metadata: skillsruntime.SkillMetadata{Name: "skill1", AllowedTools: []string{"a", "b"}}},
			},
			skillNames:    []string{"skill1"},
			expectedTools: 2,
		},
		{
			name: "multiple skills",
			skills: []*skillsruntime.Skill{
				{Metadata: skillsruntime.SkillMetadata{Name: "skill1", AllowedTools: []string{"a"}}},
				{Metadata: skillsruntime.SkillMetadata{Name: "skill2", AllowedTools: []string{"b", "c"}}},
			},
			skillNames:    []string{"skill1", "skill2"},
			expectedTools: 3,
		},
		{
			name: "duplicate tools",
			skills: []*skillsruntime.Skill{
				{Metadata: skillsruntime.SkillMetadata{Name: "skill1", AllowedTools: []string{"a", "b"}}},
				{Metadata: skillsruntime.SkillMetadata{Name: "skill2", AllowedTools: []string{"b", "c"}}},
			},
			skillNames:    []string{"skill1", "skill2"},
			expectedTools: 3, // "b" appears in both but is stored once in map
		},
		{
			name: "no matching skills",
			skills: []*skillsruntime.Skill{
				{Metadata: skillsruntime.SkillMetadata{Name: "skill1", AllowedTools: []string{"a"}}},
			},
			skillNames:    []string{"nonexistent"},
			expectedTools: 0,
		},
		{
			name:           "empty skills list",
			skills:         []*skillsruntime.Skill{},
			skillNames:     []string{"skill1"},
			expectedTools:  0,
		},
		{
			name:           "nil skill names",
			skills:         []*skillsruntime.Skill{{Metadata: skillsruntime.SkillMetadata{Name: "skill1", AllowedTools: []string{"a"}}}},
			skillNames:     nil,
			expectedTools:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed := filterSkillsByName(tt.skills, tt.skillNames)
			if len(allowed) != tt.expectedTools {
				t.Errorf("expected %d tools, got %d", tt.expectedTools, len(allowed))
			}
		})
	}
}

// --- Benchmark Tests ---

func BenchmarkFilterToolsByAllowed(b *testing.B) {
	tools := make([]lctool.BaseTool, 100)
	for i := 0; i < 100; i++ {
		tools[i] = &mockTool{name: jsonNumber(i)}
	}

	allowed := map[string]struct{}{}
	for i := 0; i < 50; i++ {
		allowed[jsonNumber(i)] = struct{}{}
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = filterToolsByAllowed(ctx, tools, allowed)
	}
}

func BenchmarkBuildSystemPrompt(b *testing.B) {
	reg := skillsruntime.NewRegistry()
	for i := 0; i < 10; i++ {
		reg.Register(&skillsruntime.Skill{
			Metadata: skillsruntime.SkillMetadata{
				Name:        "skill-" + jsonNumber(i),
				Description: "Description for skill " + jsonNumber(i),
			},
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buildSystemPrompt("", reg, nil)
	}
}

func jsonNumber(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// --- Concurrency Tests ---

func TestFilterToolsByAllowed_Concurrent(t *testing.T) {
	tools := []lctool.BaseTool{
		&mockTool{name: "tool1"},
		&mockTool{name: "tool2"},
	}
	allowed := map[string]struct{}{"tool1": {}}

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- true }()
			filtered, err := filterToolsByAllowed(context.Background(), tools, allowed)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if len(filtered) != 1 {
				t.Errorf("expected 1 tool, got %d", len(filtered))
			}
		}()
	}

	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for concurrent operations")
		}
	}
}

// --- Tests for New/NewWithName ---
// Note: These tests focus on error handling paths since full integration tests
// require mocking many external dependencies.

func TestNew_ConfigError(t *testing.T) {
	// Save and restore original
	oldGetAppConfig := getAppConfig
	defer func() { getAppConfig = oldGetAppConfig }()

	getAppConfig = func() (*config.AppConfig, error) {
		return nil, errors.New("config load failed")
	}

	_, err := New(context.Background())
	if err == nil {
		t.Error("expected error when config fails to load")
	}
	if !strings.Contains(err.Error(), "load config failed") {
		t.Errorf("expected 'load config failed' error, got: %v", err)
	}
}

func TestNewWithName_ConfigError(t *testing.T) {
	// Save and restore original
	oldGetAppConfig := getAppConfig
	defer func() { getAppConfig = oldGetAppConfig }()

	getAppConfig = func() (*config.AppConfig, error) {
		return nil, errors.New("config load failed")
	}

	_, err := NewWithName(context.Background(), "test-agent")
	if err == nil {
		t.Error("expected error when config fails to load")
	}
	if !strings.Contains(err.Error(), "load config failed") {
		t.Errorf("expected 'load config failed' error, got: %v", err)
	}
}

func TestNewWithName_EmptyAgentName(t *testing.T) {
	// Save and restore original
	oldGetAppConfig := getAppConfig
	defer func() { getAppConfig = oldGetAppConfig }()

	// Mock config with empty default model
	getAppConfig = func() (*config.AppConfig, error) {
		return &config.AppConfig{
			Skills: config.SkillsConfig{Path: ""},
		}, nil
	}

	// When agent name is empty, it should behave like New()
	// but may fail on skill/model loading. We're testing the empty name path.
	_, err := NewWithName(context.Background(), "")
	// Error is acceptable since we don't have full mock setup
	// We're just testing that empty name doesn't panic
	_ = err
}

func TestNewWithContextCancellation(t *testing.T) {
	// Save and restore original
	oldGetAppConfig := getAppConfig
	defer func() { getAppConfig = oldGetAppConfig }()

	getAppConfig = func() (*config.AppConfig, error) {
		return &config.AppConfig{}, nil
	}

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// New should handle cancelled context gracefully
	// (may fail on dependencies but shouldn't panic)
	_, _ = New(ctx)
}

func TestNewWithNameWithContextCancellation(t *testing.T) {
	// Save and restore original
	oldGetAppConfig := getAppConfig
	defer func() { getAppConfig = oldGetAppConfig }()

	getAppConfig = func() (*config.AppConfig, error) {
		return &config.AppConfig{}, nil
	}

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// NewWithName should handle cancelled context gracefully
	_, _ = NewWithName(ctx, "test-agent")
}

// Note: Full integration tests for New/NewWithName would require mocking:
// - skillsruntime.NewLoader and Loader.Load
// - skillsruntime.NewRegistry and Registry.Register/OnLoad
// - models.CreateChatModel
// - toolbootstrap.RegisterDefaultToolsWithModel
// - toolruntime functions
// - subagents.NewExecutor/NewTaskTool
// - adk.NewChatModelAgent
// - einoruntime.NewRunner
// Such comprehensive mocking is impractical for unit tests.
// Integration tests should cover the full flow.

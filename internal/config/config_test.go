package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// writeTemp writes content to a temporary YAML file and returns its path.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// ---------------------------------------------------------------------------
// TestLoad_basic
// ---------------------------------------------------------------------------

// TestLoad_basic verifies that a minimal config.yaml is parsed correctly
// into an AppConfig without environment variable substitution.
func TestLoad_basic(t *testing.T) {
	yaml := `
config_version: 1
log_level: debug

server:
  address: :8001
  cors_origins:
    - http://localhost:3000

models:
  - name: gpt-4o
    display_name: GPT-4o
    use: openai
    model: gpt-4o
    api_key: sk-test
    max_tokens: 4096
    supports_vision: true

tool_groups:
  - name: web
  - name: file:read

tools:
  - name: web_search
    group: web
    use: goclaw/internal/tools/websearch:WebSearchTool
    max_results: 5

sandbox:
  use: local
  allow_host_bash: false
  bash_output_max_chars: 20000
  read_file_output_max_chars: 50000

memory:
  enabled: true
  storage_path: memory.json
  debounce_seconds: 30
  max_facts: 100
  fact_confidence_threshold: 0.7
  injection_enabled: true
  max_injection_tokens: 2000

skills:
  container_path: /mnt/skills

checkpointer:
  type: sqlite
  connection_string: checkpoints.db
`

	path := writeTemp(t, yaml)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Top-level fields
	assert.Equal(t, 1, cfg.ConfigVersion)
	assert.Equal(t, "debug", cfg.LogLevel)

	// Server
	assert.Equal(t, ":8001", cfg.Server.Address)
	require.Len(t, cfg.Server.CORSOrigins, 1)
	assert.Equal(t, "http://localhost:3000", cfg.Server.CORSOrigins[0])

	// Models
	require.Len(t, cfg.Models, 1)
	m := cfg.Models[0]
	assert.Equal(t, "gpt-4o", m.Name)
	assert.Equal(t, "GPT-4o", m.DisplayName)
	assert.Equal(t, "openai", m.Use)
	assert.Equal(t, "gpt-4o", m.Model)
	assert.Equal(t, "sk-test", m.APIKey)
	assert.Equal(t, 4096, m.MaxTokens)
	assert.True(t, m.SupportsVision)

	// Tool groups
	require.Len(t, cfg.ToolGroups, 2)
	assert.Equal(t, "web", cfg.ToolGroups[0].Name)

	// Tools
	require.Len(t, cfg.Tools, 1)
	tool := cfg.Tools[0]
	assert.Equal(t, "web_search", tool.Name)
	assert.Equal(t, "web", tool.Group)

	// Sandbox
	assert.Equal(t, "local", cfg.Sandbox.Use)
	assert.False(t, cfg.Sandbox.AllowHostBash)
	assert.Equal(t, 20000, cfg.Sandbox.BashOutputMaxChars)

	// Memory
	assert.True(t, cfg.Memory.Enabled)
	assert.Equal(t, "memory.json", cfg.Memory.StoragePath)
	assert.Equal(t, 30, cfg.Memory.DebounceSeconds)
	assert.InDelta(t, 0.7, cfg.Memory.FactConfidenceThreshold, 1e-9)

	// Skills
	assert.Equal(t, "/mnt/skills", cfg.Skills.ContainerPath)

	// Checkpointer
	require.NotNil(t, cfg.Checkpointer)
	assert.Equal(t, "sqlite", cfg.Checkpointer.Type)
	assert.Equal(t, "checkpoints.db", cfg.Checkpointer.ConnectionString)

	// Helpers
	assert.Equal(t, &cfg.Models[0], cfg.GetModelConfig("gpt-4o"))
	assert.Nil(t, cfg.GetModelConfig("nonexistent"))
	assert.Equal(t, &cfg.Models[0], cfg.DefaultModel())
}

// ---------------------------------------------------------------------------
// TestLoad_envVar
// ---------------------------------------------------------------------------

// TestLoad_envVar verifies that "$VAR_NAME" strings in config values are
// replaced with the corresponding environment variable at load time.
func TestLoad_envVar(t *testing.T) {
	t.Setenv("TEST_API_KEY", "sk-env-resolved")
	t.Setenv("TEST_BASE_URL", "https://api.example.com/v1")

	yaml := `
config_version: 1
log_level: info

models:
  - name: env-model
    use: openai
    model: gpt-4o-mini
    api_key: $TEST_API_KEY
    base_url: $TEST_BASE_URL

sandbox:
  use: local

memory:
  enabled: false
  injection_enabled: false
`

	path := writeTemp(t, yaml)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	require.Len(t, cfg.Models, 1)
	assert.Equal(t, "sk-env-resolved", cfg.Models[0].APIKey,
		"$TEST_API_KEY should be resolved from environment")
	assert.Equal(t, "https://api.example.com/v1", cfg.Models[0].BaseURL,
		"$TEST_BASE_URL should be resolved from environment")
}

// TestLoad_envVar_missing verifies that an unset "$VAR" fails fast with an error.
func TestLoad_envVar_missing(t *testing.T) {
	// Ensure the variable is definitely unset.
	t.Setenv("GOCLAW_TEST_MISSING_VAR", "")
	os.Unsetenv("GOCLAW_TEST_MISSING_VAR") //nolint:errcheck

	yaml := `
config_version: 1
log_level: info

models:
  - name: missing-env-model
    use: openai
    model: gpt-4o
    api_key: $GOCLAW_TEST_MISSING_VAR

sandbox:
  use: local

memory:
  enabled: false
  injection_enabled: false
`

	path := writeTemp(t, yaml)
	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GOCLAW_TEST_MISSING_VAR")
}

// ---------------------------------------------------------------------------
// TestLoad_extensionsConfig
// ---------------------------------------------------------------------------

// TestLoad_extensionsConfig verifies that extensions_config.json is loaded
// from the same directory as config.yaml and merged into cfg.Extensions.
func TestLoad_extensionsConfig(t *testing.T) {
	t.Setenv("MCP_API_KEY", "token-123")

	yaml := `
config_version: 1
log_level: info

models:
  - name: env-model
    use: openai
    model: gpt-4o-mini
    api_key: sk-test

sandbox:
  use: local

memory:
  enabled: false
  injection_enabled: false
`

	path := writeTemp(t, yaml)
	dir := filepath.Dir(path)

	ext := `{
  "mcpServers": {
    "remote": {
      "enabled": true,
      "type": "http",
      "url": "https://mcp.example.com",
      "headers": {
        "Authorization": "$MCP_API_KEY"
      }
    },
    "disabled": {
      "enabled": false,
      "type": "stdio",
      "command": "python"
    }
  },
  "skills": {
    "my-skill": {"enabled": true}
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "extensions_config.json"), []byte(ext), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)

	require.Len(t, cfg.Extensions.MCPServers, 2)
	assert.Equal(t, "token-123", cfg.Extensions.MCPServers["remote"].Headers["Authorization"])
	assert.Equal(t, "http", cfg.Extensions.MCPServers["remote"].Type)

	enabled := cfg.Extensions.EnabledMCPServers()
	require.Len(t, enabled, 1)
	_, ok := enabled["remote"]
	assert.True(t, ok)

	require.Len(t, cfg.Extensions.Skills, 1)
	assert.True(t, cfg.Extensions.Skills["my-skill"].Enabled)
}

func TestLoad_extensionsConfig_missingEnvVarFails(t *testing.T) {
	t.Setenv("GOCLAW_TEST_MISSING_EXT_VAR", "")
	os.Unsetenv("GOCLAW_TEST_MISSING_EXT_VAR") //nolint:errcheck

	yaml := `
config_version: 1
log_level: info

models:
  - name: env-model
    use: openai
    model: gpt-4o-mini
    api_key: sk-test

sandbox:
  use: local

memory:
  enabled: false
  injection_enabled: false
`

	path := writeTemp(t, yaml)
	dir := filepath.Dir(path)

	ext := `{
  "mcpServers": {
    "remote": {
      "enabled": true,
      "type": "http",
      "url": "https://mcp.example.com",
      "headers": {
        "Authorization": "$GOCLAW_TEST_MISSING_EXT_VAR"
      }
    }
  },
  "skills": {}
}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "extensions_config.json"), []byte(ext), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.NotNil(t, cfg.Extensions)
	require.Contains(t, cfg.Extensions.MCPServers, "remote")
	assert.Equal(t, "", cfg.Extensions.MCPServers["remote"].Headers["Authorization"])
}

// ---------------------------------------------------------------------------
// TestWatch_reload
// ---------------------------------------------------------------------------

// TestWatch_reload verifies that Watch detects a file modification and
// calls the onChange callback with the updated config.
func TestWatch_reload(t *testing.T) {
	initial := `
config_version: 1
log_level: info

models:
  - name: initial-model
    use: openai
    model: gpt-4o
    api_key: sk-initial

sandbox:
  use: local

memory:
  enabled: false
  injection_enabled: false
`
	updated := `
config_version: 2
log_level: debug

models:
  - name: updated-model
    use: openai
    model: gpt-4o-mini
    api_key: sk-updated

sandbox:
  use: local

memory:
  enabled: false
  injection_enabled: false
`

	path := writeTemp(t, initial)

	// Verify initial load.
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "initial-model", cfg.Models[0].Name)

	received := make(chan *AppConfig, 1)
	stop := Watch(path, func(newCfg *AppConfig) {
		select {
		case received <- newCfg:
		default:
		}
	})
	defer stop()

	// Give the watcher time to record the initial mtime.
	time.Sleep(100 * time.Millisecond)

	// Overwrite the config file with updated content. Use a small sleep to
	// ensure the mtime is strictly greater than the recorded value.
	time.Sleep(10 * time.Millisecond)
	require.NoError(t, os.WriteFile(path, []byte(updated), 0o644))

	// Wait for the watcher goroutine to pick up the change (poll interval = 2s).
	select {
	case newCfg := <-received:
		assert.Equal(t, "updated-model", newCfg.Models[0].Name,
			"onChange should receive the updated config")
		assert.Equal(t, 2, newCfg.ConfigVersion)
		assert.Equal(t, "debug", newCfg.LogLevel)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Watch to detect file change")
	}
}

// ---------------------------------------------------------------------------
// TestResolveEnvVars
// ---------------------------------------------------------------------------

// TestResolveEnvVars tests the resolveEnvVars helper in isolation.
func TestResolveEnvVars(t *testing.T) {
	t.Setenv("MY_SECRET", "hunter2")

	tests := []struct {
		input       string
		expected    string
		expectError bool
	}{
		{"$MY_SECRET", "hunter2", false},
		{"literal-value", "literal-value", false},
		{"$DEFINITELY_UNSET_9999", "", true},
		{"", "", false},
	}

	for _, tc := range tests {
		got, err := resolveEnvVars(tc.input, false)
		if tc.expectError {
			require.Error(t, err, "input: %q", tc.input)
			continue
		}
		require.NoError(t, err, "input: %q", tc.input)
		assert.Equal(t, tc.expected, got, "input: %q", tc.input)
	}
}

func TestLoad_ModelConfigExplicitFields(t *testing.T) {
	yaml := `
config_version: 1
log_level: info

models:
  - name: gpt-5
    use: openai
    model: gpt-5
    api_key: sk-test
    api_base: https://api.example.com/v1
    use_responses_api: true
    output_version: responses/v1
    gemini_api_key: gemini-secret
    thinking:
      type: enabled
      budget_tokens: 1024

sandbox:
  use: local

memory:
  enabled: false
  injection_enabled: false
`
	path := writeTemp(t, yaml)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Len(t, cfg.Models, 1)
	m := cfg.Models[0]
	assert.Equal(t, "https://api.example.com/v1", m.APIBase)
	if assert.NotNil(t, m.UseResponsesAPI) {
		assert.True(t, *m.UseResponsesAPI)
	}
	assert.Equal(t, "responses/v1", m.OutputVersion)
	assert.Equal(t, "gemini-secret", m.GeminiAPIKey)
	assert.Equal(t, "enabled", m.Thinking["type"])
}

// ---------------------------------------------------------------------------
// TestSingleton
// ---------------------------------------------------------------------------

// TestSingleton verifies the GetAppConfig / SetAppConfig / ResetAppConfig
// singleton helpers work correctly.
func TestSingleton(t *testing.T) {
	defer ResetAppConfig()

	custom := &AppConfig{LogLevel: "custom", ConfigVersion: 99}
	SetAppConfig(custom)

	got, err := GetAppConfig()
	require.NoError(t, err)
	assert.Equal(t, "custom", got.LogLevel)
	assert.Equal(t, 99, got.ConfigVersion)

	ResetAppConfig()
	// After reset, GetAppConfig will try to load from disk; this will fail
	// in the test environment (no config.yaml in CWD), which is expected.
	_, err = GetAppConfig()
	assert.Error(t, err, "GetAppConfig after reset should fail without a real config file")
}

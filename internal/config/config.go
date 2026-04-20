// Package config provides configuration loading, validation, and hot-reload
// for the GoClaw AI Agent Harness. It mirrors the structure of DeerFlow's
// Python config system, supporting YAML-based config files with $ENV_VAR
// substitution and mtime-based hot reload.
package config

// ---------------------------------------------------------------------------
// Top-level AppConfig
// ---------------------------------------------------------------------------

// AppConfig is the top-level configuration for GoClaw. It mirrors
// DeerFlow's AppConfig Pydantic model.
type AppConfig struct {
	// ConfigVersion is bumped whenever the schema changes. Used to warn
	// users whose config.yaml may be outdated.
	ConfigVersion int `yaml:"config_version"`

	// LogLevel controls verbosity for goclaw modules (debug/info/warning/error).
	LogLevel string `yaml:"log_level"`

	// Server configures gateway serving behavior.
	Server ServerConfig `yaml:"server,omitempty"`

	// Models is the list of available LLM model configurations.
	Models []ModelConfig `yaml:"models"`

	// ToolGroups organises tools into named groups (web, file:read, bash, …).
	ToolGroups []ToolGroupConfig `yaml:"tool_groups"`

	// Tools is the list of individual tool configurations.
	Tools []ToolConfig `yaml:"tools"`

	// Sandbox configures the execution sandbox provider.
	Sandbox SandboxConfig `yaml:"sandbox"`

	// Memory configures the persistent memory mechanism.
	Memory MemoryConfig `yaml:"memory"`

	// Checkpointer configures checkpoint persistence backend.
	// When nil, in-memory checkpointing is used.
	Checkpointer *CheckpointerConfig `yaml:"checkpointer,omitempty"`

	// Skills configures the skills loader.
	Skills SkillsConfig `yaml:"skills"`

	// Title configures automatic conversation title generation.
	Title TitleConfig `yaml:"title"`

	// Summarization configures automatic context summarization.
	Summarization SummarizationConfig `yaml:"summarization"`

	// TokenUsage configures token usage tracking.
	TokenUsage TokenUsageConfig `yaml:"token_usage"`

	// Guardrails configures authorization policies.
	Guardrails GuardrailsConfig `yaml:"guardrails"`

	// Subagents configures sub-agent execution timeouts.
	Subagents SubagentsConfig `yaml:"subagents"`

	// Channels configures IM channel integrations.
	Channels *ChannelsConfig `yaml:"channels,omitempty"`

	// Agents holds custom agent configurations.
	Agents map[string]AgentConfig `yaml:"agents,omitempty"`

	// ExtensionsRef holds optional path override for extensions config.
	ExtensionsRef ExtensionsConfigRef `yaml:"extensions,omitempty"`

	// Extensions holds MCP server and skill enablement state loaded from
	// extensions_config.json (or extensions.json).
	Extensions ExtensionsConfig `yaml:"-"` // populated separately

	// ToolSearch configures lazy loading of MCP tools.
	ToolSearch ToolSearchConfig `yaml:"tool_search,omitempty"`

	// Plugins configures the plugin system.
	Plugins PluginsConfig `yaml:"plugins,omitempty"`
}

// GetModelConfig returns the ModelConfig with the given name, or nil if not found.
func (c *AppConfig) GetModelConfig(name string) *ModelConfig {
	for i := range c.Models {
		if c.Models[i].Name == name {
			return &c.Models[i]
		}
	}
	return nil
}

// GetToolConfig returns the ToolConfig with the given name, or nil if not found.
func (c *AppConfig) GetToolConfig(name string) *ToolConfig {
	for i := range c.Tools {
		if c.Tools[i].Name == name {
			return &c.Tools[i]
		}
	}
	return nil
}

// DefaultModel returns the first model in the list, or nil if Models is empty.
func (c *AppConfig) DefaultModel() *ModelConfig {
	if len(c.Models) == 0 {
		return nil
	}
	return &c.Models[0]
}

// ---------------------------------------------------------------------------
// ServerConfig
// ---------------------------------------------------------------------------

// ServerConfig configures gateway HTTP server behavior.
type ServerConfig struct {
	// Address is the listen address for gateway server (e.g. ":8001").
	Address string `yaml:"address,omitempty"`

	// CORSOrigins is the explicit allowlist for CORS origins.
	// When empty, gateway falls back to permissive local-development mode.
	CORSOrigins []string `yaml:"cors_origins,omitempty"`
}

// ---------------------------------------------------------------------------
// ModelConfig
// ---------------------------------------------------------------------------

// ModelConfig describes a single LLM provider configuration.
// Extra YAML keys (e.g. api_base, request_timeout, extra_body) are preserved
// in Extra for pass-through to the Eino model factory.
type ModelConfig struct {
	// Name is the unique identifier used to reference this model in config.
	Name string `yaml:"name"`

	// DisplayName is an optional human-readable name shown in the UI.
	DisplayName string `yaml:"display_name,omitempty"`

	// Description is an optional description shown in the UI.
	Description string `yaml:"description,omitempty"`

	// Use is the provider class path, e.g. "openai" or "anthropic".
	// In Go we use short provider identifiers rather than Python class paths.
	Use string `yaml:"use"`

	// Model is the underlying model name passed to the provider API.
	Model string `yaml:"model"`

	// APIKey is the API key for authenticating with the provider.
	// May reference an environment variable via "$ENV_VAR" syntax.
	APIKey string `yaml:"api_key,omitempty"`

	// BaseURL overrides the default provider API endpoint.
	BaseURL string `yaml:"base_url,omitempty"`

	// APIBase is an alias used by some configs/providers.
	// Kept as explicit field for parity with DeerFlow-side model config.
	APIBase string `yaml:"api_base,omitempty"`

	// GeminiAPIKey is an optional dedicated key for Gemini-compatible providers.
	GeminiAPIKey string `yaml:"gemini_api_key,omitempty"`

	// UseResponsesAPI routes OpenAI-compatible calls via Responses API when true.
	UseResponsesAPI *bool `yaml:"use_responses_api,omitempty"`

	// OutputVersion controls structured output format, e.g. "responses/v1".
	OutputVersion string `yaml:"output_version,omitempty"`

	// MaxTokens sets the maximum number of output tokens per request.
	MaxTokens int `yaml:"max_tokens,omitempty"`

	// Temperature controls randomness (0.0–2.0 depending on provider).
	Temperature *float64 `yaml:"temperature,omitempty"`

	// MaxRetries controls the number of automatic retries on transient errors.
	MaxRetries int `yaml:"max_retries,omitempty"`

	// RequestTimeout is the per-request timeout in seconds.
	RequestTimeout float64 `yaml:"request_timeout,omitempty"`

	// SupportsVision indicates the model can process image inputs.
	SupportsVision bool `yaml:"supports_vision,omitempty"`

	// SupportsThinking indicates the model supports extended reasoning.
	SupportsThinking bool `yaml:"supports_thinking,omitempty"`

	// SupportsReasoningEffort indicates the model accepts a reasoning_effort param.
	SupportsReasoningEffort bool `yaml:"supports_reasoning_effort,omitempty"`

	// WhenThinkingEnabled holds extra provider-specific params injected when
	// thinking/reasoning mode is active (mirrors DeerFlow's when_thinking_enabled).
	WhenThinkingEnabled map[string]any `yaml:"when_thinking_enabled,omitempty"`

	// Thinking is a shortcut config merged into when_thinking_enabled.thinking.
	Thinking map[string]any `yaml:"thinking,omitempty"`

	// Extra captures any additional YAML fields not covered above.
	// These are forwarded verbatim to the Eino model factory.
	Extra map[string]any `yaml:",inline"`
}

// ---------------------------------------------------------------------------
// ToolGroupConfig / ToolConfig
// ---------------------------------------------------------------------------

// ToolGroupConfig defines a named group for organising tools.
type ToolGroupConfig struct {
	// Name is the unique group identifier (e.g. "web", "file:read", "bash").
	Name string `yaml:"name"`
}

// ToolConfig describes a single tool available to the agent.
type ToolConfig struct {
	// Name is the unique tool identifier used in system prompt and tool calls.
	Name string `yaml:"name"`

	// Group is the ToolGroupConfig.Name this tool belongs to.
	Group string `yaml:"group"`

	// Use is the Go package + symbol path for the tool factory, e.g.
	// "goclaw/internal/tools/websearch:WebSearchTool".
	Use string `yaml:"use"`

	// Extra captures provider-specific settings (max_results, timeout, …).
	Extra map[string]any `yaml:",inline"`
}

// ---------------------------------------------------------------------------
// SandboxConfig
// ---------------------------------------------------------------------------

// VolumeMountConfig defines a host-to-container directory mapping.
type VolumeMountConfig struct {
	// HostPath is the absolute path on the host machine.
	HostPath string `yaml:"host_path"`

	// ContainerPath is the path inside the container.
	ContainerPath string `yaml:"container_path"`

	// ReadOnly prevents the container from writing to this mount.
	ReadOnly bool `yaml:"read_only,omitempty"`
}

// SandboxConfig configures the sandbox execution environment.
//
// use = "local" selects the LocalSandboxProvider (direct host execution).
// use = "docker" selects the DockerSandboxProvider (container isolation).
type SandboxConfig struct {
	// Use selects the sandbox provider implementation.
	// Valid values: "local", "docker".
	Use string `yaml:"use"`

	// AllowHostBash enables the bash tool to execute directly on the host
	// when using the local sandbox. Disabled by default for safety.
	AllowHostBash bool `yaml:"allow_host_bash,omitempty"`

	// StrictDocker controls whether sandbox.use=docker must fail hard
	// when the Docker provider cannot be initialized.
	StrictDocker bool `yaml:"strict_docker,omitempty"`

	// Image is the Docker image used by the docker sandbox provider.
	Image string `yaml:"image,omitempty"`

	// Port is the base port number for docker sandbox containers.
	Port int `yaml:"port,omitempty"`

	// Replicas is the maximum number of concurrent sandbox containers.
	// When the limit is reached, the LRU container is evicted.
	Replicas int `yaml:"replicas,omitempty"`

	// ContainerPrefix is the Docker container name prefix.
	ContainerPrefix string `yaml:"container_prefix,omitempty"`

	// IdleTimeout is the number of seconds a sandbox can be idle before
	// being released. Set to 0 to disable idle eviction.
	IdleTimeout int `yaml:"idle_timeout,omitempty"`

	// Mounts lists host directories shared into the sandbox container.
	Mounts []VolumeMountConfig `yaml:"mounts,omitempty"`

	// Environment holds env vars injected into sandbox containers.
	// Values starting with "$" are resolved from the host environment.
	Environment map[string]string `yaml:"environment,omitempty"`

	// BashOutputMaxChars is the maximum characters kept from bash output.
	// Excess is middle-truncated. Set to 0 to disable truncation.
	BashOutputMaxChars int `yaml:"bash_output_max_chars,omitempty"`

	// ReadFileOutputMaxChars is the maximum characters kept from read_file output.
	// Excess is head-truncated. Set to 0 to disable truncation.
	ReadFileOutputMaxChars int `yaml:"read_file_output_max_chars,omitempty"`
}

// ---------------------------------------------------------------------------
// MemoryConfig
// ---------------------------------------------------------------------------

// MemoryConfig configures the global persistent memory mechanism.
// Memory extracts facts from conversations and injects them into future prompts.
type MemoryConfig struct {
	// Enabled controls whether the memory middleware is active.
	Enabled bool `yaml:"enabled"`

	// StoragePath is the file path for the memory JSON store.
	// Relative paths are resolved against the current working directory.
	StoragePath string `yaml:"storage_path,omitempty"`

	// DebounceSeconds is the delay before processing queued memory updates.
	// Must be between 1 and 300.
	DebounceSeconds int `yaml:"debounce_seconds,omitempty"`

	// ModelName selects which model performs fact extraction.
	// nil / "" means use the default (first) model.
	ModelName string `yaml:"model_name,omitempty"`

	// MaxFacts is the upper limit of stored facts.
	MaxFacts int `yaml:"max_facts,omitempty"`

	// FactConfidenceThreshold is the minimum confidence [0,1] for storing a fact.
	FactConfidenceThreshold float64 `yaml:"fact_confidence_threshold,omitempty"`

	// InjectionEnabled controls whether stored facts are injected into prompts.
	InjectionEnabled bool `yaml:"injection_enabled"`

	// MaxInjectionTokens limits the token budget used for memory injection.
	MaxInjectionTokens int `yaml:"max_injection_tokens,omitempty"`
}

// ---------------------------------------------------------------------------
// CheckpointerConfig
// ---------------------------------------------------------------------------

// CheckpointerConfig configures state persistence backend.
type CheckpointerConfig struct {
	// Type is one of: memory, sqlite, postgres.
	Type string `yaml:"type"`

	// ConnectionString is required for sqlite/postgres backends.
	ConnectionString string `yaml:"connection_string,omitempty"`
}

// ---------------------------------------------------------------------------
// SkillsConfig
// ---------------------------------------------------------------------------

// SkillsConfig locates the skills directory used by the agent.
type SkillsConfig struct {
	// Path is the host-side path to the skills directory.
	// Relative paths are resolved against the goclaw root directory.
	Path string `yaml:"path,omitempty"`

	// ContainerPath is the mount path inside the sandbox container.
	// Default: /mnt/skills
	ContainerPath string `yaml:"container_path,omitempty"`
}

// ---------------------------------------------------------------------------
// TokenUsageConfig / GuardrailsConfig
// ---------------------------------------------------------------------------

// TokenUsageConfig holds configuration for token usage tracking.
// When enabled, logs input/output/total tokens for each model call.
// Mirrors DeerFlow's TokenUsageConfig.
type TokenUsageConfig struct {
	// Enabled controls whether token usage tracking middleware is active.
	Enabled bool `yaml:"enabled"`
}

// GuardrailProviderConfig configures the guardrail provider implementation.
// Use is a class path like "package.module:ClassName".
type GuardrailProviderConfig struct {
	// Use is the class path for the provider implementation.
	Use string `yaml:"use"`

	// Config contains provider-specific configuration.
	Config map[string]any `yaml:"config,omitempty"`
}

// GuardrailsConfig configures authorization policies for tool execution.
// When enabled, all tool calls are evaluated against the provider before execution.
// Mirrors DeerFlow's GuardrailsConfig.
type GuardrailsConfig struct {
	// Enabled controls whether guardrail middleware is active.
	Enabled bool `yaml:"enabled"`

	// FailClosed controls behavior when provider errors.
	// If true (default), deny on error. If false, allow on error.
	FailClosed bool `yaml:"fail_closed"`

	// Passport is passed to provider as request.agent_id.
	// Can be a file path, managed agent ID, or null.
	Passport *string `yaml:"passport,omitempty"`

	// Provider configures the authorization provider implementation.
	Provider *GuardrailProviderConfig `yaml:"provider,omitempty"`
}

// ToolSearchConfig configures lazy loading of MCP tools.
// When enabled, MCP tools are loaded on-demand rather than at startup.
type ToolSearchConfig struct {
	// Enabled controls whether MCP tool lazy loading is active.
	Enabled bool `yaml:"enabled"`
}

// ---------------------------------------------------------------------------
// TitleConfig / SummarizationConfig / SubagentsConfig
// ---------------------------------------------------------------------------

// TitleConfig configures automatic conversation title generation.
type TitleConfig struct {
	Enabled        bool   `yaml:"enabled"`
	MaxWords       int    `yaml:"max_words,omitempty"`
	MaxChars       int    `yaml:"max_chars,omitempty"`
	ModelName      string `yaml:"model_name,omitempty"`
	PromptTemplate string `yaml:"prompt_template,omitempty"` // Custom title generation prompt
}

// SummarizationTrigger defines a single summarization trigger condition.
type SummarizationTrigger struct {
	// Type is one of "tokens", "messages", or "fraction".
	Type  string  `yaml:"type"`
	Value float64 `yaml:"value"`
}

// SummarizationKeep defines the retention policy after summarization.
type SummarizationKeep struct {
	// Type is one of "messages", "tokens", or "fraction".
	Type  string  `yaml:"type"`
	Value float64 `yaml:"value"`
}

// SummarizationConfig configures automatic context summarization.
type SummarizationConfig struct {
	Enabled               bool                   `yaml:"enabled"`
	ModelName             string                 `yaml:"model_name,omitempty"`
	Trigger               []SummarizationTrigger `yaml:"trigger,omitempty"`
	Keep                  SummarizationKeep      `yaml:"keep,omitempty"`
	TrimTokensToSummarize int                    `yaml:"trim_tokens_to_summarize,omitempty"`
	SummaryPrompt         string                 `yaml:"summary_prompt,omitempty"`
}

// SubagentOverrideConfig allows per-agent timeout overrides.
type SubagentOverrideConfig struct {
	TimeoutSeconds int `yaml:"timeout_seconds,omitempty"`
}

// SubagentTypeConfig configures a subagent type with capabilities and overrides.
type SubagentTypeConfig struct {
	Enabled         bool     `yaml:"enabled,omitempty"`
	Description     string   `yaml:"description,omitempty"`
	Model           string   `yaml:"model,omitempty"`
	TimeoutSecs     int      `yaml:"timeout_seconds,omitempty"`
	SystemPrompt    string   `yaml:"system_prompt,omitempty"`
	MaxTurns        int      `yaml:"max_turns,omitempty"`
	AllowedTools    []string `yaml:"allowed_tools,omitempty"`
	DisallowedTools []string `yaml:"disallowed_tools,omitempty"`
}

// SubagentsConfig configures sub-agent execution behaviour.
type SubagentsConfig struct {
	// TimeoutSeconds is the default timeout for all sub-agents (default: 900).
	TimeoutSeconds int `yaml:"timeout_seconds,omitempty"`

	// MaxConcurrent is the global concurrency limit for subagent execution.
	MaxConcurrent int `yaml:"max_concurrent,omitempty"`

	// Types holds configuration for predefined subagent types.
	Types map[string]SubagentTypeConfig `yaml:"types,omitempty"`

	// Agents holds per-agent timeout overrides keyed by agent name.
	Agents map[string]SubagentOverrideConfig `yaml:"agents,omitempty"`
}

// ---------------------------------------------------------------------------
// ChannelsConfig
// ---------------------------------------------------------------------------

// SessionConfig holds default session parameters for IM channels.
type SessionConfig struct {
	AssistantID string         `yaml:"assistant_id,omitempty"`
	Config      map[string]any `yaml:"config,omitempty"`
	Context     map[string]any `yaml:"context,omitempty"`
}

// FeishuConfig holds Feishu/Lark channel configuration.
type FeishuConfig struct {
	Enabled     bool   `yaml:"enabled"`
	AppID       string `yaml:"app_id,omitempty"`
	AppSecret   string `yaml:"app_secret,omitempty"`
	Domain      string `yaml:"domain,omitempty"`
	WebhookPort int    `yaml:"webhook_port,omitempty"`
}

// SlackConfig holds Slack channel configuration.
type SlackConfig struct {
	Enabled      bool     `yaml:"enabled"`
	BotToken     string   `yaml:"bot_token,omitempty"`
	AppToken     string   `yaml:"app_token,omitempty"`
	AllowedUsers []string `yaml:"allowed_users,omitempty"`
}

// TelegramConfig holds Telegram channel configuration.
type TelegramConfig struct {
	Enabled      bool                      `yaml:"enabled"`
	BotToken     string                    `yaml:"bot_token,omitempty"`
	AllowedUsers []string                  `yaml:"allowed_users,omitempty"`
	Session      *SessionConfig            `yaml:"session,omitempty"`
	Users        map[string]*SessionConfig `yaml:"users,omitempty"`
}

// ChannelsConfig holds all IM channel configurations.
type ChannelsConfig struct {
	LangGraphURL string          `yaml:"langgraph_url,omitempty"`
	GatewayURL   string          `yaml:"gateway_url,omitempty"`
	Session      *SessionConfig  `yaml:"session,omitempty"`
	Feishu       *FeishuConfig   `yaml:"feishu,omitempty"`
	Slack        *SlackConfig    `yaml:"slack,omitempty"`
	Telegram     *TelegramConfig `yaml:"telegram,omitempty"`
}

// ---------------------------------------------------------------------------
// AgentConfig (custom agent definitions)
// ---------------------------------------------------------------------------

// AgentConfig holds configuration for a custom agent.
type AgentConfig struct {
	Enabled     bool     `yaml:"enabled" json:"enabled"`
	Model       string   `yaml:"model,omitempty" json:"model,omitempty"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Skills      []string `yaml:"skills,omitempty" json:"skills,omitempty"`
	ToolGroups  []string `yaml:"tool_groups,omitempty" json:"tool_groups,omitempty"`
}

// ---------------------------------------------------------------------------
// ExtensionsConfig (MCP servers + skill state)
// ---------------------------------------------------------------------------

// DefaultExtensionsConfigPath is the default location for extensions_config.json.
const DefaultExtensionsConfigPath = "extensions_config.json"

// ExtensionsConfigRef holds optional path override for extensions config.
type ExtensionsConfigRef struct {
	ConfigPath string `yaml:"config_path,omitempty" json:"config_path,omitempty"`
}

// MCPOAuthConfig configures OAuth for MCP HTTP/SSE servers.
type MCPOAuthConfig struct {
	// Enabled controls whether OAuth is active for this server.
	Enabled bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`

	// TokenURL is the OAuth token endpoint.
	TokenURL string `yaml:"token_url,omitempty" json:"token_url,omitempty"`

	// ClientID is the OAuth client id.
	ClientID string `yaml:"client_id,omitempty" json:"client_id,omitempty"`

	// ClientSecret is the OAuth client secret.
	ClientSecret string `yaml:"client_secret,omitempty" json:"client_secret,omitempty"`

	// Scope is the optional OAuth scope string.
	Scope string `yaml:"scope,omitempty" json:"scope,omitempty"`

	// GrantType defaults to client_credentials.
	GrantType string `yaml:"grant_type,omitempty" json:"grant_type,omitempty"`

	// RefreshToken allows refresh_token grant when provided.
	RefreshToken string `yaml:"refresh_token,omitempty" json:"refresh_token,omitempty"`

	// Audience is the OAuth audience field (required for Auth0, Okta, etc.).
	Audience string `yaml:"audience,omitempty" json:"audience,omitempty"`

	// TokenField is the field name in the token response that contains the access token.
	// Defaults to "access_token".
	TokenField string `yaml:"token_field,omitempty" json:"token_field,omitempty"`

	// TokenTypeField is the field name for the token type (e.g., "Bearer").
	// Defaults to "token_type".
	TokenTypeField string `yaml:"token_type_field,omitempty" json:"token_type_field,omitempty"`

	// ExpiresInField is the field name for the expires_in value.
	// Defaults to "expires_in".
	ExpiresInField string `yaml:"expires_in_field,omitempty" json:"expires_in_field,omitempty"`

	// DefaultTokenType is the default token type when not returned by the server.
	// Defaults to "Bearer".
	DefaultTokenType string `yaml:"default_token_type,omitempty" json:"default_token_type,omitempty"`

	// RefreshSkewSeconds is the number of seconds before expiration to refresh.
	// Defaults to 60 seconds.
	RefreshSkewSeconds int `yaml:"refresh_skew_seconds,omitempty" json:"refresh_skew_seconds,omitempty"`

	// ExtraTokenParams holds additional parameters sent with the token request.
	ExtraTokenParams map[string]string `yaml:"extra_token_params,omitempty" json:"extra_token_params,omitempty"`
}

// MCPServerConfig configures a single MCP server endpoint.
type MCPServerConfig struct {
	// Enabled controls whether this server is started.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Type is the transport: "stdio", "sse", or "http".
	Type string `yaml:"type" json:"type"`

	// Command is the executable to launch for stdio transport.
	Command string `yaml:"command,omitempty" json:"command,omitempty"`

	// Args are command-line arguments for stdio transport.
	Args []string `yaml:"args,omitempty" json:"args,omitempty"`

	// Env holds environment variables injected into the MCP server process.
	// Values starting with "$" are resolved from the host environment.
	Env map[string]string `yaml:"env,omitempty" json:"env,omitempty"`

	// URL is the server endpoint for sse/http transport.
	URL string `yaml:"url,omitempty" json:"url,omitempty"`

	// Headers are HTTP headers sent with sse/http requests.
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`

	// Description is a human-readable summary of the server's capabilities.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// OAuth config for HTTP/SSE transport authorization.
	OAuth *MCPOAuthConfig `yaml:"oauth,omitempty" json:"oauth,omitempty"`
}

// SkillStateConfig stores the enablement state for a skill.
type SkillStateConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

// ExtensionsConfig holds MCP server definitions and per-skill enable flags.
// This is normally loaded from extensions_config.json (or extensions.json).
type ExtensionsConfig struct {
	// MCPServers maps a server name to its configuration.
	MCPServers map[string]MCPServerConfig `yaml:"mcpServers" json:"mcpServers"`

	// Skills maps a skill name to its state.
	Skills map[string]SkillStateConfig `yaml:"skills" json:"skills"`
}

// EnabledMCPServers returns only the servers that have Enabled == true.
func (e *ExtensionsConfig) EnabledMCPServers() map[string]MCPServerConfig {
	result := make(map[string]MCPServerConfig, len(e.MCPServers))
	for name, srv := range e.MCPServers {
		if srv.Enabled {
			result[name] = srv
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// PluginsConfig (plugin system configuration)
// ---------------------------------------------------------------------------

// PluginConfig configures a single plugin.
type PluginConfig struct {
	// Enabled controls whether this plugin is active.
	Enabled bool `yaml:"enabled"`

	// Path is an optional path to the plugin binary or script.
	// For built-in plugins, this can be empty.
	Path string `yaml:"path,omitempty"`

	// Config holds plugin-specific configuration passed to Plugin.Init.
	Config map[string]any `yaml:"config,omitempty"`
}

// PluginsConfig configures the plugin system.
type PluginsConfig struct {
	// Enabled controls whether the plugin system is active.
	Enabled bool `yaml:"enabled"`

	// Directory is the base directory for plugin discovery.
	// Relative paths are resolved against the current working directory.
	Directory string `yaml:"directory,omitempty"`

	// Plugins maps plugin names to their configurations.
	Plugins map[string]PluginConfig `yaml:"plugins,omitempty"`
}

// GetEnabledPlugins returns only the plugins that have Enabled == true.
func (p *PluginsConfig) GetEnabledPlugins() map[string]PluginConfig {
	result := make(map[string]PluginConfig, len(p.Plugins))
	for name, plugin := range p.Plugins {
		if plugin.Enabled {
			result[name] = plugin
		}
	}
	return result
}

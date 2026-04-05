package channels

// SessionConfig holds session-level configuration for IM channels.
// It supports a three-tier override system: default → channel → user.
type SessionConfig struct {
	// AssistantID is the assistant/agent identifier to use.
	// Can be "lead_agent" or a custom agent name.
	AssistantID string `json:"assistant_id,omitempty"`

	// Config holds run configuration parameters (e.g., model settings).
	Config map[string]any `json:"config,omitempty"`

	// Context holds run context parameters (e.g., thread_id, agent_name).
	Context map[string]any `json:"context,omitempty"`

	// Users holds per-user session overrides.
	// Key is user_id, value is user-specific session config.
	Users map[string]*SessionConfig `json:"users,omitempty"`
}

// RunParams holds the resolved parameters for a single agent run.
type RunParams struct {
	// AssistantID is the resolved assistant identifier.
	AssistantID string

	// Config is the merged run configuration.
	Config map[string]any

	// Context is the merged run context.
	Context map[string]any
}

// SessionResolver resolves session configuration across three tiers:
// default → channel → user.
type SessionResolver struct {
	// Default is the global default session config.
	Default *SessionConfig

	// ChannelSessions maps channel name to channel-level session config.
	ChannelSessions map[string]*SessionConfig
}

// NewSessionResolver creates a new session resolver.
func NewSessionResolver(defaultSession *SessionConfig, channelSessions map[string]*SessionConfig) *SessionResolver {
	if channelSessions == nil {
		channelSessions = make(map[string]*SessionConfig)
	}
	return &SessionResolver{
		Default:         defaultSession,
		ChannelSessions: channelSessions,
	}
}

// Resolve computes the effective RunParams for a given message.
// Resolution order: default → channel → user (last wins).
func (r *SessionResolver) Resolve(msg IncomingMessage, threadID string) *RunParams {
	// Start with defaults
	params := &RunParams{
		AssistantID: "lead_agent", // default
		Config:      make(map[string]any),
		Context:     make(map[string]any),
	}

	// Layer 1: Default session
	if r.Default != nil {
		params.AssistantID = firstNonEmpty(params.AssistantID, r.Default.AssistantID)
		params.Config = mergeMaps(params.Config, r.Default.Config)
		params.Context = mergeMaps(params.Context, r.Default.Context)
	}

	// Layer 2: Channel session
	channelSession := r.ChannelSessions[msg.Channel]
	if channelSession == nil && r.Default != nil {
		// Check if channel session is nested under Default (legacy config format)
		// This supports: channels.session.feishu.users...
	}

	// Layer 3: User session (within channel)
	var userSession *SessionConfig
	if channelSession != nil && channelSession.Users != nil {
		userSession = channelSession.Users[msg.UserID]
	}

	// Apply channel layer
	if channelSession != nil {
		params.AssistantID = firstNonEmpty(params.AssistantID, channelSession.AssistantID)
		params.Config = mergeMaps(params.Config, channelSession.Config)
		params.Context = mergeMaps(params.Context, channelSession.Context)
	}

	// Apply user layer
	if userSession != nil {
		params.AssistantID = firstNonEmpty(params.AssistantID, userSession.AssistantID)
		params.Config = mergeMaps(params.Config, userSession.Config)
		params.Context = mergeMaps(params.Context, userSession.Context)
	}

	// Always set thread_id in context
	params.Context["thread_id"] = threadID

	// Normalize: if assistant_id is not "lead_agent", set agent_name in context
	if params.AssistantID != "" && params.AssistantID != "lead_agent" {
		params.Context["agent_name"] = params.AssistantID
		params.AssistantID = "lead_agent" // route through lead_agent
	}

	return params
}

// firstNonEmpty returns the first non-empty string, or the fallback.
func firstNonEmpty(fallback, s string) string {
	if s != "" {
		return s
	}
	return fallback
}

// mergeMaps returns a new map with all keys from base merged with override.
// Override values take precedence.
func mergeMaps(base, override map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range base {
		result[k] = v
	}
	for k, v := range override {
		result[k] = v
	}
	return result
}

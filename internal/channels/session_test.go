package channels

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSessionResolver_Resolve_DefaultOnly(t *testing.T) {
	resolver := NewSessionResolver(&SessionConfig{
		AssistantID: "custom_agent",
		Config:      map[string]any{"model": "gpt-4"},
		Context:     map[string]any{"locale": "en"},
	}, nil)

	msg := IncomingMessage{Channel: "telegram", ChatID: "123", UserID: "user1"}
	params := resolver.Resolve(msg, "thread-123")

	assert.Equal(t, "lead_agent", params.AssistantID)
	assert.Equal(t, "custom_agent", params.Context["agent_name"])
	assert.Equal(t, "gpt-4", params.Config["model"])
	assert.Equal(t, "en", params.Context["locale"])
	assert.Equal(t, "thread-123", params.Context["thread_id"])
}

func TestSessionResolver_Resolve_ChannelOverride(t *testing.T) {
	resolver := NewSessionResolver(&SessionConfig{
		AssistantID: "default_agent",
		Config:      map[string]any{"model": "gpt-3.5"},
	}, map[string]*SessionConfig{
		"slack": {
			AssistantID: "slack_agent",
			Config:      map[string]any{"model": "gpt-4"},
		},
	})

	// Slack user should get channel config
	msg := IncomingMessage{Channel: "slack", ChatID: "C123", UserID: "U1"}
	params := resolver.Resolve(msg, "thread-456")

	assert.Equal(t, "lead_agent", params.AssistantID)
	assert.Equal(t, "slack_agent", params.Context["agent_name"])
	assert.Equal(t, "gpt-4", params.Config["model"])

	// Telegram user should get default config
	msg2 := IncomingMessage{Channel: "telegram", ChatID: "T123", UserID: "U2"}
	params2 := resolver.Resolve(msg2, "thread-789")

	assert.Equal(t, "lead_agent", params2.AssistantID)
	assert.Equal(t, "default_agent", params2.Context["agent_name"])
	assert.Equal(t, "gpt-3.5", params2.Config["model"])
}

func TestSessionResolver_Resolve_UserOverride(t *testing.T) {
	resolver := NewSessionResolver(&SessionConfig{
		AssistantID: "default_agent",
		Config:      map[string]any{"model": "gpt-3.5"},
	}, map[string]*SessionConfig{
		"slack": {
			AssistantID: "slack_agent",
			Config:      map[string]any{"model": "gpt-4"},
			Users: map[string]*SessionConfig{
				"vip_user": {
					AssistantID: "vip_agent",
					Config:      map[string]any{"model": "gpt-4-turbo", "priority": "high"},
				},
			},
		},
	})

	// VIP user should get user-level config
	msg := IncomingMessage{Channel: "slack", ChatID: "C123", UserID: "vip_user"}
	params := resolver.Resolve(msg, "thread-111")

	assert.Equal(t, "lead_agent", params.AssistantID)
	assert.Equal(t, "vip_agent", params.Context["agent_name"])
	assert.Equal(t, "gpt-4-turbo", params.Config["model"])
	assert.Equal(t, "high", params.Config["priority"])

	// Regular user should get channel config
	msg2 := IncomingMessage{Channel: "slack", ChatID: "C123", UserID: "regular_user"}
	params2 := resolver.Resolve(msg2, "thread-222")

	assert.Equal(t, "lead_agent", params2.AssistantID)
	assert.Equal(t, "slack_agent", params2.Context["agent_name"])
	assert.Equal(t, "gpt-4", params2.Config["model"])
	assert.Nil(t, params2.Config["priority"])
}

func TestSessionResolver_Resolve_LeadAgentPassthrough(t *testing.T) {
	// When assistant_id is already "lead_agent", no agent_name should be set
	resolver := NewSessionResolver(&SessionConfig{
		AssistantID: "lead_agent",
	}, nil)

	msg := IncomingMessage{Channel: "telegram", ChatID: "123", UserID: "user1"}
	params := resolver.Resolve(msg, "thread-123")

	assert.Equal(t, "lead_agent", params.AssistantID)
	assert.Nil(t, params.Context["agent_name"]) // should not be set
}

func TestSessionResolver_Resolve_ThreadIDAlwaysSet(t *testing.T) {
	resolver := NewSessionResolver(nil, nil)

	msg := IncomingMessage{Channel: "telegram", ChatID: "123", UserID: "user1"}
	params := resolver.Resolve(msg, "my-thread-456")

	assert.Equal(t, "lead_agent", params.AssistantID)
	assert.Equal(t, "my-thread-456", params.Context["thread_id"])
}

func TestMergeMaps(t *testing.T) {
	tests := []struct {
		name     string
		base     map[string]any
		override map[string]any
		expected map[string]any
	}{
		{
			name:     "both nil",
			base:     nil,
			override: nil,
			expected: map[string]any{},
		},
		{
			name:     "override nil",
			base:     map[string]any{"a": 1},
			override: nil,
			expected: map[string]any{"a": 1},
		},
		{
			name:     "base nil",
			base:     nil,
			override: map[string]any{"b": 2},
			expected: map[string]any{"b": 2},
		},
		{
			name:     "merge",
			base:     map[string]any{"a": 1, "c": 3},
			override: map[string]any{"b": 2, "c": 99},
			expected: map[string]any{"a": 1, "b": 2, "c": 99},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeMaps(tt.base, tt.override)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFirstNonEmpty(t *testing.T) {
	assert.Equal(t, "fallback", firstNonEmpty("fallback", ""))
	assert.Equal(t, "value", firstNonEmpty("fallback", "value"))
	assert.Equal(t, "", firstNonEmpty("", ""))
}

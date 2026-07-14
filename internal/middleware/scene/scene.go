// Package scene provides a middleware that injects scene-specific context
// into the agent's system prompt and metadata, enabling a single agent instance
// to adapt its behavior based on the active scene (e.g., "paper_reading", "kids_edu").
//
// Scene configuration is defined in config.yaml under a "scenes" key and/or
// passed per-request via RunConfig.Scene. The middleware reads the scene from
// State.Extra["scene"] (set by the gateway/agent from RunConfig).
package scene

import (
	"context"
	"fmt"
	"strings"

	"goclaw/internal/logging"
	"goclaw/internal/middleware"
)

// SceneConfig defines the configuration for a single scene.
type SceneConfig struct {
	// SceneID is the unique identifier for this scene (e.g., "paper_reading").
	SceneID string `yaml:"scene_id" json:"scene_id"`

	// Description is a human-readable label for logging.
	Description string `yaml:"description" json:"description"`

	// PromptSuffix is appended to the system prompt as <scene_context> when this scene is active.
	// Use this to give the agent scene-specific behavioral instructions.
	PromptSuffix string `yaml:"prompt_suffix" json:"prompt_suffix"`

	// SafetyProfile identifies the content safety policy to apply.
	// Typical values: "default", "strict", "kids".
	// The GuardrailMiddleware uses this to select the appropriate rule set.
	SafetyProfile string `yaml:"safety_profile" json:"safety_profile"`
}

// Config holds the overall scene middleware configuration.
type Config struct {
	// DefaultScene is the scene ID to use when RunConfig.Scene is empty.
	DefaultScene string `yaml:"default_scene" json:"default_scene"`

	// Scenes is the map of known scenes, keyed by scene ID.
	Scenes map[string]SceneConfig `yaml:"scenes" json:"scenes"`
}

// DefaultConfig returns a Config with sensible defaults and no predefined scenes.
func DefaultConfig() Config {
	return Config{
		DefaultScene: "",
		Scenes:       nil,
	}
}

// SceneMiddleware injects scene context into the agent run.
// It implements the middleware.Middleware interface.
type SceneMiddleware struct {
	middleware.MiddlewareWrapper
	cfg Config
}

// New creates a new SceneMiddleware with the given configuration.
func New(cfg Config) *SceneMiddleware {
	return &SceneMiddleware{cfg: cfg}
}

// Name implements middleware.Middleware.
func (m *SceneMiddleware) Name() string { return "SceneMiddleware" }

// BeforeModel injects the scene context prompt suffix and safety profile into State.
//
// The scene is resolved in this order:
//  1. State.Extra["scene"] (set per-run from RunConfig.Scene)
//  2. cfg.DefaultScene
//
// If no scene is configured, this is a no-op.
func (m *SceneMiddleware) BeforeModel(_ context.Context, state *middleware.State) error {
	if state == nil {
		return nil
	}

	sceneID := m.resolveScene(state)
	if sceneID == "" {
		return nil
	}

	sceneCfg, ok := m.cfg.Scenes[sceneID]
	if !ok {
		logging.Debug("scene: unknown scene ID, skipping", "scene", sceneID)
		return nil
	}

	// Ensure Extra map exists.
	if state.Extra == nil {
		state.Extra = make(map[string]any)
	}

	// Store resolved scene and safety profile in Extra for downstream middlewares.
	state.Extra["scene"] = sceneID
	if sceneCfg.SafetyProfile != "" {
		state.Extra["scene_safety_profile"] = sceneCfg.SafetyProfile
	}

	// Inject scene-specific prompt suffix into the system message.
	if sceneCfg.PromptSuffix != "" {
		m.injectScenePrompt(state, sceneCfg)
	}

	logging.Debug("scene: applied scene context", "scene", sceneID, "safety", sceneCfg.SafetyProfile)
	return nil
}

// resolveScene determines the active scene ID for this run.
func (m *SceneMiddleware) resolveScene(state *middleware.State) string {
	if state.Extra != nil {
		if s, ok := state.Extra["scene"].(string); ok && s != "" {
			return s
		}
	}
	return m.cfg.DefaultScene
}

// injectScenePrompt prepends a <scene_context> block to the system message.
func (m *SceneMiddleware) injectScenePrompt(state *middleware.State, sceneCfg SceneConfig) {
	sceneBlock := fmt.Sprintf("<scene_context>\n%s\n</scene_context>", sceneCfg.PromptSuffix)

	for i, msg := range state.Messages {
		role, _ := msg["role"].(string)
		if role == "system" {
			content, _ := msg["content"].(string)
			// Guard against compounding: skip if scene block already present.
			if strings.HasPrefix(content, sceneBlock) {
				return
			}
			msg["content"] = sceneBlock + "\n\n" + content
			state.Messages[i] = msg
			return
		}
	}

	// No system message found; create one.
	state.Messages = append([]map[string]any{
		{"role": "system", "content": sceneBlock},
	}, state.Messages...)
}

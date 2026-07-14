// Package control implements control-flow middleware for GoClaw.
//
// This file provides a file-based GuardrailProvider that loads tool authorization
// rules from YAML or JSON files, supporting scene-specific filtering.
package control

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"goclaw/internal/logging"
	"gopkg.in/yaml.v3"
)

// FileGuardrailRule defines a single rule in the guardrail rules file.
type FileGuardrailRule struct {
	// Pattern is a glob pattern for matching tool names (e.g., "bash", "web_*", "*").
	Pattern string `yaml:"pattern" json:"pattern"`

	// Action is the authorization decision: "allow", "deny", or "ask".
	Action string `yaml:"action" json:"action"`

	// Reason is a human-readable explanation shown when the rule blocks a tool.
	Reason string `yaml:"reason" json:"reason"`

	// Scenes restricts this rule to specific scene IDs. Empty means all scenes.
	Scenes []string `yaml:"scenes,omitempty" json:"scenes,omitempty"`
}

// FileGuardrailConfig is the top-level structure of a guardrail rules file.
type FileGuardrailConfig struct {
	// DefaultAction is the action when no rule matches: "allow" or "deny".
	DefaultAction string `yaml:"default_action" json:"default_action"`

	// Rules is the ordered list of rules. First match wins.
	Rules []FileGuardrailRule `yaml:"rules" json:"rules"`
}

// FileBasedProvider implements GuardrailProvider by loading rules from a YAML or JSON file.
// It supports scene-specific rule filtering. Rules are loaded once at construction;
// call NewFileBasedProvider again to reload after file changes.
type FileBasedProvider struct {
	mu       sync.RWMutex
	filePath string
	config   FileGuardrailConfig
}

// NewFileBasedProvider creates a provider that loads rules from the given file.
// The file must be YAML (.yaml/.yml) or JSON (.json). Returns an error if the file
// cannot be read or parsed.
func NewFileBasedProvider(filePath string) (*FileBasedProvider, error) {
	p := &FileBasedProvider{filePath: filePath}
	if err := p.reload(); err != nil {
		return nil, fmt.Errorf("file_provider: load %s: %w", filePath, err)
	}
	logging.Info("guardrail: loaded rules from file", "path", filePath, "rules", len(p.config.Rules))
	return p, nil
}

// Name implements GuardrailProvider.
func (p *FileBasedProvider) Name() string { return "FileBasedProvider" }

// Evaluate implements GuardrailProvider.
//
// Rule matching order:
//  1. Check each rule in order. First pattern match wins.
//  2. If the rule has Scenes, only apply when request.Scene matches one of them.
//  3. If no rule matches, use DefaultAction.
func (p *FileBasedProvider) Evaluate(_ context.Context, request GuardrailRequest) (GuardrailDecision, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, rule := range p.config.Rules {
		if !matchPattern(rule.Pattern, request.ToolName) {
			continue
		}

		// Scene filtering: if rule specifies scenes, only match when request scene is in the list.
		if len(rule.Scenes) > 0 && request.Scene != "" {
			sceneMatch := false
			for _, s := range rule.Scenes {
				if strings.EqualFold(s, request.Scene) {
					sceneMatch = true
					break
				}
			}
			if !sceneMatch {
				continue // rule doesn't apply to this scene; try next rule
			}
		}

		// Rule matched. Apply action.
		return p.applyRule(rule), nil
	}

	// No rule matched. Use default action.
	if strings.EqualFold(p.config.DefaultAction, "deny") {
		return DecisionDenied(ReasonToolNotAllowed, "blocked by default guardrail policy (file-based)"), nil
	}
	return DecisionAllowed(), nil
}

// applyRule converts a rule action to a GuardrailDecision.
func (p *FileBasedProvider) applyRule(rule FileGuardrailRule) GuardrailDecision {
	switch strings.ToLower(rule.Action) {
	case "deny", "block":
		reason := rule.Reason
		if reason == "" {
			reason = fmt.Sprintf("tool '%s' blocked by rule", rule.Pattern)
		}
		return DecisionDenied(ReasonToolNotAllowed, reason)
	case "ask":
		// "ask" maps to denying with a specific reason that the agent can handle.
		// The ClarificationMiddleware already handles ask_clarification intercepts.
		return DecisionDenied("oap.requires_approval", "tool requires human approval")
	default:
		return DecisionAllowed()
	}
}

// reload reads and parses the rules file.
func (p *FileBasedProvider) reload() error {
	data, err := os.ReadFile(p.filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	var config FileGuardrailConfig
	content := strings.TrimSpace(string(data))

	// Auto-detect format: JSON starts with '{', YAML otherwise.
	if strings.HasPrefix(content, "{") {
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("parse JSON: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("parse YAML: %w", err)
		}
	}

	// Validate rules.
	for i, rule := range config.Rules {
		if strings.TrimSpace(rule.Pattern) == "" {
			return fmt.Errorf("rule %d: pattern is required", i)
		}
		action := strings.ToLower(rule.Action)
		if action != "allow" && action != "deny" && action != "block" && action != "ask" {
			return fmt.Errorf("rule %d: unknown action '%s' (must be allow, deny, or ask)", i, rule.Action)
		}
	}

	p.config = config
	return nil
}

var _ GuardrailProvider = (*FileBasedProvider)(nil)

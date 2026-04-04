// Package tools defines the core Tool interface and ToolRegistry used by the
// GoClaw agent harness. All built-in and community tools must implement the
// Tool interface so they can be registered, discovered, and invoked uniformly.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Tool is the primary interface that every GoClaw tool must implement.
// It mirrors the key surface of the Eino InvokableTool interface while
// keeping the internal layer independent of the Eino import graph.
//
// Name returns a stable, unique snake_case identifier used by the model to
// reference the tool in its tool-call JSON (e.g. "read_file", "bash").
//
// Description returns a human-readable explanation of what the tool does and
// when the model should use it. Rich few-shot examples in the description
// significantly improve model accuracy.
//
// InputSchema returns a JSON Schema (as a raw JSON byte slice) that describes
// the tool's input parameters. The schema is forwarded to the LLM so it can
// generate well-typed tool calls. Return nil when the tool accepts no input.
//
// Execute receives the model's tool call arguments as a JSON-encoded string
// and returns a plain text result that is sent back to the model as a tool
// message. Implementations must handle ctx cancellation and respect deadlines.
type Tool interface {
	Name() string
	Description() string
	InputSchema() json.RawMessage
	Execute(ctx context.Context, input string) (string, error)
}

// ToolRegistry is a thread-safe registry that maps tool names to Tool
// implementations. A single global registry is used throughout the process;
// callers may also create isolated registries for testing.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry creates an empty ToolRegistry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register adds t to the registry. If a tool with the same name is already
// registered, Register returns an error rather than silently overwriting it,
// preventing accidental shadowing of built-in tools.
func (r *ToolRegistry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := t.Name()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tools: tool %q is already registered", name)
	}
	r.tools[name] = t
	return nil
}

// MustRegister is like Register but panics on error. Intended for use during
// package-level initialization where failure is a programming error.
func (r *ToolRegistry) MustRegister(t Tool) {
	if err := r.Register(t); err != nil {
		panic(err)
	}
}

// Get looks up a tool by name. It returns (tool, true) when found and
// (nil, false) when the name is unknown.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.tools[name]
	return t, ok
}

// GetAll returns a snapshot of all registered tools in an unspecified order.
// The returned slice is safe to iterate without holding the registry lock.
func (r *ToolRegistry) GetAll() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// ---------------------------------------------------------------------------
// Package-level default registry helpers
// ---------------------------------------------------------------------------

// defaultRegistry is the singleton registry used by the package-level helpers.
var defaultRegistry = NewToolRegistry()

// Register adds t to the default package-level registry.
func Register(t Tool) error {
	return defaultRegistry.Register(t)
}

// MustRegister adds t to the default registry and panics on duplicate.
func MustRegister(t Tool) {
	defaultRegistry.MustRegister(t)
}

// Get looks up a tool by name in the default registry.
func Get(name string) (Tool, bool) {
	return defaultRegistry.Get(name)
}

// GetAll returns all tools registered in the default registry.
func GetAll() []Tool {
	return defaultRegistry.GetAll()
}

// ResetDefaultRegistry clears and recreates the default registry.
// Useful when rebuilding runtime tool sets from config.
func ResetDefaultRegistry() {
	defaultRegistry = NewToolRegistry()
}

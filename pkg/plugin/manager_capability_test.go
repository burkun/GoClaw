package plugin

import (
	"context"
	"encoding/json"
	"testing"

	"goclaw/internal/middleware"
	"goclaw/internal/tools"
)

// mockTool implements tools.Tool for testing
type mockTool struct {
	name        string
	description string
	schema      json.RawMessage
}

func (t *mockTool) Name() string {
	return t.name
}

func (t *mockTool) Description() string {
	return t.description
}

func (t *mockTool) InputSchema() json.RawMessage {
	return t.schema
}

func (t *mockTool) Execute(ctx context.Context, input string) (string, error) {
	return "mock result", nil
}

// mockMiddleware implements middleware.Middleware for testing
type mockMiddleware struct {
	name string
}

func (m *mockMiddleware) BeforeAgent(ctx context.Context, state *middleware.State) error {
	return nil
}

func (m *mockMiddleware) BeforeModel(ctx context.Context, state *middleware.State) error {
	return nil
}

func (m *mockMiddleware) AfterModel(ctx context.Context, state *middleware.State, response *middleware.Response) error {
	return nil
}

func (m *mockMiddleware) AfterAgent(ctx context.Context, state *middleware.State, response *middleware.Response) error {
	return nil
}

func (m *mockMiddleware) WrapToolCall(ctx context.Context, state *middleware.State, toolCall *middleware.ToolCall, handler middleware.ToolHandler) (*middleware.ToolResult, error) {
	return nil, nil
}

func (m *mockMiddleware) Name() string {
	return m.name
}

// pluginWithCapabilities is a plugin that provides tools and middlewares
type pluginWithCapabilities struct {
	*mockPlugin
	tools       []tools.Tool
	middlewares []middleware.Middleware
}

func newPluginWithCapabilities(name string, toolCount, middlewareCount int) *pluginWithCapabilities {
	p := &pluginWithCapabilities{
		mockPlugin:  newMockPlugin(name),
		tools:       make([]tools.Tool, toolCount),
		middlewares: make([]middleware.Middleware, middlewareCount),
	}

	for i := 0; i < toolCount; i++ {
		p.tools[i] = &mockTool{
			name:        name + "-tool-" + string(rune('0'+i)),
			description: "Tool " + string(rune('0'+i)) + " from " + name,
		}
	}

	for i := 0; i < middlewareCount; i++ {
		p.middlewares[i] = &mockMiddleware{
			name: name + "-middleware-" + string(rune('0'+i)),
		}
	}

	return p
}

func (p *pluginWithCapabilities) RegisterTools() []tools.Tool {
	return p.tools
}

func (p *pluginWithCapabilities) RegisterMiddlewares() []middleware.Middleware {
	return p.middlewares
}

// TestGetAllToolsCorrectness tests GetAllTools returns correct tools
func TestGetAllToolsCorrectness(t *testing.T) {
	mgr := NewManager()

	// Register plugins with tools
	p1 := newPluginWithCapabilities("plugin-1", 2, 0)
	p2 := newPluginWithCapabilities("plugin-2", 3, 0)
	p3 := newPluginWithCapabilities("plugin-3", 0, 0) // no tools

	mgr.Register(p1, nil)
	mgr.Register(p2, nil)
	mgr.Register(p3, nil)

	tools := mgr.GetAllTools()

	// Should have 2 + 3 = 5 tools
	if len(tools) != 5 {
		t.Errorf("Expected 5 tools, got %d", len(tools))
	}

	// Verify tool names
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name()] = true
	}

	// Check expected tools
	expectedTools := []string{
		"plugin-1-tool-0", "plugin-1-tool-1",
		"plugin-2-tool-0", "plugin-2-tool-1", "plugin-2-tool-2",
	}

	for _, name := range expectedTools {
		if !toolNames[name] {
			t.Errorf("Expected tool %s not found", name)
		}
	}
}

// TestGetAllMiddlewaresCorrectness tests GetAllMiddlewares returns correct middlewares
func TestGetAllMiddlewaresCorrectness(t *testing.T) {
	mgr := NewManager()

	// Register plugins with middlewares
	p1 := newPluginWithCapabilities("plugin-1", 0, 2)
	p2 := newPluginWithCapabilities("plugin-2", 0, 3)
	p3 := newPluginWithCapabilities("plugin-3", 0, 0) // no middlewares

	mgr.Register(p1, nil)
	mgr.Register(p2, nil)
	mgr.Register(p3, nil)

	middlewares := mgr.GetAllMiddlewares()

	// Should have 2 + 3 = 5 middlewares
	if len(middlewares) != 5 {
		t.Errorf("Expected 5 middlewares, got %d", len(middlewares))
	}

	// Verify middleware names
	middlewareNames := make(map[string]bool)
	for _, m := range middlewares {
		middlewareNames[m.Name()] = true
	}

	// Check expected middlewares
	expectedMiddlewares := []string{
		"plugin-1-middleware-0", "plugin-1-middleware-1",
		"plugin-2-middleware-0", "plugin-2-middleware-1", "plugin-2-middleware-2",
	}

	for _, name := range expectedMiddlewares {
		if !middlewareNames[name] {
			t.Errorf("Expected middleware %s not found", name)
		}
	}
}

// TestGetAllToolsEmptyPluginList tests GetAllTools with no plugins
func TestGetAllToolsEmptyPluginList(t *testing.T) {
	mgr := NewManager()

	tools := mgr.GetAllTools()
	if tools != nil {
		t.Errorf("Expected nil tools for empty manager, got %v", tools)
	}
}

// TestGetAllMiddlewaresEmptyPluginList tests GetAllMiddlewares with no plugins
func TestGetAllMiddlewaresEmptyPluginList(t *testing.T) {
	mgr := NewManager()

	middlewares := mgr.GetAllMiddlewares()
	if middlewares != nil {
		t.Errorf("Expected nil middlewares for empty manager, got %v", middlewares)
	}
}

// TestGetAllToolsMixedPlugins tests with mix of plugins with/without tools
func TestGetAllToolsMixedPlugins(t *testing.T) {
	mgr := NewManager()

	// Plugin with tools
	p1 := newPluginWithCapabilities("plugin-1", 2, 0)
	// Plugin without tools (base plugin)
	p2 := newMockPlugin("plugin-2")
	// Plugin with tools
	p3 := newPluginWithCapabilities("plugin-3", 1, 0)

	mgr.Register(p1, nil)
	mgr.Register(p2, nil)
	mgr.Register(p3, nil)

	tools := mgr.GetAllTools()

	// Should have 2 + 1 = 3 tools
	if len(tools) != 3 {
		t.Errorf("Expected 3 tools, got %d", len(tools))
	}
}

// TestGetAllMiddlewaresMixedPlugins tests with mix of plugins with/without middlewares
func TestGetAllMiddlewaresMixedPlugins(t *testing.T) {
	mgr := NewManager()

	// Plugin with middlewares
	p1 := newPluginWithCapabilities("plugin-1", 0, 2)
	// Plugin without middlewares (base plugin)
	p2 := newMockPlugin("plugin-2")
	// Plugin with middlewares
	p3 := newPluginWithCapabilities("plugin-3", 0, 1)

	mgr.Register(p1, nil)
	mgr.Register(p2, nil)
	mgr.Register(p3, nil)

	middlewares := mgr.GetAllMiddlewares()

	// Should have 2 + 1 = 3 middlewares
	if len(middlewares) != 3 {
		t.Errorf("Expected 3 middlewares, got %d", len(middlewares))
	}
}

// TestCapabilitiesAfterRegistration tests tools/middlewares available after registration
func TestCapabilitiesAfterRegistration(t *testing.T) {
	mgr := NewManager()

	p := newPluginWithCapabilities("test", 1, 1)

	// Register plugin
	mgr.Register(p, nil)

	// Capabilities should be available immediately
	tools := mgr.GetAllTools()
	if len(tools) != 1 {
		t.Errorf("Expected 1 tool after registration, got %d", len(tools))
	}

	middlewares := mgr.GetAllMiddlewares()
	if len(middlewares) != 1 {
		t.Errorf("Expected 1 middleware after registration, got %d", len(middlewares))
	}
}

// TestCapabilitiesAfterUnregister tests tools/middlewares removed after unregister
func TestCapabilitiesAfterUnregister(t *testing.T) {
	mgr := NewManager()

	p := newPluginWithCapabilities("test", 1, 1)
	mgr.Register(p, nil)

	// Verify capabilities exist
	if len(mgr.GetAllTools()) != 1 {
		t.Fatal("Expected 1 tool before unregister")
	}

	// Unregister
	mgr.Unregister("test")

	// Capabilities should be removed
	tools := mgr.GetAllTools()
	if len(tools) != 0 {
		t.Errorf("Expected 0 tools after unregister, got %d", len(tools))
	}

	middlewares := mgr.GetAllMiddlewares()
	if len(middlewares) != 0 {
		t.Errorf("Expected 0 middlewares after unregister, got %d", len(middlewares))
	}
}

// TestPluginWithLargeNumberOfTools tests plugin with many tools
func TestPluginWithLargeNumberOfTools(t *testing.T) {
	mgr := NewManager()

	// Plugin with 100 tools
	p := newPluginWithCapabilities("large-plugin", 100, 0)
	mgr.Register(p, nil)

	tools := mgr.GetAllTools()
	if len(tools) != 100 {
		t.Errorf("Expected 100 tools, got %d", len(tools))
	}
}

// TestMultiplePluginsWithCapabilities tests collecting from multiple plugins
func TestMultiplePluginsWithCapabilities(t *testing.T) {
	mgr := NewManager()

	// Register 10 plugins, each with 5 tools and 3 middlewares
	for i := 0; i < 10; i++ {
		name := string(rune('a' + i))
		p := newPluginWithCapabilities(name, 5, 3)
		mgr.Register(p, nil)
	}

	tools := mgr.GetAllTools()
	if len(tools) != 50 {
		t.Errorf("Expected 50 tools (10 plugins * 5), got %d", len(tools))
	}

	middlewares := mgr.GetAllMiddlewares()
	if len(middlewares) != 30 {
		t.Errorf("Expected 30 middlewares (10 plugins * 3), got %d", len(middlewares))
	}
}

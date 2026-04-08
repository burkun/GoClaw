package plugin

import (
	"context"
	"testing"
)

// mockPlugin is a simple test plugin implementation
type mockPlugin struct {
	*BasePlugin
	initCalled      bool
	startCalled     bool
	stopCalled      bool
	stopCallCount   int
	initConfig      map[string]any
	toolCount       int
	middlewareCount int
	initFunc        func()
	startFunc       func()
	stopFunc        func()
}

func newMockPlugin(name string) *mockPlugin {
	return &mockPlugin{
		BasePlugin: NewBasePlugin(name, "1.0.0", "Mock plugin for testing"),
	}
}

func (p *mockPlugin) Init(ctx context.Context, config map[string]any) error {
	p.initCalled = true
	p.initConfig = config
	if p.initFunc != nil {
		p.initFunc()
	}
	return p.BasePlugin.Init(ctx, config)
}

func (p *mockPlugin) Start(ctx context.Context) error {
	p.startCalled = true
	if p.startFunc != nil {
		p.startFunc()
	}
	return p.BasePlugin.Start(ctx)
}

func (p *mockPlugin) Stop(ctx context.Context) error {
	p.stopCalled = true
	p.stopCallCount++
	if p.stopFunc != nil {
		p.stopFunc()
	}
	return p.BasePlugin.Stop(ctx)
}

func TestManager_Register(t *testing.T) {
	mgr := NewManager()
	p := newMockPlugin("test-plugin")

	// Test successful registration
	err := mgr.Register(p, nil)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Test duplicate registration
	err = mgr.Register(p, nil)
	if err == nil {
		t.Fatal("Expected error for duplicate registration")
	}
}

func TestManager_Get(t *testing.T) {
	mgr := NewManager()
	p := newMockPlugin("test-plugin")
	mgr.Register(p, nil)

	// Test get existing plugin
	retrieved := mgr.Get("test-plugin")
	if retrieved == nil {
		t.Fatal("Expected to retrieve plugin")
	}

	// Test get non-existent plugin
	retrieved = mgr.Get("non-existent")
	if retrieved != nil {
		t.Fatal("Expected nil for non-existent plugin")
	}
}

func TestManager_List(t *testing.T) {
	mgr := NewManager()

	p1 := newMockPlugin("plugin-1")
	p2 := newMockPlugin("plugin-2")
	mgr.Register(p1, nil)
	mgr.Register(p2, nil)

	list := mgr.List()
	if len(list) != 2 {
		t.Fatalf("Expected 2 plugins, got %d", len(list))
	}
}

func TestManager_Lifecycle(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager()

	p1 := newMockPlugin("plugin-1")
	p2 := newMockPlugin("plugin-2")

	config := map[string]any{"key": "value"}

	mgr.Register(p1, config)
	mgr.Register(p2, nil)

	// Test InitAll
	err := mgr.InitAll(ctx)
	if err != nil {
		t.Fatalf("InitAll failed: %v", err)
	}

	if !p1.initCalled {
		t.Error("Expected Init to be called on plugin-1")
	}
	if p1.initConfig["key"] != "value" {
		t.Error("Expected config to be passed to Init")
	}
	if !p2.initCalled {
		t.Error("Expected Init to be called on plugin-2")
	}

	// Test StartAll
	err = mgr.StartAll(ctx)
	if err != nil {
		t.Fatalf("StartAll failed: %v", err)
	}

	if !p1.startCalled {
		t.Error("Expected Start to be called on plugin-1")
	}
	if !p2.startCalled {
		t.Error("Expected Start to be called on plugin-2")
	}

	// Test double start
	err = mgr.StartAll(ctx)
	if err == nil {
		t.Fatal("Expected error for double start")
	}

	// Test StopAll
	err = mgr.StopAll(ctx)
	if err != nil {
		t.Fatalf("StopAll failed: %v", err)
	}

	if !p1.stopCalled {
		t.Error("Expected Stop to be called on plugin-1")
	}
	if !p2.stopCalled {
		t.Error("Expected Stop to be called on plugin-2")
	}
}

func TestManager_EnableDisable(t *testing.T) {
	mgr := NewManager()
	p := newMockPlugin("test-plugin")
	mgr.Register(p, nil)

	// Test Enable
	err := mgr.Enable("test-plugin")
	if err != nil {
		t.Fatalf("Enable failed: %v", err)
	}

	if !mgr.IsEnabled("test-plugin") {
		t.Error("Expected plugin to be enabled")
	}

	// Test Disable (should fail while running)
	ctx := context.Background()
	mgr.InitAll(ctx)
	mgr.StartAll(ctx)

	err = mgr.Disable("test-plugin")
	if err == nil {
		t.Fatal("Expected error when disabling running plugin")
	}

	// Disable after stop
	mgr.StopAll(ctx)
	err = mgr.Disable("test-plugin")
	if err != nil {
		t.Fatalf("Disable failed: %v", err)
	}

	if mgr.IsEnabled("test-plugin") {
		t.Error("Expected plugin to be disabled")
	}

	// Test non-existent plugin
	err = mgr.Enable("non-existent")
	if err == nil {
		t.Fatal("Expected error for non-existent plugin")
	}
}

func TestManager_Unregister(t *testing.T) {
	mgr := NewManager()
	p := newMockPlugin("test-plugin")
	mgr.Register(p, nil)

	// Test unregister while running (should fail)
	ctx := context.Background()
	mgr.InitAll(ctx)
	mgr.StartAll(ctx)

	err := mgr.Unregister("test-plugin")
	if err == nil {
		t.Fatal("Expected error when unregistering running plugin")
	}

	// Test unregister after stop
	mgr.StopAll(ctx)
	err = mgr.Unregister("test-plugin")
	if err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}

	// Verify plugin is removed
	if mgr.Get("test-plugin") != nil {
		t.Fatal("Expected plugin to be removed")
	}

	// Test unregister non-existent
	err = mgr.Unregister("non-existent")
	if err == nil {
		t.Fatal("Expected error for non-existent plugin")
	}
}

func TestBasePlugin(t *testing.T) {
	ctx := context.Background()
	bp := NewBasePlugin("test", "2.0.0", "Test plugin")

	// Test metadata
	if bp.Name() != "test" {
		t.Errorf("Expected name 'test', got %s", bp.Name())
	}
	if bp.Version() != "2.0.0" {
		t.Errorf("Expected version '2.0.0', got %s", bp.Version())
	}
	if bp.Description() != "Test plugin" {
		t.Errorf("Expected description 'Test plugin', got %s", bp.Description())
	}

	// Test default implementations (should not error)
	if err := bp.Init(ctx, nil); err != nil {
		t.Errorf("Init failed: %v", err)
	}
	if err := bp.Start(ctx); err != nil {
		t.Errorf("Start failed: %v", err)
	}
	if err := bp.Stop(ctx); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
	if tools := bp.RegisterTools(); tools != nil {
		t.Errorf("Expected nil tools, got %v", tools)
	}
	if middlewares := bp.RegisterMiddlewares(); middlewares != nil {
		t.Errorf("Expected nil middlewares, got %v", middlewares)
	}
}

func TestDefaultManager(t *testing.T) {
	ResetDefaultManager()

	p := newMockPlugin("default-test")
	err := Register(p, map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	retrieved := Get("default-test")
	if retrieved == nil {
		t.Fatal("Expected to retrieve plugin")
	}

	list := List()
	if len(list) != 1 {
		t.Fatalf("Expected 1 plugin, got %d", len(list))
	}

	ctx := context.Background()
	err = InitAll(ctx)
	if err != nil {
		t.Fatalf("InitAll failed: %v", err)
	}

	err = StartAll(ctx)
	if err != nil {
		t.Fatalf("StartAll failed: %v", err)
	}

	err = StopAll(ctx)
	if err != nil {
		t.Fatalf("StopAll failed: %v", err)
	}

	ResetDefaultManager()
}

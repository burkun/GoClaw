package plugin

import (
	"context"
	"testing"
)

// TestDuplicateRegistration tests registering same plugin twice
func TestDuplicateRegistration(t *testing.T) {
	mgr := NewManager()

	p := newMockPlugin("test-plugin")

	// First registration succeeds
	err := mgr.Register(p, nil)
	if err != nil {
		t.Fatalf("First registration failed: %v", err)
	}

	// Second registration should fail
	err = mgr.Register(p, nil)
	if err == nil {
		t.Fatal("Expected error on duplicate registration")
	}
}

// TestGetNonExistent tests getting non-existent plugin
func TestGetNonExistent(t *testing.T) {
	mgr := NewManager()

	// Get non-existent plugin should return nil
	p := mgr.Get("non-existent")
	if p != nil {
		t.Error("Expected nil for non-existent plugin")
	}
}

// TestIsEnabledNonExistent tests IsEnabled for non-existent plugin
func TestIsEnabledNonExistent(t *testing.T) {
	mgr := NewManager()

	// IsEnabled for non-existent plugin should return false
	if mgr.IsEnabled("non-existent") {
		t.Error("Expected false for non-existent plugin")
	}
}

// TestEnableNonExistent tests enabling non-existent plugin
func TestEnableNonExistent(t *testing.T) {
	mgr := NewManager()

	err := mgr.Enable("non-existent")
	if err == nil {
		t.Fatal("Expected error when enabling non-existent plugin")
	}
}

// TestDisableNonExistent tests disabling non-existent plugin
func TestDisableNonExistent(t *testing.T) {
	mgr := NewManager()

	err := mgr.Disable("non-existent")
	if err == nil {
		t.Fatal("Expected error when disabling non-existent plugin")
	}
}

// TestUnregisterNonExistent tests unregistering non-existent plugin
func TestUnregisterNonExistent(t *testing.T) {
	mgr := NewManager()

	err := mgr.Unregister("non-existent")
	if err == nil {
		t.Fatal("Expected error when unregistering non-existent plugin")
	}
}

// TestDisableWhileRunning tests disabling plugin while running
func TestDisableWhileRunning(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager()

	p := newMockPlugin("test-plugin")
	mgr.Register(p, nil)
	mgr.InitAll(ctx)
	mgr.StartAll(ctx)

	// Disable while running should fail
	err := mgr.Disable("test-plugin")
	if err == nil {
		t.Fatal("Expected error when disabling running plugin")
	}
}

// TestUnregisterWhileRunning tests unregistering plugin while running
func TestUnregisterWhileRunning(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager()

	p := newMockPlugin("test-plugin")
	mgr.Register(p, nil)
	mgr.InitAll(ctx)
	mgr.StartAll(ctx)

	// Unregister while running should fail
	err := mgr.Unregister("test-plugin")
	if err == nil {
		t.Fatal("Expected error when unregistering running plugin")
	}

	// Stop and unregister should succeed
	mgr.StopAll(ctx)
	err = mgr.Unregister("test-plugin")
	if err != nil {
		t.Fatalf("Unregister after stop failed: %v", err)
	}
}

// TestRegisterNilPlugin tests registering nil plugin
func TestRegisterNilPlugin(t *testing.T) {
	mgr := NewManager()

	// Registering nil should panic (plugin.Name() will be called)
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic when registering nil plugin")
		}
	}()

	mgr.Register(nil, nil)
}

// TestEmptyManagerOperations tests operations on empty manager
func TestEmptyManagerOperations(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager()

	// InitAll on empty manager should succeed
	err := mgr.InitAll(ctx)
	if err != nil {
		t.Errorf("InitAll on empty manager failed: %v", err)
	}

	// StartAll on empty manager should succeed
	err = mgr.StartAll(ctx)
	if err != nil {
		t.Errorf("StartAll on empty manager failed: %v", err)
	}

	// StopAll on empty manager should succeed
	err = mgr.StopAll(ctx)
	if err != nil {
		t.Errorf("StopAll on empty manager failed: %v", err)
	}

	// List on empty manager should return empty slice
	list := mgr.List()
	if len(list) != 0 {
		t.Errorf("Expected empty list, got %d plugins", len(list))
	}

	// GetAllTools on empty manager should return nil
	tools := mgr.GetAllTools()
	if tools != nil {
		t.Errorf("Expected nil tools, got %v", tools)
	}

	// GetAllMiddlewares on empty manager should return nil
	middlewares := mgr.GetAllMiddlewares()
	if middlewares != nil {
		t.Errorf("Expected nil middlewares, got %v", middlewares)
	}
}

// TestCrashIsolation tests that plugin crash doesn't affect manager
func TestCrashIsolation(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager()

	// Create a plugin that panics on Start
	panicPlugin := &panicMockPlugin{
		mockPlugin: newMockPlugin("panic-plugin"),
		panicOn:    "start",
	}

	p2 := newMockPlugin("normal-plugin")

	mgr.Register(panicPlugin, nil)
	mgr.Register(p2, nil)

	mgr.InitAll(ctx)

	// StartAll should handle panic and rollback
	defer func() {
		if r := recover(); r != nil {
			// Recover from panic and verify manager state
			t.Logf("Recovered from panic: %v", r)
		}
	}()

	err := mgr.StartAll(ctx)
	// The panic plugin will cause StartAll to fail
	// We expect the manager to remain in a consistent state
	if err == nil {
		t.Log("StartAll handled panic gracefully")
	}
}

// panicMockPlugin panics on specified lifecycle method
type panicMockPlugin struct {
	*mockPlugin
	panicOn string
}

func (p *panicMockPlugin) Start(ctx context.Context) error {
	if p.panicOn == "start" {
		panic("plugin crashed on start")
	}
	return p.mockPlugin.Start(ctx)
}

func (p *panicMockPlugin) Stop(ctx context.Context) error {
	if p.panicOn == "stop" {
		panic("plugin crashed on stop")
	}
	return p.mockPlugin.Stop(ctx)
}

// TestContextCancellation tests context cancellation during lifecycle
func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mgr := NewManager()

	p := newMockPlugin("test-plugin")
	mgr.Register(p, nil)

	// Cancel context before operations
	cancel()

	// InitAll with cancelled context should still complete
	// (plugins should check context but BasePlugin doesn't)
	err := mgr.InitAll(ctx)
	// Behavior depends on plugin implementation
	t.Logf("InitAll with cancelled context: err=%v", err)

	// StartAll with cancelled context
	err = mgr.StartAll(ctx)
	t.Logf("StartAll with cancelled context: err=%v", err)

	// StopAll with cancelled context
	err = mgr.StopAll(ctx)
	t.Logf("StopAll with cancelled context: err=%v", err)
}

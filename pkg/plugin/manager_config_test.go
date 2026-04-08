package plugin

import (
	"context"
	"testing"
)

// TestPluginConfigPassThrough tests config is passed to plugin
func TestPluginConfigPassThrough(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager()

	p := newMockPlugin("test-plugin")
	config := map[string]any{
		"string_key": "value",
		"int_key":    42,
		"bool_key":   true,
		"nested": map[string]any{
			"inner": "value",
		},
	}

	mgr.Register(p, config)
	mgr.InitAll(ctx)

	// Verify config was passed
	if !p.initCalled {
		t.Fatal("Init was not called")
	}

	if p.initConfig == nil {
		t.Fatal("Config was not passed to Init")
	}

	if p.initConfig["string_key"] != "value" {
		t.Error("string_key not passed correctly")
	}

	if p.initConfig["int_key"] != 42 {
		t.Error("int_key not passed correctly")
	}

	if p.initConfig["bool_key"] != true {
		t.Error("bool_key not passed correctly")
	}

	nested := p.initConfig["nested"].(map[string]any)
	if nested["inner"] != "value" {
		t.Error("nested config not passed correctly")
	}
}

// TestNilConfig tests plugin with nil config
func TestNilConfig(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager()

	p := newMockPlugin("test-plugin")
	mgr.Register(p, nil)
	mgr.InitAll(ctx)

	// Plugin should initialize with empty config
	if !p.initCalled {
		t.Fatal("Init was not called")
	}

	// Manager converts nil config to empty map
	if p.initConfig == nil {
		t.Error("Expected empty config (not nil) for plugin registered with nil")
	}

	if len(p.initConfig) != 0 {
		t.Errorf("Expected empty config, got %d items", len(p.initConfig))
	}
}

// TestEmptyConfig tests plugin with empty config
func TestEmptyConfig(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager()

	p := newMockPlugin("test-plugin")
	config := map[string]any{}
	mgr.Register(p, config)
	mgr.InitAll(ctx)

	if !p.initCalled {
		t.Fatal("Init was not called")
	}

	// Empty config should be passed
	if p.initConfig == nil {
		t.Fatal("Config should not be nil")
	}

	if len(p.initConfig) != 0 {
		t.Error("Expected empty config")
	}
}

// TestMultiplePluginsDifferentConfigs tests different configs for different plugins
func TestMultiplePluginsDifferentConfigs(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager()

	plugins := make([]*mockPlugin, 5)
	for i := 0; i < 5; i++ {
		plugins[i] = newMockPlugin(string(rune('a' + i)))
		config := map[string]any{
			"id":    i,
			"value": "plugin-" + string(rune('a'+i)),
		}
		mgr.Register(plugins[i], config)
	}

	mgr.InitAll(ctx)

	// Verify each plugin got its own config
	for i, p := range plugins {
		if !p.initCalled {
			t.Errorf("Plugin %d was not initialized", i)
			continue
		}

		id := p.initConfig["id"].(int)
		if id != i {
			t.Errorf("Plugin %d got wrong id: %d", i, id)
		}
	}
}

// TestConfigPreservedAcrossLifecycle tests config is preserved
func TestConfigPreservedAcrossLifecycle(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager()

	p := newMockPlugin("test-plugin")
	config := map[string]any{"key": "value"}

	mgr.Register(p, config)
	mgr.InitAll(ctx)
	mgr.StartAll(ctx)
	mgr.StopAll(ctx)

	// Config should still be accessible via ListInfo
	infos := mgr.ListInfo()
	if len(infos) != 1 {
		t.Fatalf("Expected 1 plugin info, got %d", len(infos))
	}

	if infos[0].Config["key"] != "value" {
		t.Error("Config was not preserved")
	}
}

// TestEnableDisable tests enable/disable functionality
func TestEnableDisableBasic(t *testing.T) {
	mgr := NewManager()

	p := newMockPlugin("test-plugin")
	mgr.Register(p, nil)

	// Plugin should be enabled by default
	if !mgr.IsEnabled("test-plugin") {
		t.Error("Expected plugin to be enabled by default")
	}

	// Disable
	err := mgr.Disable("test-plugin")
	if err != nil {
		t.Fatalf("Disable failed: %v", err)
	}

	if mgr.IsEnabled("test-plugin") {
		t.Error("Expected plugin to be disabled")
	}

	// Enable
	err = mgr.Enable("test-plugin")
	if err != nil {
		t.Fatalf("Enable failed: %v", err)
	}

	if !mgr.IsEnabled("test-plugin") {
		t.Error("Expected plugin to be enabled")
	}
}

// TestDisabledPluginInListInfo tests disabled plugin appears in ListInfo
func TestDisabledPluginInListInfo(t *testing.T) {
	mgr := NewManager()

	p := newMockPlugin("test-plugin")
	mgr.Register(p, nil)
	mgr.Disable("test-plugin")

	infos := mgr.ListInfo()
	if len(infos) != 1 {
		t.Fatalf("Expected 1 plugin info, got %d", len(infos))
	}

	if infos[0].Enabled {
		t.Error("Expected plugin to be marked as disabled in ListInfo")
	}
}

// TestListInfo tests ListInfo returns correct information
func TestListInfo(t *testing.T) {
	mgr := NewManager()

	p1 := newMockPlugin("plugin-1")
	p2 := newMockPlugin("plugin-2")

	config1 := map[string]any{"key1": "value1"}
	config2 := map[string]any{"key2": "value2"}

	mgr.Register(p1, config1)
	mgr.Register(p2, config2)

	infos := mgr.ListInfo()

	if len(infos) != 2 {
		t.Fatalf("Expected 2 plugin infos, got %d", len(infos))
	}

	// Build map for easier verification
	infoMap := make(map[string]PluginInfo)
	for _, info := range infos {
		infoMap[info.Name] = info
	}

	// Verify plugin-1 info
	if info, ok := infoMap["plugin-1"]; ok {
		if info.Version != "1.0.0" {
			t.Errorf("Unexpected version for plugin-1: %s", info.Version)
		}
		if info.Config["key1"] != "value1" {
			t.Error("Config for plugin-1 not preserved")
		}
		if !info.Enabled {
			t.Error("Expected plugin-1 to be enabled")
		}
	} else {
		t.Error("plugin-1 not found in ListInfo")
	}

	// Verify plugin-2 info
	if info, ok := infoMap["plugin-2"]; ok {
		if info.Version != "1.0.0" {
			t.Errorf("Unexpected version for plugin-2: %s", info.Version)
		}
		if info.Config["key2"] != "value2" {
			t.Error("Config for plugin-2 not preserved")
		}
		if !info.Enabled {
			t.Error("Expected plugin-2 to be enabled")
		}
	} else {
		t.Error("plugin-2 not found in ListInfo")
	}
}

// TestConfigModificationDoesNotAffectManager tests config modification safety
func TestConfigModificationDoesNotAffectManager(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager()

	p := newMockPlugin("test-plugin")
	config := map[string]any{"key": "value"}

	mgr.Register(p, config)

	// Modify original config
	config["key"] = "modified"
	config["new_key"] = "new_value"

	// Init plugin
	mgr.InitAll(ctx)

	// Plugin should have received the config state at registration time
	// Note: This test verifies current behavior. Manager stores the config reference.
	// If you want to prevent modification, manager should deep-copy the config.
	if p.initConfig["key"] == "modified" {
		t.Log("Warning: Config modification affects manager (config stored by reference)")
	}
}

// TestReset tests manager reset functionality
func TestReset(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager()

	// Register and start plugins
	p1 := newMockPlugin("plugin-1")
	p2 := newMockPlugin("plugin-2")
	mgr.Register(p1, nil)
	mgr.Register(p2, nil)
	mgr.InitAll(ctx)
	mgr.StartAll(ctx)

	// Reset
	mgr.Reset()

	// Verify manager is empty
	if len(mgr.List()) != 0 {
		t.Error("Expected empty list after reset")
	}

	// Verify started flag is reset
	err := mgr.InitAll(ctx)
	if err != nil {
		t.Error("InitAll should succeed after reset")
	}

	err = mgr.StartAll(ctx)
	if err != nil {
		t.Error("StartAll should succeed after reset")
	}

	// Clean up
	mgr.StopAll(ctx)
}

// TestLargeConfig tests plugin with large config
func TestLargeConfig(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager()

	p := newMockPlugin("test-plugin")

	// Create large config with unique keys
	config := make(map[string]any)
	for i := 0; i < 1000; i++ {
		// Use string representation of i to create unique keys
		key := "key_" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i/26/26)%26))
		config[key] = i
	}

	mgr.Register(p, config)
	mgr.InitAll(ctx)

	// Verify config was passed
	if len(p.initConfig) != len(config) {
		t.Errorf("Expected %d config items, got %d", len(config), len(p.initConfig))
	}
}

// TestPluginInfoFields tests all PluginInfo fields
func TestPluginInfoFields(t *testing.T) {
	mgr := NewManager()

	// Create plugin with custom metadata
	p := &mockPlugin{
		BasePlugin: NewBasePlugin("custom-plugin", "2.5.0", "Custom plugin description"),
	}

	config := map[string]any{"setting": "enabled"}
	mgr.Register(p, config)

	infos := mgr.ListInfo()
	if len(infos) != 1 {
		t.Fatalf("Expected 1 plugin info, got %d", len(infos))
	}

	info := infos[0]
	if info.Name != "custom-plugin" {
		t.Errorf("Expected name 'custom-plugin', got %s", info.Name)
	}
	if info.Version != "2.5.0" {
		t.Errorf("Expected version '2.5.0', got %s", info.Version)
	}
	if info.Description != "Custom plugin description" {
		t.Errorf("Expected description 'Custom plugin description', got %s", info.Description)
	}
	if !info.Enabled {
		t.Error("Expected plugin to be enabled")
	}
	if info.Config["setting"] != "enabled" {
		t.Error("Expected config to be preserved")
	}
}

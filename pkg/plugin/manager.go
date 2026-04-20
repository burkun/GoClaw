package plugin

import (
	"context"
	"fmt"
	"sync"

	"goclaw/internal/logging"
	"goclaw/internal/middleware"
	"goclaw/internal/tools"
)

// Manager manages plugin lifecycle and registration.
// It provides a thread-safe registry for plugins and coordinates
// initialization, startup, and shutdown.
type Manager struct {
	mu      sync.RWMutex
	plugins map[string]Plugin
	configs map[string]PluginInfo
	started bool
}

// NewManager creates a new plugin manager.
func NewManager() *Manager {
	return &Manager{
		plugins: make(map[string]Plugin),
		configs: make(map[string]PluginInfo),
		started: false,
	}
}

// Register registers a plugin with the manager.
// The plugin will be initialized with the provided config when InitAll is called.
// Returns an error if a plugin with the same name is already registered.
func (m *Manager) Register(p Plugin, config map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := p.Name()
	if _, exists := m.plugins[name]; exists {
		return fmt.Errorf("plugin %q is already registered", name)
	}

	m.plugins[name] = p
	m.configs[name] = PluginInfo{
		Name:        p.Name(),
		Version:     p.Version(),
		Description: p.Description(),
		Enabled:     true,
		Config:      config,
	}

	logging.Debug("Plugin registered", "name", name, "version", p.Version())
	return nil
}

// Unregister removes a plugin from the manager.
// Returns an error if the plugin is not found or is currently running.
func (m *Manager) Unregister(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("cannot unregister plugin %q while manager is running", name)
	}

	if _, exists := m.plugins[name]; !exists {
		return fmt.Errorf("plugin %q not found", name)
	}

	delete(m.plugins, name)
	delete(m.configs, name)

	logging.Debug("Plugin unregistered", "name", name)
	return nil
}

// Get retrieves a plugin by name.
// Returns nil if the plugin is not found.
func (m *Manager) Get(name string) Plugin {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.plugins[name]
}

// List returns all registered plugins.
func (m *Manager) List() []Plugin {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]Plugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		result = append(result, p)
	}
	return result
}

// ListInfo returns information about all registered plugins.
func (m *Manager) ListInfo() []PluginInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]PluginInfo, 0, len(m.configs))
	for _, info := range m.configs {
		result = append(result, info)
	}
	return result
}

// InitAll initializes all registered plugins with their configurations.
// This should be called after all plugins are registered and before StartAll.
func (m *Manager) InitAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("plugins already started")
	}

	for name, p := range m.plugins {
		config := m.configs[name].Config
		if config == nil {
			config = make(map[string]any)
		}

		if err := p.Init(ctx, config); err != nil {
			return fmt.Errorf("failed to initialize plugin %q: %w", name, err)
		}

		logging.Info("Plugin initialized",
			"name", name,
			"version", p.Version(),
			"description", p.Description(),
		)
	}

	return nil
}

// StartAll starts all registered plugins.
// This should be called after InitAll.
func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("plugins already started")
	}

	for name, p := range m.plugins {
		if err := p.Start(ctx); err != nil {
			// Try to stop already started plugins on error
			_ = m.stopAllLocked(ctx)
			return fmt.Errorf("failed to start plugin %q: %w", name, err)
		}

		logging.Info("Plugin started", "name", name)
	}

	m.started = true
	return nil
}

// StopAll stops all registered plugins in reverse order.
// This should be called during graceful shutdown.
func (m *Manager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.stopAllLocked(ctx)
}

// stopAllLocked stops all plugins without acquiring the lock.
// Must be called with m.mu held.
func (m *Manager) stopAllLocked(ctx context.Context) error {
	if !m.started {
		return nil
	}

	var lastErr error
	for name, p := range m.plugins {
		if err := p.Stop(ctx); err != nil {
			lastErr = err
			logging.Error("Failed to stop plugin",
				"name", name,
				"error", err,
			)
		} else {
			logging.Info("Plugin stopped", "name", name)
		}
	}

	m.started = false
	return lastErr
}

// GetAllTools returns all tools from all registered plugins.
func (m *Manager) GetAllTools() []tools.Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var allTools []tools.Tool
	for _, p := range m.plugins {
		allTools = append(allTools, p.RegisterTools()...)
	}
	return allTools
}

// GetAllMiddlewares returns all middlewares from all registered plugins.
func (m *Manager) GetAllMiddlewares() []middleware.Middleware {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var allMiddlewares []middleware.Middleware
	for _, p := range m.plugins {
		allMiddlewares = append(allMiddlewares, p.RegisterMiddlewares()...)
	}
	return allMiddlewares
}

// Enable enables a plugin by name.
// Returns an error if the plugin is not found.
func (m *Manager) Enable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, exists := m.configs[name]
	if !exists {
		return fmt.Errorf("plugin %q not found", name)
	}

	info.Enabled = true
	m.configs[name] = info

	logging.Info("Plugin enabled", "name", name)
	return nil
}

// Disable disables a plugin by name.
// Returns an error if the plugin is not found or is currently running.
func (m *Manager) Disable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("cannot disable plugin %q while manager is running", name)
	}

	info, exists := m.configs[name]
	if !exists {
		return fmt.Errorf("plugin %q not found", name)
	}

	info.Enabled = false
	m.configs[name] = info

	logging.Info("Plugin disabled", "name", name)
	return nil
}

// IsEnabled returns whether a plugin is enabled.
func (m *Manager) IsEnabled(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info, exists := m.configs[name]
	if !exists {
		return false
	}
	return info.Enabled
}

// Reset clears all plugins and resets the manager state.
// This should only be used in tests.
func (m *Manager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.plugins = make(map[string]Plugin)
	m.configs = make(map[string]PluginInfo)
	m.started = false
}

// DefaultManager is the default plugin manager instance.
var defaultManager = NewManager()

// Register registers a plugin with the default manager.
func Register(p Plugin, config map[string]any) error {
	return defaultManager.Register(p, config)
}

// Unregister removes a plugin from the default manager.
func Unregister(name string) error {
	return defaultManager.Unregister(name)
}

// Get retrieves a plugin by name from the default manager.
func Get(name string) Plugin {
	return defaultManager.Get(name)
}

// List returns all registered plugins from the default manager.
func List() []Plugin {
	return defaultManager.List()
}

// InitAll initializes all plugins in the default manager.
func InitAll(ctx context.Context) error {
	return defaultManager.InitAll(ctx)
}

// StartAll starts all plugins in the default manager.
func StartAll(ctx context.Context) error {
	return defaultManager.StartAll(ctx)
}

// StopAll stops all plugins in the default manager.
func StopAll(ctx context.Context) error {
	return defaultManager.StopAll(ctx)
}

// GetAllTools returns all tools from the default manager.
func GetAllTools() []tools.Tool {
	return defaultManager.GetAllTools()
}

// GetAllMiddlewares returns all middlewares from the default manager.
func GetAllMiddlewares() []middleware.Middleware {
	return defaultManager.GetAllMiddlewares()
}

// Enable enables a plugin in the default manager.
func Enable(name string) error {
	return defaultManager.Enable(name)
}

// Disable disables a plugin in the default manager.
func Disable(name string) error {
	return defaultManager.Disable(name)
}

// IsEnabled returns whether a plugin is enabled in the default manager.
func IsEnabled(name string) bool {
	return defaultManager.IsEnabled(name)
}

// ResetDefaultManager clears the default manager.
func ResetDefaultManager() {
	defaultManager.Reset()
}

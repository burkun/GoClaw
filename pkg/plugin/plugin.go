// Package plugin defines the plugin system for GoClaw.
// It supports a configuration-based plugin architecture where tools and
// middlewares can be registered as plugins.
package plugin

import (
	"context"

	"goclaw/internal/middleware"
	"goclaw/internal/tools"
)

// Plugin is the interface that every GoClaw plugin must implement.
// Plugins can extend GoClaw's functionality by providing custom tools,
// middlewares, and lifecycle hooks.
//
// The plugin lifecycle:
//  1. Init - Called once when the plugin is loaded, receives config
//  2. Start - Called when the application starts
//  3. RegisterTools/RegisterMiddlewares - Called to collect capabilities
//  4. Stop - Called when the application shuts down
type Plugin interface {
	// Basic information
	Name() string
	Version() string
	Description() string

	// Lifecycle hooks
	// Init initializes the plugin with the provided configuration.
	// The config map contains plugin-specific settings from config.yaml.
	Init(ctx context.Context, config map[string]any) error

	// Start starts the plugin after initialization.
	// This is called after all plugins have been initialized.
	Start(ctx context.Context) error

	// Stop stops the plugin and releases resources.
	// This is called during graceful shutdown.
	Stop(ctx context.Context) error

	// Capability registration
	// RegisterTools returns the tools provided by this plugin.
	// Return nil or empty slice if the plugin doesn't provide tools.
	RegisterTools() []tools.Tool

	// RegisterMiddlewares returns the middlewares provided by this plugin.
	// Return nil or empty slice if the plugin doesn't provide middlewares.
	RegisterMiddlewares() []middleware.Middleware
}

// BasePlugin provides a default implementation of the Plugin interface.
// Embed this in your plugin struct to avoid implementing all methods.
type BasePlugin struct {
	name        string
	version     string
	description string
}

// NewBasePlugin creates a new BasePlugin with the given metadata.
func NewBasePlugin(name, version, description string) *BasePlugin {
	return &BasePlugin{
		name:        name,
		version:     version,
		description: description,
	}
}

// Name returns the plugin name.
func (p *BasePlugin) Name() string {
	return p.name
}

// Version returns the plugin version.
func (p *BasePlugin) Version() string {
	return p.version
}

// Description returns the plugin description.
func (p *BasePlugin) Description() string {
	return p.description
}

// Init does nothing and returns nil. Override in your plugin if needed.
func (p *BasePlugin) Init(ctx context.Context, config map[string]any) error {
	return nil
}

// Start does nothing and returns nil. Override in your plugin if needed.
func (p *BasePlugin) Start(ctx context.Context) error {
	return nil
}

// Stop does nothing and returns nil. Override in your plugin if needed.
func (p *BasePlugin) Stop(ctx context.Context) error {
	return nil
}

// RegisterTools returns nil. Override in your plugin to provide tools.
func (p *BasePlugin) RegisterTools() []tools.Tool {
	return nil
}

// RegisterMiddlewares returns nil. Override in your plugin to provide middlewares.
func (p *BasePlugin) RegisterMiddlewares() []middleware.Middleware {
	return nil
}

// PluginInfo contains metadata about a plugin.
type PluginInfo struct {
	Name        string
	Version     string
	Description string
	Enabled     bool
	Config      map[string]any
}

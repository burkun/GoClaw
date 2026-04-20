// Package plugininit initializes the plugin system and integrates it with GoClaw.
package plugininit

import (
	"context"
	"fmt"

	"goclaw/internal/config"
	"goclaw/internal/logging"
	"goclaw/internal/middleware"
	"goclaw/internal/tools"
	"goclaw/pkg/plugin"
)

// InitializePlugins initializes the plugin system based on the configuration.
// It loads all enabled plugins, initializes them, and registers their tools and middlewares.
// Returns the plugin manager and any error encountered.
func InitializePlugins(ctx context.Context, cfg *config.AppConfig) (*plugin.Manager, error) {
	if cfg == nil {
		return nil, nil
	}

	// Check if plugin system is enabled
	if !cfg.Plugins.Enabled {
		logging.Debug("Plugin system is disabled")
		return nil, nil
	}

	mgr := plugin.NewManager()

	// Register built-in plugins (example: will be populated later)
	// For now, we just initialize the manager

	// Load plugins from configuration
	for name, pluginCfg := range cfg.Plugins.GetEnabledPlugins() {
		logging.Info("Loading plugin from config",
			"name", name,
			"enabled", pluginCfg.Enabled,
			"path", pluginCfg.Path,
		)

		// For configuration-based plugins, we'll create them later
		// For now, just log the configuration
		logging.Debug("Plugin config loaded",
			"name", name,
			"config", pluginCfg.Config,
		)
	}

	// Initialize all registered plugins
	if err := mgr.InitAll(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize plugins: %w", err)
	}

	// Start all plugins
	if err := mgr.StartAll(ctx); err != nil {
		return nil, fmt.Errorf("failed to start plugins: %w", err)
	}

	logging.Info("Plugin system initialized successfully",
		"plugin_count", len(mgr.List()),
	)

	return mgr, nil
}

// RegisterPluginTools registers all tools from plugins into the tool registry.
func RegisterPluginTools(mgr *plugin.Manager, registry *tools.ToolRegistry) error {
	if mgr == nil || registry == nil {
		return nil
	}

	for _, p := range mgr.List() {
		pluginTools := p.RegisterTools()
		for _, tool := range pluginTools {
			if err := registry.Register(tool); err != nil {
				return fmt.Errorf("failed to register tool from plugin %s: %w", p.Name(), err)
			}
			logging.Info("Registered tool from plugin",
				"plugin", p.Name(),
				"tool", tool.Name(),
			)
		}
	}

	return nil
}

// RegisterPluginMiddlewares registers all middlewares from plugins.
// Returns a slice of middlewares that can be added to the middleware chain.
func RegisterPluginMiddlewares(mgr *plugin.Manager) ([]middleware.Middleware, error) {
	if mgr == nil {
		return nil, nil
	}

	var allMiddlewares []middleware.Middleware

	for _, p := range mgr.List() {
		pluginMiddlewares := p.RegisterMiddlewares()
		for _, m := range pluginMiddlewares {
			allMiddlewares = append(allMiddlewares, m)
			logging.Info("Registered middleware from plugin",
				"plugin", p.Name(),
				"middleware", m.Name(),
			)
		}
	}

	return allMiddlewares, nil
}

// ShutdownPlugins gracefully shuts down the plugin system.
func ShutdownPlugins(ctx context.Context, mgr *plugin.Manager) error {
	if mgr == nil {
		return nil
	}

	if err := mgr.StopAll(ctx); err != nil {
		logging.Error("Failed to stop plugins", "error", err)
		return err
	}

	logging.Info("Plugin system shut down successfully")
	return nil
}

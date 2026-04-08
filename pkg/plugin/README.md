# GoClaw Plugin System

GoClaw supports a configuration-based plugin system that allows third-party extensions to add custom tools and middlewares.

## Overview

The plugin system provides:
- **Plugin Interface**: A standard interface that all plugins must implement
- **Plugin Manager**: Manages plugin lifecycle (registration, initialization, start, stop)
- **Configuration Support**: Plugins can be configured via `config.yaml`
- **Tool Registration**: Plugins can register custom tools
- **Middleware Registration**: Plugins can register custom middlewares

## Architecture

```
pkg/plugin/
├── plugin.go          # Plugin interface definition
├── manager.go         # Plugin manager implementation
└── examples/
    └── echo.go        # Example plugin implementation
```

## Plugin Interface

Every plugin must implement the `Plugin` interface:

```go
type Plugin interface {
    // Basic information
    Name() string
    Version() string
    Description() string

    // Lifecycle hooks
    Init(ctx context.Context, config map[string]any) error
    Start(ctx context.Context) error
    Stop(ctx context.Context) error

    // Capability registration
    RegisterTools() []tool.Tool
    RegisterMiddlewares() []middleware.Middleware
}
```

### Lifecycle

1. **Registration**: Plugin is registered with the manager
2. **Init**: Called once when the plugin is loaded, receives configuration
3. **Start**: Called when the application starts
4. **RegisterTools/RegisterMiddlewares**: Called to collect capabilities
5. **Stop**: Called during graceful shutdown

## Creating a Plugin

### 1. Implement the Plugin Interface

```go
package myplugin

import (
    "context"
    "github.com/bookerbai/goclaw/pkg/plugin"
    "github.com/bookerbai/goclaw/internal/tools"
    "github.com/bookerbai/goclaw/internal/middleware"
)

type MyPlugin struct {
    *plugin.BasePlugin
    // Add custom fields
}

func New() *MyPlugin {
    return &MyPlugin{
        BasePlugin: plugin.NewBasePlugin(
            "my-plugin",
            "1.0.0",
            "A custom plugin that does something useful",
        ),
    }
}

// Override methods as needed
func (p *MyPlugin) Init(ctx context.Context, config map[string]any) error {
    // Parse configuration
    if value, ok := config["key"].(string); ok {
        // Use configuration
    }
    return p.BasePlugin.Init(ctx, config)
}

func (p *MyPlugin) RegisterTools() []tools.Tool {
    return []tools.Tool{&myTool{}}
}

func (p *MyPlugin) RegisterMiddlewares() []middleware.Middleware {
    return []middleware.Middleware{&myMiddleware{}}
}
```

### 2. Use BasePlugin for Default Implementation

The `BasePlugin` provides default implementations for all methods, so you only need to override what you need:

```go
type SimplePlugin struct {
    *plugin.BasePlugin
}

func New() *SimplePlugin {
    return &SimplePlugin{
        BasePlugin: plugin.NewBasePlugin("simple", "1.0.0", "A simple plugin"),
    }
}

// Only override the methods you need
func (p *SimplePlugin) RegisterTools() []tools.Tool {
    return []tools.Tool{&myTool{}}
}
```

## Configuration

### config.yaml

Add plugin configuration to your `config.yaml`:

```yaml
plugins:
  enabled: true
  directory: "./plugins"  # Optional: base directory for plugins
  plugins:
    my-plugin:
      enabled: true
      config:
        key: "value"
        option: 42
```

### Plugin Configuration Structure

```go
type PluginsConfig struct {
    Enabled   bool                       // Enable/disable plugin system
    Directory string                     // Base directory for plugins
    Plugins  map[string]PluginConfig    // Plugin configurations
}

type PluginConfig struct {
    Enabled bool            // Enable/disable this plugin
    Path    string          // Optional path to plugin binary
    Config  map[string]any  // Plugin-specific configuration
}
```

## Using the Plugin Manager

### Manual Registration

```go
import (
    "context"
    "github.com/bookerbai/goclaw/pkg/plugin"
    "github.com/bookerbai/goclaw/pkg/pluginloader"
)

func main() {
    ctx := context.Background()
    cfg, _ := config.GetAppConfig()

    // Initialize plugin system from config
    mgr, err := pluginloader.InitializePlugins(ctx, cfg)
    if err != nil {
        log.Fatal(err)
    }
    defer pluginloader.ShutdownPlugins(ctx, mgr)

    // Or manually register plugins
    mgr := plugin.NewManager()
    myPlugin := myplugin.New()
    mgr.Register(myPlugin, map[string]any{
        "key": "value",
    })

    // Initialize and start
    mgr.InitAll(ctx)
    mgr.StartAll(ctx)
    defer mgr.StopAll(ctx)

    // Get tools and middlewares
    tools := mgr.GetAllTools()
    middlewares := mgr.GetAllMiddlewares()
}
```

### Using Default Manager

```go
import "github.com/bookerbai/goclaw/pkg/plugin"

func init() {
    // Register plugin at startup
    plugin.Register(myplugin.New(), map[string]any{
        "key": "value",
    })
}
```

## Example Plugin

See `pkg/plugin/examples/echo.go` for a complete example that demonstrates:
- Plugin initialization with configuration
- Custom tool implementation (echo tool)
- Custom middleware implementation (logging middleware)
- Lifecycle management

## Best Practices

1. **Use BasePlugin**: Embed `BasePlugin` to get default implementations
2. **Validate Configuration**: Check configuration types in `Init()`
3. **Log Meaningful Events**: Use logging to track plugin lifecycle
4. **Handle Errors Gracefully**: Return errors from lifecycle methods if initialization fails
5. **Clean Up Resources**: Implement `Stop()` to release resources
6. **Thread Safety**: The plugin manager is thread-safe, but your plugin should handle concurrent access to its own state

## Limitations

Currently, GoClaw uses a **configuration-based plugin system** rather than dynamic loading due to Go's plugin package limitations:
- Go plugins require exact version matching
- Symbol export issues
- Platform limitations (not supported on Windows)

Future enhancements may include:
- Lua/WASM-based dynamic plugins
- gRPC-based plugin architecture
- Hot-reload support

## Testing Plugins

```go
func TestMyPlugin(t *testing.T) {
    ctx := context.Background()
    p := myplugin.New()

    // Test initialization
    err := p.Init(ctx, map[string]any{"key": "value"})
    assert.NoError(t, err)

    // Test tools
    tools := p.RegisterTools()
    assert.NotEmpty(t, tools)

    // Test lifecycle
    err = p.Start(ctx)
    assert.NoError(t, err)

    err = p.Stop(ctx)
    assert.NoError(t, err)
}
```

package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/bookerbai/goclaw/internal/logging"
)

// SetLogger allows external code to inject a custom logger.
// Deprecated: Use logging.Init() instead.
func SetLogger(l *slog.Logger) {
	// No-op, kept for backward compatibility
}

// ---------------------------------------------------------------------------
// Load
// ---------------------------------------------------------------------------

// Load reads and parses the YAML config file at path, resolves $ENV_VAR
// substitutions, and returns a fully populated *AppConfig.
//
// Path resolution priority (mirroring DeerFlow):
//  1. The path argument (if non-empty).
//  2. GOCLAW_CONFIG_PATH environment variable.
//  3. ./config.yaml in the current working directory.
//  4. ../config.yaml (parent directory).
func Load(path string) (*AppConfig, error) {
	resolved, err := resolvePath(path)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", resolved, err)
	}

	// First decode into a generic map so we can apply env-var substitution
	// before re-decoding into typed structs.
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", resolved, err)
	}

	// Check config version before resolving env vars (mirrors DeerFlow).
	checkConfigVersion(raw, resolved)

	// config.yaml uses fail-fast for missing env vars (mirrors DeerFlow).
	raw, err = resolveEnvVarsInAny(raw, false)
	if err != nil {
		return nil, err
	}

	// Re-encode the resolved map back to YAML bytes and unmarshal into AppConfig.
	// This two-pass approach avoids a custom YAML decoder and keeps the struct tags.
	resolved2, err := yaml.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("config: re-marshal: %w", err)
	}

	var cfg AppConfig
	if err := yaml.Unmarshal(resolved2, &cfg); err != nil {
		return nil, fmt.Errorf("config: decode struct: %w", err)
	}

	// Load extensions_config.json / extensions.json from the same directory.
	extensions, err := loadExtensionsConfig(filepath.Dir(resolved))
	if err != nil {
		return nil, err
	}
	cfg.Extensions = extensions

	return &cfg, nil
}

// checkConfigVersion warns if the user's config.yaml is outdated compared to
// config.example.yaml. Mirrors DeerFlow's _check_config_version.
func checkConfigVersion(rawConfig any, configPath string) {
	// Extract user version from raw config map.
	var userVersion int
	if m, ok := rawConfig.(map[string]any); ok {
		if v, ok := m["config_version"]; ok {
			switch val := v.(type) {
			case int:
				userVersion = val
			case float64:
				userVersion = int(val)
			}
		}
	}

	// Find config.example.yaml by searching config.yaml's directory and parents.
	examplePath := ""
	searchDir := filepath.Dir(configPath)
	for i := 0; i < 5; i++ {
		candidate := filepath.Join(searchDir, "config.example.yaml")
		if _, err := os.Stat(candidate); err == nil {
			examplePath = candidate
			break
		}
		parent := filepath.Dir(searchDir)
		if parent == searchDir {
			break
		}
		searchDir = parent
	}

	if examplePath == "" {
		return
	}

	// Read example config version.
	exampleData, err := os.ReadFile(examplePath)
	if err != nil {
		return
	}

	var exampleRaw any
	if err := yaml.Unmarshal(exampleData, &exampleRaw); err != nil {
		return
	}

	var exampleVersion int
	if m, ok := exampleRaw.(map[string]any); ok {
		if v, ok := m["config_version"]; ok {
			switch val := v.(type) {
			case int:
				exampleVersion = val
			case float64:
				exampleVersion = int(val)
			}
		}
	}

	if userVersion < exampleVersion {
		logging.Warn("Your config.yaml is outdated",
			"current_version", userVersion,
			"latest_version", exampleVersion,
			"hint", "Run 'make config-upgrade' to merge new fields into your config.",
		)
	}
}

func loadExtensionsConfig(rootDir string) (ExtensionsConfig, error) {
	candidates := []string{
		filepath.Join(rootDir, "extensions_config.json"),
		filepath.Join(rootDir, "extensions.json"),
	}

	var target string
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			target = p
			break
		}
	}

	// Optional file: if not found, return empty config.
	if target == "" {
		return ExtensionsConfig{
			MCPServers: map[string]MCPServerConfig{},
			Skills:     map[string]SkillStateConfig{},
		}, nil
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return ExtensionsConfig{}, fmt.Errorf("config: read %s: %w", target, err)
	}

	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return ExtensionsConfig{}, fmt.Errorf("config: parse %s: %w", target, err)
	}

	// extensions_config.json uses fail-late for missing env vars (mirrors DeerFlow).
	// Missing env vars are replaced with empty string, allowing the service to start
	// but potentially fail later if the value is required.
	raw, err = resolveEnvVarsInAny(raw, true)
	if err != nil {
		return ExtensionsConfig{}, err
	}
	resolvedJSON, err := json.Marshal(raw)
	if err != nil {
		return ExtensionsConfig{}, fmt.Errorf("config: re-marshal extensions: %w", err)
	}

	var ext ExtensionsConfig
	if err := json.Unmarshal(resolvedJSON, &ext); err != nil {
		return ExtensionsConfig{}, fmt.Errorf("config: decode extensions struct: %w", err)
	}

	if ext.MCPServers == nil {
		ext.MCPServers = map[string]MCPServerConfig{}
	}
	if ext.Skills == nil {
		ext.Skills = map[string]SkillStateConfig{}
	}

	return ext, nil
}

// resolvePath applies the four-step path resolution strategy.
func resolvePath(path string) (string, error) {
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("config: file not found at %s", path)
		}
		return path, nil
	}

	if env := os.Getenv("GOCLAW_CONFIG_PATH"); env != "" {
		if _, err := os.Stat(env); err != nil {
			return "", fmt.Errorf("config: GOCLAW_CONFIG_PATH=%s not found", env)
		}
		return env, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("config: getwd: %w", err)
	}

	candidates := []string{
		filepath.Join(cwd, "config.yaml"),
		filepath.Join(filepath.Dir(cwd), "config.yaml"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("config: config.yaml not found in %s or its parent", cwd)
}

// ---------------------------------------------------------------------------
// Watch (hot reload)
// ---------------------------------------------------------------------------

// watcher holds the state for a single hot-reload subscription.
type watcher struct {
	path     string
	onChange func(*AppConfig)
	stop     chan struct{}
}

var (
	watchersMu sync.Mutex
	watchers   []*watcher
)

// Watch starts a background goroutine that polls the file at path every
// pollInterval and calls onChange whenever the file's mtime changes.
// Call the returned stop function to cancel the watcher.
//
// Implementation notes:
//   - Polling interval is 2 seconds (balances responsiveness vs. syscall cost).
//   - The initial mtime is recorded at Watch() call time; the first detected
//     change triggers an immediate reload.
//   - If Load() fails on a changed file, the error is logged and the previous
//     config remains in effect (fail-safe behaviour mirrors DeerFlow).
func Watch(path string, onChange func(*AppConfig)) (stop func()) {
	resolved, err := resolvePath(path)
	if err != nil {
		logging.Warn("config: Watch path resolution failed", "path", path, "error", err)
		return func() {}
	}

	w := &watcher{
		path:     resolved,
		onChange: onChange,
		stop:     make(chan struct{}),
	}

	watchersMu.Lock()
	watchers = append(watchers, w)
	watchersMu.Unlock()

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		var lastMtime time.Time
		if info, err := os.Stat(resolved); err == nil {
			lastMtime = info.ModTime()
		}

		for {
			select {
			case <-w.stop:
				return
			case <-ticker.C:
				info, err := os.Stat(resolved)
				if err != nil {
					logging.Warn("config: file stat failed, may be temporarily removed", "path", resolved, "error", err)
					continue
				}
				if info.ModTime().After(lastMtime) {
					lastMtime = info.ModTime()
					cfg, err := Load(resolved)
					if err != nil {
						logging.Error("config: reload failed, retaining old config", "path", resolved, "error", err)
						continue
					}
					onChange(cfg)
				}
			}
		}
	}()

	return func() { close(w.stop) }
}

// ---------------------------------------------------------------------------
// Singleton helpers (mirrors DeerFlow get_app_config / reload_app_config)
// ---------------------------------------------------------------------------

var (
	globalMu     sync.RWMutex
	globalConfig *AppConfig
	globalPath   string
	globalMtime  time.Time
)

// GetAppConfig returns the cached singleton AppConfig, reloading from disk
// automatically when the file's mtime has changed (hot-reload without Watch).
//
// The config path is resolved on first call using the standard priority order.
// Subsequent calls use the same resolved path; only the mtime is re-checked.
func GetAppConfig() (*AppConfig, error) {
	globalMu.RLock()
	current := globalConfig
	cachedPath := globalPath
	cachedMtime := globalMtime
	globalMu.RUnlock()

	// Resolve path (uses cached path if already set).
	path, err := resolvePath(cachedPath)
	if err != nil {
		if current != nil {
			return current, nil // serve stale config rather than erroring
		}
		return nil, err
	}

	info, err := os.Stat(path)
	if err != nil {
		if current != nil {
			return current, nil
		}
		return nil, err
	}

	// Return cached config if nothing has changed.
	if current != nil && path == cachedPath && !info.ModTime().After(cachedMtime) {
		return current, nil
	}

	// Reload from disk.
	cfg, err := Load(path)
	if err != nil {
		if current != nil {
			logging.Warn("config: reload failed, serving stale config", "path", path, "error", err)
			return current, nil
		}
		return nil, err
	}

	globalMu.Lock()
	globalConfig = cfg
	globalPath = path
	globalMtime = info.ModTime()
	globalMu.Unlock()

	return cfg, nil
}

// ReloadAppConfig forces a fresh load from disk, bypassing the mtime cache.
func ReloadAppConfig(path string) (*AppConfig, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	info, _ := os.Stat(path)

	globalMu.Lock()
	globalConfig = cfg
	globalPath = path
	if info != nil {
		globalMtime = info.ModTime()
	}
	globalMu.Unlock()

	return cfg, nil
}

// ResetAppConfig clears the singleton cache. Useful in tests.
func ResetAppConfig() {
	globalMu.Lock()
	globalConfig = nil
	globalPath = ""
	globalMtime = time.Time{}
	globalMu.Unlock()
}

// SetAppConfig injects a pre-built config (useful for testing).
func SetAppConfig(cfg *AppConfig) {
	globalMu.Lock()
	globalConfig = cfg
	globalPath = "" // mark as custom – skip mtime checks
	globalMtime = time.Time{}
	globalMu.Unlock()
}

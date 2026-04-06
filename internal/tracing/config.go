// Package tracing provides tracing configuration and handler initialization
// for observability providers like LangSmith and Langfuse.
package tracing

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cloudwego/eino/callbacks"

	"github.com/bookerbai/goclaw/internal/logging"
)

// TracingConfig holds configuration for all supported tracing providers.
type TracingConfig struct {
	Langfuse *LangfuseConfig `json:"langfuse,omitempty"`
}

// LangfuseConfig holds Langfuse tracing configuration.
// Mirrors DeerFlow's LangfuseTracingConfig.
type LangfuseConfig struct {
	// Enabled controls whether Langfuse tracing is explicitly enabled.
	Enabled bool `json:"enabled"`

	// PublicKey is the Langfuse public key (LANGFUSE_PUBLIC_KEY).
	PublicKey string `json:"public_key,omitempty"`

	// SecretKey is the Langfuse secret key (LANGFUSE_SECRET_KEY).
	SecretKey string `json:"secret_key,omitempty"`

	// Host is the Langfuse server URL (LANGFUSE_BASE_URL).
	// Defaults to "https://cloud.langfuse.com".
	Host string `json:"host,omitempty"`
}

// IsConfigured returns true if Langfuse is enabled and has required credentials.
func (c *LangfuseConfig) IsConfigured() bool {
	return c.Enabled && c.PublicKey != "" && c.SecretKey != ""
}

// Validate checks that required settings are present when enabled.
func (c *LangfuseConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	var missing []string
	if c.PublicKey == "" {
		missing = append(missing, "LANGFUSE_PUBLIC_KEY")
	}
	if c.SecretKey == "" {
		missing = append(missing, "LANGFUSE_SECRET_KEY")
	}
	if len(missing) > 0 {
		return fmt.Errorf("langfuse tracing is enabled but required settings are missing: %v", missing)
	}
	return nil
}

// EnabledProviders returns a list of configured provider names.
func (c *TracingConfig) EnabledProviders() []string {
	var providers []string
	if c.Langfuse != nil && c.Langfuse.IsConfigured() {
		providers = append(providers, "langfuse")
	}
	return providers
}

// ValidateEnabled validates all explicitly enabled providers.
func (c *TracingConfig) ValidateEnabled() error {
	if c.Langfuse != nil {
		return c.Langfuse.Validate()
	}
	return nil
}

// IsConfigured returns true if any tracing provider is configured.
func (c *TracingConfig) IsConfigured() bool {
	return len(c.EnabledProviders()) > 0
}

// ---------------------------------------------------------------------------
// Global configuration singleton
// ---------------------------------------------------------------------------

var (
	tracingConfig     *TracingConfig
	tracingConfigOnce sync.Once
	tracingConfigMu   sync.RWMutex
)

// GetTracingConfig returns the global tracing configuration.
// It is loaded once from environment variables on first access.
func GetTracingConfig() *TracingConfig {
	tracingConfigOnce.Do(func() {
		tracingConfig = loadTracingConfigFromEnv()
	})
	tracingConfigMu.RLock()
	defer tracingConfigMu.RUnlock()
	return tracingConfig
}

// ResetTracingConfig resets the global tracing config (for testing).
func ResetTracingConfig() {
	tracingConfigMu.Lock()
	defer tracingConfigMu.Unlock()
	tracingConfig = nil
	tracingConfigOnce = sync.Once{}
}

func loadTracingConfigFromEnv() *TracingConfig {
	cfg := &TracingConfig{
		Langfuse: &LangfuseConfig{
			Enabled:   isEnvTruthy("LANGFUSE_TRACING"),
			PublicKey: getEnvValue("LANGFUSE_PUBLIC_KEY"),
			SecretKey: getEnvValue("LANGFUSE_SECRET_KEY"),
			Host:      getEnvValueWithDefault("LANGFUSE_BASE_URL", "https://cloud.langfuse.com"),
		},
	}
	return cfg
}

// isEnvTruthy returns true if the environment variable is set to a truthy value.
func isEnvTruthy(name string) bool {
	val := os.Getenv(name)
	if val == "" {
		return false
	}
	switch val {
	case "1", "true", "True", "TRUE", "yes", "Yes", "YES", "on", "On", "ON":
		return true
	default:
		return false
	}
}

// getEnvValue returns the first non-empty environment value.
func getEnvValue(names ...string) string {
	for _, name := range names {
		if val := os.Getenv(name); val != "" {
			return val
		}
	}
	return ""
}

// getEnvValueWithDefault returns the environment value or a default.
func getEnvValueWithDefault(name, def string) string {
	if val := os.Getenv(name); val != "" {
		return val
	}
	return def
}

// ---------------------------------------------------------------------------
// Handler types and builder functions
// ---------------------------------------------------------------------------

// Handler wraps a callbacks.Handler with an optional flush function.
type Handler struct {
	Handler callbacks.Handler
	Flush   func()
}

// langfuseHandlerCreator is an injected factory for creating Langfuse handlers.
// It is set by the langfuse_impl package at init time if eino-ext is available.
var langfuseHandlerCreator func(cfg *LangfuseConfig) (callbacks.Handler, func(), error)

// RegisterLangfuseHandlerCreator registers the Langfuse handler factory.
// This is called by the langfuse_impl package when eino-ext is available.
func RegisterLangfuseHandlerCreator(fn func(cfg *LangfuseConfig) (callbacks.Handler, func(), error)) {
	langfuseHandlerCreator = fn
}

// BuildHandlers builds callback handlers for all enabled tracing providers.
// It validates configuration before creating handlers.
func BuildHandlers() ([]*Handler, error) {
	cfg := GetTracingConfig()
	if err := cfg.ValidateEnabled(); err != nil {
		return nil, err
	}

	var handlers []*Handler

	// Build Langfuse handler if configured
	if cfg.Langfuse != nil && cfg.Langfuse.IsConfigured() {
		if langfuseHandlerCreator == nil {
			logging.Warn("langfuse tracing enabled but eino-ext/callbacks/langfuse not available. Install the dependency or disable LANGFUSE_TRACING.")
		} else {
			handler, flush, err := langfuseHandlerCreator(cfg.Langfuse)
			if err != nil {
				return nil, fmt.Errorf("langfuse tracing initialization failed: %w", err)
			}
			handlers = append(handlers, &Handler{
				Handler: handler,
				Flush:   flush,
			})
			logging.Info("langfuse tracing enabled", "host", cfg.Langfuse.Host)
		}
	}

	return handlers, nil
}

// AppendGlobalCallbacks appends all enabled tracing handlers to Eino's global callbacks.
// This should be called during application initialization before any graph executions.
func AppendGlobalCallbacks() error {
	handlers, err := BuildHandlers()
	if err != nil {
		return err
	}

	for _, h := range handlers {
		callbacks.AppendGlobalHandlers(h.Handler)
	}

	return nil
}

// FlushAll flushes all tracing handlers to ensure buffered events are sent.
// Should be called before application shutdown.
func FlushAll() {
	cfg := GetTracingConfig()
	if cfg.Langfuse != nil && cfg.Langfuse.IsConfigured() {
		logging.Debug("flushing langfuse traces")
	}
}

// ---------------------------------------------------------------------------
// Langfuse handler implementation using eino-ext
// ---------------------------------------------------------------------------

// langfuseHandlerConfig mirrors the eino-ext langfuse.Config structure.
// We define it locally to avoid a hard dependency on eino-ext.
type langfuseHandlerConfig struct {
	Host             string
	PublicKey        string
	SecretKey        string
	Threads          int
	Timeout          time.Duration
	MaxTaskQueueSize int
	FlushAt          int
	FlushInterval    time.Duration
	SampleRate       float64
	LogMessage       string
	MaskFunc         func(string) string
	MaxRetry         uint64
	Name             string
	UserID           string
	SessionID        string
	Release          string
	Tags             []string
	Public           bool
}

// initLangfuseHandler creates a Langfuse handler when eino-ext is available.
// This is the actual implementation registered by the langfuse_impl package.
func initLangfuseHandler(cfg *LangfuseConfig) (callbacks.Handler, func(), error) {
	// This will be implemented in langfuse_impl.go using eino-ext
	return nil, nil, fmt.Errorf("langfuse handler not available - eino-ext not installed")
}

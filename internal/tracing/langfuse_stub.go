//go:build !langfuse

// Package tracing provides tracing configuration and handler initialization.
// This file is the stub implementation when eino-ext/langfuse is not available.
package tracing

import (
	"github.com/cloudwego/eino/callbacks"
)

func init() {
	// Register a no-op handler creator when eino-ext is not available
	// BuildHandlers will log a warning if Langfuse is enabled but not available
	RegisterLangfuseHandlerCreator(func(cfg *LangfuseConfig) (callbacks.Handler, func(), error) {
		return nil, nil, nil
	})
}

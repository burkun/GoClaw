//go:build langfuse

// Package tracing provides tracing configuration and handler initialization.
// This file is the real implementation when eino-ext/langfuse is available.
// Build with `-tags langfuse` to enable Langfuse support.
package tracing

import (
	"time"

	"github.com/cloudwego/eino/callbacks"

	langfuse "github.com/cloudwego/eino-ext/callbacks/langfuse"
)

func init() {
	RegisterLangfuseHandlerCreator(createLangfuseHandler)
}

func createLangfuseHandler(cfg *LangfuseConfig) (callbacks.Handler, func(), error) {
	handler, flush := langfuse.NewLangfuseHandler(&langfuse.Config{
		Host:          cfg.Host,
		PublicKey:     cfg.PublicKey,
		SecretKey:     cfg.SecretKey,
		Threads:       1,
		FlushAt:       15,
		FlushInterval: 500 * time.Millisecond,
		SampleRate:    1.0,
		MaxRetry:      3,
	})
	return handler, flush, nil
}

package models

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/cloudwego/eino/components/model"

	"github.com/bookerbai/goclaw/internal/config"
	"github.com/bookerbai/goclaw/internal/logging"
)

// SetLogger allows external code to inject a custom logger.
// Deprecated: Use logging.Init() instead.
func SetLogger(l *slog.Logger) {
	// No-op, kept for backward compatibility
}

// CreateRequest 描述一次模型创建请求。
type CreateRequest struct {
	// ModelName 指定模型配置名；为空时使用默认模型（第一项）。
	ModelName string

	// ThinkingEnabled 是否启用思考模式。
	ThinkingEnabled bool

	// ReasoningEffort 可选推理强度参数（低/中/高等，按 provider 解释）。
	ReasoningEffort string
}

// BuildOptions 是传入 ProviderBuilder 的运行参数。
type BuildOptions struct {
	ThinkingEnabled bool
	ReasoningEffort string
}

// ProviderBuilder 根据 ModelConfig 构建 Eino BaseChatModel。
type ProviderBuilder func(ctx context.Context, cfg config.ModelConfig, opts BuildOptions) (model.BaseChatModel, error)

var (
	buildersMu sync.RWMutex
	builders   = map[string]ProviderBuilder{}
)

// RegisterProviderBuilder 注册 provider 构建器（例如 openai/anthropic/google）。
func RegisterProviderBuilder(provider string, builder ProviderBuilder) {
	buildersMu.Lock()
	defer buildersMu.Unlock()
	builders[strings.ToLower(strings.TrimSpace(provider))] = builder
}

// ResetProviderBuilders 清空注册（用于测试）。
func ResetProviderBuilders() {
	buildersMu.Lock()
	defer buildersMu.Unlock()
	builders = map[string]ProviderBuilder{}
}

func getProviderBuilder(provider string) (ProviderBuilder, bool) {
	buildersMu.RLock()
	defer buildersMu.RUnlock()
	b, ok := builders[strings.ToLower(strings.TrimSpace(provider))]
	return b, ok
}

// CreateChatModel 根据 AppConfig 和请求参数创建模型。
func CreateChatModel(ctx context.Context, appCfg *config.AppConfig, req CreateRequest) (model.BaseChatModel, error) {
	if appCfg == nil {
		return nil, fmt.Errorf("models: app config is nil")
	}

	var modelCfg *config.ModelConfig
	if strings.TrimSpace(req.ModelName) == "" {
		modelCfg = appCfg.DefaultModel()
	} else {
		modelCfg = appCfg.GetModelConfig(req.ModelName)
	}
	if modelCfg == nil {
		if strings.TrimSpace(req.ModelName) == "" {
			return nil, fmt.Errorf("models: no model configured")
		}
		return nil, fmt.Errorf("models: model %q not found", req.ModelName)
	}

	// Thinking mode compatibility check (mirrors DeerFlow factory.py).
	// Warn and disable thinking mode if model doesn't support it, rather than failing.
	if req.ThinkingEnabled && !modelCfg.SupportsThinking {
		logging.Warn("thinking mode enabled but model does not support it; disabling",
			"model", modelCfg.Name)
		req.ThinkingEnabled = false
	}

	provider := strings.ToLower(strings.TrimSpace(modelCfg.Use))
	builder, ok := getProviderBuilder(provider)
	if !ok {
		return nil, fmt.Errorf("models: provider %q is not registered", provider)
	}

	// Warn if reasoning_effort is requested but not supported.
	if req.ReasoningEffort != "" && !modelCfg.SupportsReasoningEffort {
		logging.Warn("reasoning_effort requested but model does not support it; ignoring",
			"model", modelCfg.Name,
			"reasoning_effort", req.ReasoningEffort)
	}

	return builder(ctx, *modelCfg, BuildOptions{
		ThinkingEnabled: req.ThinkingEnabled,
		ReasoningEffort: req.ReasoningEffort,
	})
}

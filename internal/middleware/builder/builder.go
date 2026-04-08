package builder

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"

	"github.com/bookerbai/goclaw/internal/config"
	"github.com/bookerbai/goclaw/internal/middleware"
	"github.com/bookerbai/goclaw/internal/middleware/clarification"
	"github.com/bookerbai/goclaw/internal/middleware/control"
	"github.com/bookerbai/goclaw/internal/middleware/dangling"
	"github.com/bookerbai/goclaw/internal/middleware/llmerror"
	"github.com/bookerbai/goclaw/internal/middleware/memory"
	"github.com/bookerbai/goclaw/internal/middleware/monitoring"
	"github.com/bookerbai/goclaw/internal/middleware/sandboxmw"
	"github.com/bookerbai/goclaw/internal/middleware/summarize"
	"github.com/bookerbai/goclaw/internal/middleware/threaddata"
	"github.com/bookerbai/goclaw/internal/middleware/todo"
	"github.com/bookerbai/goclaw/internal/middleware/tool"
	"github.com/bookerbai/goclaw/internal/middleware/uploads"
	"github.com/bookerbai/goclaw/internal/middleware/ui"
	"github.com/bookerbai/goclaw/internal/sandbox"
	"github.com/bookerbai/goclaw/pkg/errors"
)

// BuilderConfig 包含构建 middleware 所需的所有配置
type BuilderConfig struct {
	AppConfig    *config.AppConfig
	ModelName    string

	// Sandbox provider
	SandboxProvider sandbox.SandboxProvider

	// Model creator function
	CreateChatModel middleware.ModelCreator
}

// BuildMiddlewaresFromBuilder 使用 BuilderConfig 构建 middleware 链
// 这个函数将配置逻辑从 agent 包中解耦出来
func BuildMiddlewaresFromBuilder(cfg *BuilderConfig) []middleware.Middleware {
	if cfg == nil {
		cfg = &BuilderConfig{}
	}

	// Create helper for creating chat models
	createChatModel := func(modelName string) (model.BaseChatModel, error) {
		if cfg.CreateChatModel == nil {
			return nil, errors.ConfigError("model creator not provided")
		}
		return cfg.CreateChatModel(context.Background(), modelName)
	}

	// Extract config values with defaults
	memoryEnabled := true
	titleEnabled := true
	summarizeEnabled := true
	memoryPath := memory.DefaultMemoryPath
	memoryDebounce := 30 * time.Second
	memoryConfidence := 0.7
	memoryMaxFacts := 100
	memoryInjectionEnabled := true
	memoryMaxInjectionTokens := 2000

	if cfg.AppConfig != nil {
		memoryEnabled = cfg.AppConfig.Memory.Enabled
		titleEnabled = cfg.AppConfig.Title.Enabled
		summarizeEnabled = cfg.AppConfig.Summarization.Enabled
		if strings.TrimSpace(cfg.AppConfig.Memory.StoragePath) != "" {
			memoryPath = cfg.AppConfig.Memory.StoragePath
		}
		if cfg.AppConfig.Memory.DebounceSeconds > 0 {
			memoryDebounce = time.Duration(cfg.AppConfig.Memory.DebounceSeconds) * time.Second
		}
		if cfg.AppConfig.Memory.FactConfidenceThreshold > 0 {
			memoryConfidence = cfg.AppConfig.Memory.FactConfidenceThreshold
		}
		if cfg.AppConfig.Memory.MaxFacts > 0 {
			memoryMaxFacts = cfg.AppConfig.Memory.MaxFacts
		}
		memoryInjectionEnabled = cfg.AppConfig.Memory.InjectionEnabled
		if cfg.AppConfig.Memory.MaxInjectionTokens > 0 {
			memoryMaxInjectionTokens = cfg.AppConfig.Memory.MaxInjectionTokens
		}
	}

	middlewares := make([]middleware.Middleware, 0, 16)

	// Core middleware chain aligned with deer-flow order.
	middlewares = append(middlewares, threaddata.New(threaddata.DefaultConfig()))
	middlewares = append(middlewares, uploads.New())
	if cfg.SandboxProvider != nil {
		middlewares = append(middlewares, sandboxmw.New(cfg.SandboxProvider))
	}
	middlewares = append(middlewares, dangling.New())

	// GuardrailMiddleware
	guardrailCfg := control.DefaultGuardrailConfig()
	if cfg.AppConfig != nil {
		guardrailCfg.Enabled = cfg.AppConfig.Guardrails.Enabled
		guardrailCfg.FailClosed = cfg.AppConfig.Guardrails.FailClosed
		if cfg.AppConfig.Guardrails.Passport != nil {
			guardrailCfg.Passport = *cfg.AppConfig.Guardrails.Passport
		}

		// Build AllowlistProvider from provider config if configured.
		if cfg.AppConfig.Guardrails.Provider != nil {
			providerCfg := cfg.AppConfig.Guardrails.Provider
			// Check if using built-in AllowlistProvider.
			if providerCfg.Use == "" || providerCfg.Use == "allowlist" || providerCfg.Use == "goclaw/internal/middleware/control:AllowlistProvider" {
				allowlistCfg := control.AllowlistProviderConfig{}
				if providerCfg.Config != nil {
					if allowed, ok := providerCfg.Config["allowed_tools"]; ok {
						switch vv := allowed.(type) {
						case []any:
							for _, item := range vv {
								if name, ok := item.(string); ok && strings.TrimSpace(name) != "" {
									allowlistCfg.AllowedTools = append(allowlistCfg.AllowedTools, strings.TrimSpace(name))
								}
							}
						case []string:
							for _, name := range vv {
								if strings.TrimSpace(name) != "" {
									allowlistCfg.AllowedTools = append(allowlistCfg.AllowedTools, strings.TrimSpace(name))
								}
							}
						}
					}
					if denied, ok := providerCfg.Config["denied_tools"]; ok {
						switch vv := denied.(type) {
						case []any:
							for _, item := range vv {
								if name, ok := item.(string); ok && strings.TrimSpace(name) != "" {
									allowlistCfg.DeniedTools = append(allowlistCfg.DeniedTools, strings.TrimSpace(name))
								}
							}
						case []string:
							for _, name := range vv {
								if strings.TrimSpace(name) != "" {
									allowlistCfg.DeniedTools = append(allowlistCfg.DeniedTools, strings.TrimSpace(name))
								}
							}
						}
					}
				}
				guardrailCfg.Provider = control.NewAllowlistProvider(allowlistCfg)
			}
			// For custom providers, fall back to legacy policy-based approach.
			// Future: support dynamic provider loading via reflection/plugin.
		}
	}
	middlewares = append(middlewares, control.NewGuardrailMiddleware(guardrailCfg))

	middlewares = append(middlewares, monitoring.NewSandboxAuditMiddleware(nil))

	if summarizeEnabled {
		summCfg := summarize.DefaultConfig()
		if cfg.AppConfig != nil {
			if strings.TrimSpace(cfg.AppConfig.Summarization.SummaryPrompt) != "" {
				summCfg.PromptTemplate = cfg.AppConfig.Summarization.SummaryPrompt
			}
			if cfg.AppConfig.Summarization.Keep.Type == "messages" && int(cfg.AppConfig.Summarization.Keep.Value) > 0 {
				summCfg.KeepRecentMessages = int(cfg.AppConfig.Summarization.Keep.Value)
			}
			for _, tr := range cfg.AppConfig.Summarization.Trigger {
				switch strings.ToLower(strings.TrimSpace(tr.Type)) {
				case "fraction":
					if tr.Value > 0 && tr.Value <= 1 {
						summCfg.ThresholdRatio = tr.Value
					}
				case "tokens":
					if tr.Value > 0 {
						summCfg.TokenLimit = int(tr.Value)
					}
				}
			}
		}
		var summarizer summarize.Summarizer
		if cfg.AppConfig != nil {
			if cm, err := createChatModel(cfg.AppConfig.Summarization.ModelName); err == nil && cm != nil {
				summarizer = summarize.NewEinoSummarizer(cm)
			}
		}
		middlewares = append(middlewares, summarize.NewSummarizationMiddleware(summCfg, summarizer))
	}

	middlewares = append(middlewares, todo.NewTodoMiddleware())

	// TitleMiddleware comes after TodoMiddleware (matches DeerFlow order #8)
	if titleEnabled {
		titleCfg := ui.DefaultTitleConfig()
		if cfg.AppConfig != nil {
			if cfg.AppConfig.Title.MaxWords > 0 {
				titleCfg.MaxWords = cfg.AppConfig.Title.MaxWords
			}
		}
		var titleGen ui.TitleGenerator
		if cfg.AppConfig != nil {
			if cm, err := createChatModel(cfg.AppConfig.Title.ModelName); err == nil && cm != nil {
				titleGen = ui.NewEinoTitleGenerator(cm)
			}
		}
		middlewares = append(middlewares, ui.NewTitleMiddleware(titleCfg, titleGen))
	}

	if memoryEnabled {
		store := memory.NewJSONFileStore(memoryPath)
		queue := memory.GetGlobalQueue(memoryPath)
		queue.DebounceDelay = memoryDebounce
		queue.SetMaxFacts(memoryMaxFacts)
		if cfg.AppConfig != nil {
			if cm, err := createChatModel(cfg.AppConfig.Memory.ModelName); err == nil && cm != nil {
				queue.SetExtractor(memory.NewEinoFactExtractor(cm, memoryConfidence))
			}
		}
		middlewares = append(middlewares, memory.NewMemoryMiddleware(
			store,
			queue,
			"",
			memory.WithInjectionEnabled(memoryInjectionEnabled),
			memory.WithMaxInjectionTokens(memoryMaxInjectionTokens),
		))
	}

	middlewares = append(middlewares, ui.NewViewImageMiddleware())
	middlewares = append(middlewares, control.NewSubagentLimitMiddleware(control.DefaultSubagentLimitConfig()))

	// LoopDetectionMiddleware comes after SubagentLimitMiddleware (matches DeerFlow order #12)
	middlewares = append(middlewares, control.NewLoopDetectionMiddleware(control.DefaultLoopDetectionConfig()))

	middlewares = append(middlewares, tool.NewDeferredToolFilterMiddleware(tool.DefaultDeferredTools()))
	if cfg.AppConfig != nil && cfg.AppConfig.TokenUsage.Enabled {
		middlewares = append(middlewares, monitoring.NewTokenUsageMiddleware())
	}
	llmErrorMaxRetries := 3
	if cfg.AppConfig != nil {
		var targetModel *config.ModelConfig
		if strings.TrimSpace(cfg.ModelName) != "" {
			targetModel = cfg.AppConfig.GetModelConfig(strings.TrimSpace(cfg.ModelName))
		}
		if targetModel == nil {
			targetModel = cfg.AppConfig.DefaultModel()
		}
		if targetModel != nil && targetModel.MaxRetries > 0 {
			llmErrorMaxRetries = targetModel.MaxRetries
		}
	}
	middlewares = append(middlewares, llmerror.NewLLMErrorHandlingMiddleware(llmErrorMaxRetries))
	middlewares = append(middlewares, tool.NewToolErrorHandlingMiddleware())
	middlewares = append(middlewares, clarification.NewClarificationMiddleware())

	return middlewares
}

// RegisterMiddlewares 注册所有 middleware 到 registry
// 这是一个辅助函数,用于在初始化时注册所有 middleware
func RegisterMiddlewares(registry middleware.Registry, middlewares []middleware.Middleware) error {
	for _, mw := range middlewares {
		if err := registry.Register(mw); err != nil {
			return errors.WrapInternalError(err, fmt.Sprintf("failed to register middleware %s", mw.Name()))
		}
	}
	return nil
}

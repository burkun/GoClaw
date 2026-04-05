# 第 20 章　模型配置与适配

## 20.1　模型工厂

```go
// internal/models/factory.go

type CreateRequest struct {
    ModelName        string
    ThinkingEnabled  bool
    VisionEnabled    bool
}

func CreateChatModel(ctx context.Context, cfg *config.AppConfig, req CreateRequest) (model.ChatModel, error) {
    // 查找模型配置
    modelCfg := findModelConfig(cfg, req.ModelName)
    if modelCfg == nil {
        return nil, fmt.Errorf("model not found: %s", req.ModelName)
    }

    // 根据 use 字段创建模型
    switch modelCfg.Use {
    case "openai":
        return createOpenAIModel(ctx, modelCfg, req)
    case "anthropic":
        return createAnthropicModel(ctx, modelCfg, req)
    case "azure":
        return createAzureModel(ctx, modelCfg, req)
    default:
        // 尝试反射加载
        return createByReflection(ctx, modelCfg, req)
    }
}

func findModelConfig(cfg *config.AppConfig, name string) *config.ModelConfig {
    if name == "" {
        // 返回第一个模型
        if len(cfg.Models) > 0 {
            return &cfg.Models[0]
        }
        return nil
    }

    for i := range cfg.Models {
        if cfg.Models[i].Name == name {
            return &cfg.Models[i]
        }
    }

    return nil
}
```

## 20.2　OpenAI 模型

```go
func createOpenAIModel(ctx context.Context, cfg *config.ModelConfig, req CreateRequest) (model.ChatModel, error) {
    opts := []openai.Option{
        openai.WithModel(cfg.Model),
        openai.WithAPIKey(cfg.APIKey),
    }

    if cfg.BaseURL != "" {
        opts = append(opts, openai.WithBaseURL(cfg.BaseURL))
    }

    if cfg.MaxTokens > 0 {
        opts = append(opts, openai.WithMaxTokens(cfg.MaxTokens))
    }

    if cfg.Temperature > 0 {
        opts = append(opts, openai.WithTemperature(cfg.Temperature))
    }

    return openai.NewChatModel(ctx, opts...)
}
```

## 20.3　Anthropic 模型

```go
func createAnthropicModel(ctx context.Context, cfg *config.ModelConfig, req CreateRequest) (model.ChatModel, error) {
    opts := []anthropic.Option{
        anthropic.WithModel(cfg.Model),
        anthropic.WithAPIKey(cfg.APIKey),
    }

    if cfg.MaxTokens > 0 {
        opts = append(opts, anthropic.WithMaxTokens(cfg.MaxTokens))
    }

    // Thinking 支持
    if req.ThinkingEnabled && cfg.SupportsThinking {
        opts = append(opts, anthropic.WithThinkingEnabled(true))
    }

    return anthropic.NewChatModel(ctx, opts...)
}
```

## 20.4　反射加载

```go
func createByReflection(ctx context.Context, cfg *config.ModelConfig, req CreateRequest) (model.ChatModel, error) {
    // use 格式: "module/path:StructName"
    parts := strings.Split(cfg.Use, ":")
    if len(parts) != 2 {
        return nil, fmt.Errorf("invalid use format: %s", cfg.Use)
    }

    modulePath := parts[0]
    structName := parts[1]

    // 动态导入模块
    var module interface{}
    switch modulePath {
    case "langchain_openai":
        // 使用 langchain-openai-go
    case "langchain_anthropic":
        // 使用 langchain-anthropic-go
    default:
        return nil, fmt.Errorf("unknown module: %s", modulePath)
    }

    // 创建实例
    creator, ok := module.(ModelCreator)
    if !ok {
        return nil, fmt.Errorf("module does not implement ModelCreator")
    }

    return creator.Create(ctx, cfg, req)
}
```

## 20.5　模型配置示例

### OpenAI

```yaml
models:
  - name: gpt-4
    display_name: GPT-4
    use: openai
    model: gpt-4
    api_key: $OPENAI_API_KEY
    max_tokens: 4096
    temperature: 0.7
```

### OpenRouter

```yaml
models:
  - name: gemini-2.5-flash
    display_name: Gemini 2.5 Flash
    use: openai
    model: google/gemini-2.5-flash-preview
    api_key: $OPENROUTER_API_KEY
    base_url: https://openrouter.ai/api/v1
```

### Anthropic

```yaml
models:
  - name: claude-sonnet-4
    display_name: Claude Sonnet 4
    use: anthropic
    model: claude-sonnet-4-20250514
    api_key: $ANTHROPIC_API_KEY
    max_tokens: 4096
    supports_thinking: true
    supports_vision: true
```

### Azure OpenAI

```yaml
models:
  - name: azure-gpt-4
    display_name: Azure GPT-4
    use: azure
    model: gpt-4
    api_key: $AZURE_OPENAI_API_KEY
    base_url: $AZURE_OPENAI_ENDPOINT
    api_version: "2024-02-15-preview"
```

## 20.6　Thinking 模式

```go
type ThinkingConfig struct {
    Enabled       bool
    BudgetTokens  int  // 预算 token 数
}

func applyThinkingConfig(opts []openai.Option, cfg *config.ModelConfig, req CreateRequest) []openai.Option {
    if !req.ThinkingEnabled || !cfg.SupportsThinking {
        return opts
    }

    // 模型特定的 thinking 配置
    if cfg.ThinkingBudget > 0 {
        opts = append(opts, openai.WithThinkingBudget(cfg.ThinkingBudget))
    }

    return opts
}
```

## 20.7　Vision 模式

```go
func supportsVision(cfg *config.ModelConfig) bool {
    return cfg.SupportsVision
}

// 在工具组装时检查
func (a *leadAgent) buildTools(ctx context.Context, cfg RunConfig) []lcTool.BaseTool {
    var tools []lcTool.BaseTool

    // ...

    // 仅在模型支持 vision 时添加 view_image
    if a.modelSupportsVision {
        tools = append(tools, builtin.NewViewImageTool())
    }

    return tools
}
```

## 20.8　模型切换

```go
// 运行时切换模型
func (a *leadAgent) Run(ctx context.Context, state *ThreadState, cfg RunConfig) (<-chan Event, error) {
    // 根据 cfg.ModelName 创建模型
    chatModel, err := models.CreateChatModel(ctx, a.appCfg, models.CreateRequest{
        ModelName:       cfg.ModelName,
        ThinkingEnabled: cfg.ThinkingEnabled,
    })

    // ...
}
```

# 第 4 章　Eino 框架：Go 的 Agent 编排

## 4.1　Eino 是什么

Eino 是 CloudWeGo（字节跳动开源）推出的 Go 语言 AI Agent 框架，对标 Python 的 LangChain/LangGraph。它提供了：

- **ADK (Agent Development Kit)**: 高层 Agent 抽象
- **Components**: 模块化组件（model、tool、retriever 等）
- **Compose**: 图编排能力
- **Schema**: 统一的消息类型定义

## 4.2　核心概念

### 消息 Schema

```go
import "github.com/cloudwego/eino/schema"

// 消息类型
message := &schema.Message{
    Role:    schema.Human,      // Human / Assistant / System / Tool
    Content: "Hello",
}

// 多模态消息
message := &schema.Message{
    Role: schema.Human,
    MultiContent: []schema.ChatMessagePart{
        {
            Type: schema.ChatMessagePartTypeText,
            Text: "What is in this image?",
        },
        {
            Type: schema.ChatMessagePartTypeImageURL,
            ImageURL: &schema.ChatMessagePartImageURL{
                URL: "data:image/png;base64,...",
            },
        },
    },
}
```

### Tool 接口

```go
import lcTool "github.com/cloudwego/eino/components/tool"

// 工具定义
toolInfo := &lcTool.ToolInfo{
    Name: "bash",
    Desc: "Execute a bash command",
    ParamsOneOf: schema.NewParamsOneOfByParams(map[string]any{
        "command": map[string]any{
            "type":        "string",
            "description": "The command to execute",
        },
    }),
}

// 工具实现
type BashTool struct{}

func (t *BashTool) Info(ctx context.Context) (*lcTool.ToolInfo, error) {
    return toolInfo, nil
}

func (t *BashTool) Run(ctx context.Context, input string) (string, error) {
    // 执行命令
    return "output", nil
}
```

### Model 接口

```go
import "github.com/cloudwego/eino/components/model"

// 模型调用
chatModel, _ := openai.NewChatModel(ctx, &openai.ChatModelConfig{
    Model:   "gpt-4",
    APIKey:  "your-key",
})

response, _ := chatModel.Generate(ctx, []*schema.Message{
    {Role: schema.Human, Content: "Hello"},
})
```

### Agent (ADK)

```go
import "github.com/cloudwego/eino/adk"

// 创建 Agent
agent, _ := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
    ChatModel: chatModel,
    Tools:     []lcTool.BaseTool{bashTool},
    // 中间件通过 Handlers 注入
    Handlers: middlewares,
})
```

## 4.3　GoClaw 的 Eino 适配

### Eino Runner

GoClaw 封装了 Eino 的运行时：

```go
// internal/eino/runner.go
type Runner struct {
    agent adk.Agent
}

func (r *Runner) Run(ctx context.Context, input *schema.Message, opts ...compose.Option) (*schema.Message, error) {
    return r.agent.Generate(ctx, []*schema.Message{input}, opts...)
}
```

### 中间件适配

Eino 的中间件接口是 `ChatModelAgentMiddleware`，GoClaw 定义了自己的 `Middleware` 接口并适配：

```go
// internal/middleware/eino_adapter.go

// AdaptMiddlewares 将 GoClaw Middleware 转换为 Eino 中间件
func AdaptMiddlewares(middlewares []Middleware) []adk.ChatModelAgentMiddleware {
    var result []adk.ChatModelAgentMiddleware
    for _, mw := range middlewares {
        result = append(result, adaptMiddleware(mw))
    }
    return result
}

func adaptMiddleware(mw Middleware) adk.ChatModelAgentMiddleware {
    return adk.ChatModelAgentMiddleware{
        BeforeAgent: func(ctx context.Context, input *adk.AgentInput) (*adk.AgentInput, error) {
            state := GetStateFromContext(ctx)
            if err := mw.BeforeAgent(ctx, state); err != nil {
                return nil, err
            }
            return input, nil
        },
        // ... 其他钩子
    }
}
```

## 4.4　工具调用流程

```
User Input
    │
    ▼
┌─────────────────┐
│  Lead Agent     │
│  (Eino Agent)   │
└─────────────────┘
    │
    ▼
┌─────────────────┐
│  ChatModel      │ ──► 决定调用工具
│  (LLM)          │
└─────────────────┘
    │
    ▼
┌─────────────────┐
│  Middleware     │ ──► WrapToolCall 钩子
│  WrapToolCall   │
└─────────────────┘
    │
    ▼
┌─────────────────┐
│  Tool Execution │ ──► 实际执行工具
└─────────────────┘
    │
    ▼
┌─────────────────┐
│  Middleware     │ ──► After 钩子
│  After          │
└─────────────────┘
    │
    ▼
继续循环或返回响应
```

## 4.5　与 LangGraph 的对比

| 特性 | LangGraph (Python) | Eino (Go) |
|------|-------------------|-----------|
| 图编排 | 显式 StateGraph | 隐式 Agent 循环 |
| 状态管理 | checkpointing | 手动实现 |
| 中间件 | before_model/after_model | ChatModelAgentMiddleware |
| 流式输出 | astream_events | GenerateStream |
| 并发模型 | asyncio | goroutine |

## 4.6　最佳实践

### 1. 使用 Context 传递状态

```go
// 存储
ctx = context.WithValue(ctx, middlewareStateKey, state)

// 读取
state := ctx.Value(middlewareStateKey).(*middleware.State)
```

### 2. 流式响应处理

```go
stream, _ := chatModel.Stream(ctx, messages)
for {
    chunk, err := stream.Recv()
    if err == io.EOF {
        break
    }
    // 处理 chunk
}
```

### 3. 错误处理

Eino 的错误需要转换为 GoClaw 的错误类型：

```go
if errors.Is(err, context.Canceled) {
    return &Event{Type: EventError, Payload: map[string]any{
        "code":    "agent/context_cancelled",
        "message": "context cancelled",
    }}
}
```

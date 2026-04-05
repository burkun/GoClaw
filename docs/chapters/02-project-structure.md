# 第 2 章　项目结构与技术栈

## 2.1　目录结构

```text
goclaw/
├── cmd/
│   └── goclaw/
│       └── main.go              # 程序入口
├── internal/
│   ├── agent/                   # Lead Agent 核心
│   │   ├── lead.go              # Agent 主逻辑
│   │   ├── events.go            # 事件定义
│   │   ├── checkpoint_store.go  # 状态持久化
│   │   └── subagents/           # Sub-Agent 系统
│   │       ├── executor.go      # 执行引擎
│   │       ├── task_tool.go     # task 工具
│   │       ├── agent_worker.go  # Worker 抽象
│   │       └── builtins/        # 内置子代理
│   ├── middleware/              # 中间件系统
│   │   ├── middleware.go        # 接口定义
│   │   ├── eino_adapter.go      # Eino 适配器
│   │   ├── memory/              # 记忆中间件
│   │   ├── title/               # 标题生成
│   │   ├── todo/                # 计划模式
│   │   ├── sandboxmw/           # 沙箱管理
│   │   ├── guardrail/           # 工具调用守卫
│   │   └── ...                  # 其他中间件
│   ├── sandbox/                 # 沙箱抽象
│   │   ├── sandbox.go           # 接口定义
│   │   ├── local/               # 本地沙箱
│   │   └── docker/              # Docker 沙箱
│   ├── tools/                   # 工具系统
│   │   ├── tool.go              # 工具接口
│   │   ├── bootstrap/           # 工具注册
│   │   ├── fs/                  # 文件系统工具
│   │   ├── shell/               # Shell 工具
│   │   ├── web/                 # Web 工具
│   │   ├── search/              # 搜索工具
│   │   ├── builtin/             # 内置工具
│   │   └── mcp_*.go             # MCP 集成
│   ├── models/                  # 模型工厂
│   ├── skills/                  # Skills 系统
│   ├── channels/                # IM 渠道
│   │   ├── base.go              # 基础接口
│   │   ├── bus.go               # 消息总线
│   │   ├── manager.go           # 渠道管理
│   │   ├── telegram/            # Telegram 渠道
│   │   ├── slack/               # Slack 渠道
│   │   └── feishu/              # 飞书渠道
│   ├── config/                  # 配置系统
│   ├── agentconfig/             # Agent 配置加载
│   └── eino/                    # Eino 运行时
├── pkg/
│   └── gateway/                 # HTTP Gateway
│       ├── server.go            # Gin 服务器
│       └── handlers/            # 路由处理器
├── configs/
│   └── config.yaml              # 配置示例
├── .goclaw/
│   └── agents/                  # 自定义 Agent
├── config.yaml                  # 主配置文件
├── go.mod                       # Go 模块
├── go.sum                       # 依赖锁定
├── PLAN.md                      # 实现计划
└── EVENTS.md                    # 事件协议文档
```

## 2.2　技术栈

### 核心框架

| 组件 | 技术 | 版本 | 用途 |
|------|------|------|------|
| Agent 框架 | CloudWeGo Eino | latest | Agent 编排、工具调用、流式处理 |
| HTTP 框架 | Gin | latest | REST API、SSE 流式响应 |
| 配置解析 | gopkg.in/yaml.v3 | latest | YAML 配置加载 |

### Eino 框架核心组件

```go
import (
    "github.com/cloudwego/eino/adk"
    "github.com/cloudwego/eino/components/model"
    "github.com/cloudwego/eino/components/tool"
    "github.com/cloudwego/eino/compose"
    "github.com/cloudwego/eino/schema"
)
```

- **adk**: Agent Development Kit，提供 `ChatModelAgent` 等高层抽象
- **components/model**: LLM 模型接口
- **components/tool**: 工具接口
- **compose**: 组合器，用于构建 Agent 图
- **schema**: 消息 schema（HumanMessage, AIMessage 等）

## 2.3　模块职责

### internal/ vs pkg/

Go 项目遵循标准布局：
- `internal/`: 私有模块，不可被外部导入
- `pkg/`: 公开模块，可被外部导入

GoClaw 的划分：
- `internal/agent`: Agent 核心逻辑（私有）
- `internal/middleware`: 中间件实现（私有）
- `internal/sandbox`: 沙箱实现（私有）
- `pkg/gateway`: HTTP API（可公开）

### 依赖方向

```
pkg/gateway
    │
    ▼
internal/agent ─────────────────┐
    │                           │
    ▼                           ▼
internal/middleware      internal/subagents
    │                           │
    ▼                           ▼
internal/sandbox ◄────── internal/tools
    │
    ▼
internal/config
```

**核心原则**：
- `internal/config` 是最底层，不依赖任何模块
- `internal/sandbox` 依赖 `config`
- `internal/tools` 依赖 `sandbox`
- `internal/middleware` 依赖 `tools` 和 `sandbox`
- `internal/agent` 依赖 `middleware` 和 `tools`
- `pkg/gateway` 依赖 `agent`

## 2.4　关键接口

### Middleware 接口

```go
type Middleware interface {
    BeforeAgent(ctx context.Context, state *State) error
    Before(ctx context.Context, state *State) error
    After(ctx context.Context, state *State, response *Response) error
    AfterAgent(ctx context.Context, state *State, response *Response) error
    WrapToolCall(ctx context.Context, state *State, toolCall *ToolCall, handler ToolHandler) (*ToolResult, error)
    Name() string
}
```

### Sandbox 接口

```go
type Sandbox interface {
    ID() string
    Execute(ctx context.Context, command string) (ExecuteResult, error)
    ReadFile(ctx context.Context, path string, startLine, endLine int) (string, error)
    WriteFile(ctx context.Context, path string, content string, append bool) error
    ListDir(ctx context.Context, path string, maxDepth int) ([]FileInfo, error)
    StrReplace(ctx context.Context, path string, oldStr, newStr string, replaceAll bool) error
    Glob(ctx context.Context, path, pattern string, includeDirs bool, maxResults int) ([]string, bool, error)
    Grep(ctx context.Context, path, pattern, glob string, literal, caseSensitive bool, maxResults int) ([]GrepMatch, bool, error)
    UpdateFile(ctx context.Context, path string, content []byte) error
}
```

### LeadAgent 接口

```go
type LeadAgent interface {
    Run(ctx context.Context, state *ThreadState, cfg RunConfig) (<-chan Event, error)
    Resume(ctx context.Context, state *ThreadState, cfg RunConfig, checkpointID string) (<-chan Event, error)
}
```

## 2.5　构建与运行

```bash
# 构建
go build -o goclaw ./cmd/goclaw

# 运行
./goclaw

# 或直接运行
go run ./cmd/goclaw
```

**环境变量**：
- `GOCRAW_CONFIG_PATH`: 配置文件路径（默认 `config.yaml`）

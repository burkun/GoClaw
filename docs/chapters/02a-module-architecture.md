# 第 2A 章　模块架构详解

本章从架构层面深入解析 GoClaw 各模块的设计思想、职责边界、交互模式和关键数据流。建议先阅读[第 2 章（项目结构）](/chapters/02-project-structure)建立全局视图，再通过本章理解模块间的深层关系。

---

## 2A.1　架构全景图

```
┌─────────────────────────────────────────────────────────────────────┐
│                          外部交互层                                   │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌───────────┐ │
│  │ REST API │ │ SSE Push │ │Telegram  │ │  Slack   │ │  飞书     │ │
│  │  (Gin)   │ │ (Stream) │ │ (Poll)   │ │ (Socket) │ │(Webhook) │ │
│  └────┬─────┘ └────┬─────┘ └────┬─────┘ └────┬─────┘ └─────┬─────┘ │
│       └─────────────┴─────────────┴─────────────┴─────────────┘     │
│                                 │                                    │
│                     pkg/gateway │ internal/channels                  │
│                                 │                                    │
├─────────────────────────────────┼────────────────────────────────────┤
│                          编排与控制层                          │      │
│                                 ▼                                    │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │                     Lead Agent (internal/agent)               │   │
│  │  ┌─────────────┐  ┌──────────────────┐  ┌─────────────────┐  │   │
│  │  │ Eino Runner │  │ Middleware Chain │  │ Tool Registry   │  │   │
│  │  │ (Agent 编排) │  │    (18 层流水线)  │  │  (工具注册/查找) │  │   │
│  │  └─────────────┘  └──────────────────┘  └─────────────────┘  │   │
│  │                              │                                 │   │
│  │  ┌──────────────────────────────────────────────────────────┐ │   │
│  │  │              Iteration Loop (迭代循环)                     │ │   │
│  │  │  BeforeModel → LLM Call → Tool Exec → AfterModel → ...   │ │   │
│  │  └──────────────────────────────────────────────────────────┘ │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                 │                                    │
├─────────────────────────────────┼────────────────────────────────────┤
│                          能力与服务层                                │
│         ┌───────────────────────┼───────────────────────┐            │
│         ▼                       ▼                       ▼            │
│  ┌─────────────┐  ┌─────────────────────┐  ┌─────────────────────┐  │
│  │  Sub-Agent  │  │   Middleware Stack  │  │    Skills System    │  │
│  │  Executor   │  │   (能力注入管道)     │  │   (插件化能力扩展)   │  │
│  │ (并行调度)   │  │                     │  │                     │  │
│  └──────┬──────┘  └──────────┬──────────┘  └──────────┬──────────┘  │
│         │                    │                        │              │
├─────────┼────────────────────┼────────────────────────┼──────────────┤
│         │             基础设施与抽象层                 │              │
│         ▼                    ▼                        ▼              │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │                      Tool System (工具系统)                    │   │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────────┐ │   │
│  │  │Sandbox   │ │   MCP    │ │ Built-in │ │   Web/Search     │ │   │
│  │  │Tools     │ │  Tools   │ │  Tools   │ │   Tools          │ │   │
│  │  │(FS/Shell)│ │(外部服务) │ │(task等)  │ │(web_search等)   │ │   │
│  │  └────┬─────┘ └────┬─────┘ └──────────┘ └──────────────────┘ │   │
│  └───────┼────────────┼─────────────────────────────────────────┘   │
│          │            │                                              │
│          ▼            ▼                                              │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │                  Sandbox Abstraction (沙箱抽象)                │   │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐ │   │
│  │  │ Local Sandbox│  │Docker Sandbox│  │ Kubernetes Sandbox   │ │   │
│  │  │ (进程内隔离)  │  │ (容器隔离)    │  │  (集群隔离)          │ │   │
│  │  └──────────────┘  └──────────────┘  └──────────────────────┘ │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────────────┐   │
│  │  Config  │ │  Models  │ │  Memory  │ │ Thread / Checkpoint  │   │
│  │  (配置)   │ │ (LLM工厂)│ │ (长记忆)  │ │   Store (持久化)     │   │
│  └──────────┘ └──────────┘ └──────────┘ └──────────────────────┘   │
└─────────────────────────────────────────────────────────────────────┘
```

### 分层职责

| 层 | 目录 | 职责 | 依赖方向 |
|---|------|------|---------|
| **外部交互** | `pkg/gateway`, `internal/channels` | HTTP API、SSE、IM 消息 | → 编排层 |
| **编排控制** | `internal/agent` | Agent 生命周期、迭代循环、工具调度 | → 能力层 |
| **能力服务** | `internal/middleware`, `internal/subagents`, `internal/skills` | 横切关注点、子代理调度、插件扩展 | → 基础层 |
| **基础设施** | `internal/sandbox`, `internal/tools`, `internal/models`, `internal/config`, `internal/threadstore` | 底层抽象与实现 | 无上层依赖 |

---

## 2A.2　Lead Agent 核心引擎

### 设计目标

Lead Agent 是整个系统的核心编排器，负责：

1. **接收请求** — 从 Gateway 或 IM 渠道获取用户输入
2. **初始化状态** — 加载对话历史、配置文件、沙箱环境
3. **执行迭代** — 通过中间件流水线反复调用 LLM + 工具
4. **流式输出** — 通过 SSE 将增量结果推送给客户端
5. **状态持久化** — 保存 checkpoint 和线程元数据

### 生命周期

```
New(ctx) / NewWithName(ctx, agentName)
  │
  ├─ 1. Load AppConfig
  ├─ 2. Create ChatModel (LLM)
  ├─ 3. Register Default Tools (bootstrap)
  ├─ 4. Build Middleware Chain
  ├─ 5. Create Sandbox Provider
  ├─ 6. Build Eino ChatModelAgent
  └─ 7. Return *leadAgent

Run(ctx, state, cfg)
  │
  ├─ 1. Build session values (thread_id, plan_mode, scene, ...)
  ├─ 2. Create buffered event channel (cap=32)
  ├─ 3. Call runner.Run(ctx, messages, opts...)
  └─ 4. Spawn drainIter goroutine → SSE events

Resume(ctx, state, cfg, checkpointID)
  │
  └─ Similar to Run but starts from checkpoint state
```

### 关键数据结构

```go
// ThreadState 是贯穿整个 Agent 运行的状态容器
type ThreadState struct {
    Messages      []*schema.Message  // 对话历史
    Sandbox       *SandboxState      // 沙箱绑定
    ThreadData    *ThreadDataState   // 线程目录路径
    UploadedFiles []string           // 上传文件列表
    ViewedImages  map[string]ViewedImage // 已查看图片
}

// RunConfig 控制单次运行的行为
type RunConfig struct {
    ThreadID              string
    RunID                 string
    ModelName             string
    AgentName             string
    IsPlanMode            bool
    SubagentEnabled       bool
    MaxConcurrentSubagents int
    ThinkingEnabled       bool
    CheckpointID          string
    Scene                 string   // 场景 ID（论文阅读/儿童教育等）
}
```

---

## 2A.3　中间件系统

### 设计模式

GoClaw 的中间件系统采用 **"洋葱模型"** 设计，每一层中间件可以在 Agent 生命周期的 5 个钩子点介入：

```
         Request
            │
    ┌───────▼────────┐  BeforeAgent:  thread_data → uploads → sandbox
    │  BeforeAgent   │  (顺序执行)
    └───────┬────────┘
            │
    ┌───────▼────────┐
    │ Iteration Loop │
    │                │
    │ ┌────────────┐ │  BeforeModel: memory → view_image → summarize
    │ │BeforeModel │ │  (顺序执行)
    │ └─────┬──────┘ │
    │       ▼        │
    │ ┌────────────┐ │  LLM Call
    │ │ Model Call │ │
    │ └─────┬──────┘ │
    │       ▼        │
    │ ┌────────────┐ │  Tool Execution → WrapToolCall (逆序)
    │ │Tool Execute│ │  guardrail → tool_error → clarification
    │ └─────┬──────┘ │
    │       ▼        │
    │ ┌────────────┐ │  AfterModel: title → loop_detection → token_usage
    │ │ AfterModel │ │  (逆序执行)
    │ └─────┬──────┘ │
    │       │        │
    │   (repeat)     │
    └───────┬────────┘
            │
    ┌───────▼────────┐  AfterAgent: memory_update → sandbox_release
    │  AfterAgent    │  (逆序执行)
    └───────┬────────┘
            │
         Response
```

### 完整中间件列表与执行顺序

| # | 中间件 | BeforeAgent | BeforeModel | WrapToolCall | AfterModel | AfterAgent |
|---|--------|:-----------:|:-----------:|:------------:|:----------:|:----------:|
| 1 | **ThreadData** | ✅ | - | - | - | - |
| 2 | **Uploads** | ✅ | - | - | - | - |
| 3 | **Sandbox** | ✅ | - | - | - | ✅ |
| 4 | **DanglingToolCall** | - | ✅ | - | - | - |
| 5 | **Guardrail** | - | - | ✅ | - | - |
| 6 | **Audit** | ✅ | ✅ | ✅ | ✅ | ✅ |
| 7 | **Summarization** | - | ✅ | - | - | - |
| 8 | **Todo** | ✅ | - | - | ✅ | - |
| 9 | **Title** | - | - | - | ✅ | - |
| 10 | **Memory** | - | ✅ | - | ✅ | - |
| 11 | **ViewImage** | - | ✅ | - | - | - |
| 12 | **SubagentLimit** | - | - | - | ✅ | - |
| 13 | **LoopDetection** | - | - | - | ✅ | - |
| 14 | **DeferredToolFilter** | - | ✅ | - | - | - |
| 15 | **TokenUsage** | - | - | - | ✅ | - |
| 16 | **LLMError** | - | - | - | ✅ | - |
| 17 | **ToolError** | - | - | ✅ | - | - |
| 18 | **Clarification** | - | - | ✅ | - | - |

### 状态传递机制

中间件之间通过 `State` 结构体共享数据：

```go
type State struct {
    ThreadID     string
    Messages     []map[string]any  // 对话消息（可被中间件修改）
    PlanMode     bool
    Title        string            // 自动生成的标题
    Todos        []map[string]any  // 计划模式的任务列表
    MemoryFacts  []string          // 注入的记忆事实
    ViewedImages map[string]ViewedImage
    Extra        map[string]any    // 扩展字段（场景、任务计数等）
}
```

**关键约定**：
- `State.Extra` 是中间件间通信的主要通道（如 `scene`、`task_tool_calls_count`）
- 对话内容修改通过 `State.Messages` 中 map 的直接修改完成
- Reducer 函数用于合并并发更新（如 `MergeArtifacts`、`MergeViewedImages`）

---

## 2A.4　工具系统

### 工具注册流程

```
extensions_config.json
        │
        ▼
┌───────────────────┐
│ MCP Discovery     │  1. 解析 MCP 服务器配置
│ (mcp_discover.go) │  2. 调用 tools/list 发现工具
└────────┬──────────┘  3. 缓存工具 schema（基于 mtime 失效）
         │
         ▼
┌───────────────────┐
│ Tool Bootstrap    │  1. 注册内置沙箱工具 (bash, read_file, ...)
│ (bootstrap/)      │  2. 注册 Web 工具 (search, fetch, ...)
└────────┬──────────┘  3. 注册 Built-in 工具 (clarification, tool_search, setup_agent)
         │
         ▼
┌───────────────────┐
│ Agent Builder     │  1. 合并默认工具 + MCP 工具
│ (builder.go)      │  2. 添加 Sub-Agent Task Tool
└────────┬──────────┘  3. 应用 Skills 白名单过滤
         │             4. 应用 Tool Groups 过滤
         ▼
┌───────────────────┐
│ Eino Tool Adapter │  转换为 Eino 框架兼容的工具格式
│ (AdaptToEinoTool) │
└────────┬──────────┘
         │
         ▼
    Agent Tools
```

### Tool 接口

```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() json.RawMessage
    Execute(ctx context.Context, input string) (string, error)
}
```

### 工具分类

| 类别 | 位置 | 示例 | 生命周期 |
|------|------|------|---------|
| **Sandbox Tools** | `internal/tools/fs/`, `shell/` | `read_file`, `write_file`, `bash` | 每次请求通过 bootstrap 创建运行时包装器 |
| **Web Tools** | `internal/tools/web/` | `web_search`, `web_fetch`, `image_search` | 通过 bootstrap 注册 |
| **Built-in Tools** | `internal/tools/builtin/` | `ask_clarification`, `tool_search`, `setup_agent` | 通过 bootstrap 注册 |
| **Sub-Agent Tool** | `internal/agent/subagents/` | `task` | 在 builder 中直接创建，不经过默认注册表 |
| **MCP Tools** | `internal/tools/mcp_*.go` | 外部服务工具 | 启动时动态发现，schema 缓存 |

### 运行时工具调用流程

```
LLM Response: tool_call(name="read_file", args={"path":"/mnt/..."})
  │
  ▼
┌──────────────────────┐
│ WrapToolCall Chain   │  中间件逆序包装：clarification → tool_error → guardrail
│ (洋葱模型)            │
└──────────┬───────────┘
           │
           ▼
┌──────────────────────┐
│ Tool.Execute(ctx,    │
│   json_arguments)    │
└──────────┬───────────┘
           │
           ▼
┌──────────────────────┐
│ Virtual Path → Host  │  路径转换 + 文件锁 + 沙箱执行
│ Path Resolution      │
└──────────┬───────────┘
           │
           ▼
    Tool Result → StateUpdates → AfterModel hooks
```

---

## 2A.5　沙箱系统

### 三层隔离模型

GoClaw 的沙箱系统提供三层隔离，从轻到重：

```
┌──────────────────────────────────────────────────────┐
│ Layer 1: Virtual Path System                         │
│ • Agent 只能看到 /mnt/user-data/* 和 /mnt/skills/*   │
│ • 所有宿主机路径被虚拟化映射                            │
│ • 文件操作通过虚拟路径接口进行                          │
├──────────────────────────────────────────────────────┤
│ Layer 2: Command Sandbox (Local Mode)                 │
│ • 命令白名单 / 黑名单过滤                               │
│ • 输出截断 (防 token 爆炸)                              │
│ • 超时控制 + Context 取消                              │
│ • 文件操作锁 (进程内 + 跨进程 flock)                    │
├──────────────────────────────────────────────────────┤
│ Layer 3: Container Isolation (Docker / K8s Mode)      │
│ • 完整容器隔离                                         │
│ • CPU / Memory / Network 资源限制                      │
│ • 卷挂载 (只读/读写)                                   │
│ • Warm Pool 预热 + 空闲超时逐出                         │
│ • 跨进程确定性 Sandbox ID                              │
└──────────────────────────────────────────────────────┘
```

### Sandbox Provider 模式

```go
type SandboxProvider interface {
    Acquire(ctx context.Context, threadID string) (string, error)
    Release(ctx context.Context, sandboxID string) error
    Get(sandboxID string) Sandbox
    Shutdown(ctx context.Context) error
}
```

- **Local Provider**: 每个线程获得独立的工作目录（`.goclaw/threads/{id}/user-data/`）
- **Docker Provider**: 每个线程绑定一个 Docker 容器，支持 warm pool 预热
- **Kubernetes Provider**: 每个线程绑定一个 Pod

### Warm Pool 机制 (Docker)

```
Acquire Request
  │
  ├─ 1. Check in-process cache (threadSandboxes)
  ├─ 2. Check warm pool → 命中则直接复用（零冷启动）
  ├─ 3. Cross-process file lock (确定性 sandbox ID)
  └─ 4. Create new container

Release Request
  │
  ├─ 1. Remove from active pool
  ├─ 2. Add to warm pool (记录加入时间)
  └─ 3. Watchdog 后台线程: 空闲 > idle_timeout → evict
```

---

## 2A.6　Sub-Agent 执行引擎

### 架构

```
Lead Agent
  │
  │ tool_call(name="task", args={"subagent_type":"bash","prompt":"..."})
  ▼
┌────────────────────────────────────────────┐
│ TaskTool (internal/agent/subagents/)       │
│                                             │
│  1. Validate request                       │
│  2. Create TaskResult (pending)            │
│  3. Submit to Executor                     │
│  4. Return task_id to Lead Agent           │
└────────────────┬───────────────────────────┘
                 │
                 ▼
┌────────────────────────────────────────────┐
│ Executor (信号量并发控制)                    │
│                                             │
│  ┌─────────────────────────────────────┐   │
│  │ Semaphore (max_concurrent_subagents)│   │
│  └────────┬────────────────────────────┘   │
│           │                                 │
│  ┌────────▼────────────────────────────┐   │
│  │ runTask goroutine                    │   │
│  │  ├─ Acquire semaphore slot          │   │
│  │  ├─ Create child agent instance     │   │
│  │  ├─ Execute worker function         │   │
│  │  └─ Release slot + finish task      │   │
│  └─────────────────────────────────────┘   │
│                                             │
│  State Machine:                             │
│  pending → queued → in_progress             │
│                   ├─ completed              │
│                   ├─ failed                 │
│                   └─ timed_out              │
└────────────────────────────────────────────┘
```

### Worker 抽象

```go
type WorkerFunc func(ctx context.Context, req TaskRequest) (string, error)
type WorkerFuncWithMessages func(ctx context.Context, req TaskRequest) (TaskResult, error)
```

Worker 由 builder 注入，在实际场景中它会：
1. 创建新的 Agent 实例（可能使用不同模型或系统提示）
2. 在新会话中执行被委托的任务
3. 返回最终结果和 AI 消息

---

## 2A.7　MCP (Model Context Protocol) 集成

### 传输方式支持

```
┌─────────────────────────────────────────────────────┐
│              MCP Transport Layer                     │
├───────────────┬───────────────┬─────────────────────┤
│    stdio      │     sse       │        http         │
│ (子进程通信)   │ (Server-Sent  │   (REST + SSE)      │
│               │   Events)     │                     │
├───────────────┼───────────────┼─────────────────────┤
│ • 进程池管理   │ • SSE 连接池   │ • HTTP POST + SSE   │
│ • 自动重启     │ • 端点发现     │ • OAuth 认证        │
│ • 框架检测     │ • 自动重连     │ • Header 传递       │
│ (LF/CRLF)    │ • 流体重连     │ • Timeout 控制      │
└───────────────┴───────────────┴─────────────────────┘
```

### 工具发现与缓存

```
Startup
  │
  ├─ 1. Read extensions_config.json
  ├─ 2. For each MCP server:
  │     ├─ Calculate config signature (SHA256)
  │     ├─ Check cache (mtime-based invalidation)
  │     ├─ If miss: connect → initialize → tools/list
  │     └─ Cache tool schemas
  └─ 3. Register tools into default registry

Runtime (per request)
  │
  ├─ Check config mtime → invalidate if changed
  ├─ tools/call → MCP server → parse result
  └─ Return as tool output
```

### 认证支持

```json
{
  "mcpServers": {
    "my-api": {
      "type": "http",
      "url": "https://api.example.com/mcp",
      "oauth": {
        "token_url": "https://auth.example.com/oauth/token",
        "grant_type": "client_credentials",
        "client_id": "$MY_CLIENT_ID",
        "client_secret": "$MY_CLIENT_SECRET"
      }
    }
  }
}
```

支持环境变量自动展开（`$VAR_NAME` 语法）。

---

## 2A.8　记忆系统

### 两层存储架构

```
┌─────────────────────────────────────────────┐
│           MemoryMiddleware                   │
├─────────────────────────────────────────────┤
│                                             │
│  BeforeModel:                               │
│  ┌─────────────────────────────────────┐    │
│  │ 1. Load facts from JSONFileStore    │    │
│  │ 2. Select top-15 by confidence      │    │
│  │ 3. Format as <memory> XML block     │    │
│  │ 4. Prepend to system message        │    │
│  └─────────────────────────────────────┘    │
│                                             │
│  AfterModel:                                │
│  ┌─────────────────────────────────────┐    │
│  │ 1. Filter conversation messages     │    │
│  │ 2. Queue for async LLM extraction   │    │
│  └─────────────────────────────────────┘    │
│                                             │
│  AfterAgent:                                │
│  ┌─────────────────────────────────────┐    │
│  │ Trigger UpdateQueue.process()       │    │
│  │ (30s debounce, per-thread queue)    │    │
│  └─────────────────────────────────────┘    │
│                                             │
├─────────────────────────────────────────────┤
│           UpdateQueue (异步管道)              │
│                                             │
│  entries[threadID] → time.AfterFunc(30s)    │
│       │                                      │
│       ▼                                      │
│  LLMMemoryUpdater:                           │
│  ┌─────────────────────────────────────┐    │
│  │ 1. Build extraction prompt          │    │
│  │ 2. Call LLM → structured JSON       │    │
│  │ 3. Merge/update facts               │    │
│  │ 4. Save via JSONFileStore            │    │
│  └─────────────────────────────────────┘    │
│                                             │
└─────────────────────────────────────────────┘
```

### 事实数据模型

```json
{
  "version": "1.0",
  "lastUpdated": "2026-07-14T10:00:00Z",
  "facts": [
    {
      "id": "fact_1720953600_1",
      "category": "preference",
      "content": "用户偏好使用中文回答",
      "confidence": 0.95,
      "createdAt": "2026-07-14T10:00:00Z",
      "lastAccessedAt": "2026-07-14T10:00:00Z"
    }
  ],
  "userContext": "用户是一名 Go 后端开发者",
  "historySummary": "最近讨论了 GoClaw 项目的审计工作"
}
```

### 关键设计决策

- **多队列支持**: 每个 `memoryPath` 一个独立的 `UpdateQueue`，支持多 Agent 独立记忆
- **防抖机制**: 30 秒防抖，避免频繁对话触发过多 LLM 提取调用
- **复合注入防护**: `BeforeModel` 检测系统消息是否已包含记忆块，避免每次迭代重复添加前缀
- **置信度阈值**: 默认 0.7，低于阈值的事实不会被注入到提示中

---

## 2A.9　Gateway 与通信层

### HTTP API 路由设计

```
POST   /api/threads/:id/runs         创建运行 (SSE 流式响应)
POST   /api/threads/:id/runs/stream  LangGraph 兼容 SSE 端点
POST   /api/threads/:id/uploads      上传文件
GET    /api/threads/:id/artifacts/*  获取产物文件
POST   /api/threads/:id/cancel       取消运行
GET    /api/threads                   列出线程
DELETE /api/threads/:id               删除线程
GET    /api/models                    列出模型
GET    /api/agents                    列出 Agent
GET    /api/memory                    获取记忆
GET    /api/skills                    列出 Skills
GET    /api/mcp/config                MCP 配置
PUT    /api/mcp/config                更新 MCP 配置
GET    /health                        健康检查
```

### SSE 事件协议

每个流都以 `completed` 或 `error` 事件终止：

```json
{
  "type": "message_delta",
  "thread_id": "thread-001",
  "payload": {
    "content": "Hello!",
    "is_thinking": false
  },
  "timestamp": 1720953600000
}
```

| 事件 | 触发时机 |
|------|---------|
| `message_delta` | 增量文本（含 `is_thinking` 标记区分思考/输出） |
| `tool_event` | 两阶段：`call`（含输入）→ `result`（含输出） |
| `task_started/running/completed/failed/timed_out` | 子代理状态变更 |
| `state_snapshot` / `state_delta` | AG-UI 协议，工具输出的 UI 状态 |
| `title` | 自动生成的对话标题 |
| `completed` | 正常终止（含 `final_message` 和 `title`） |
| `error` | 异常终止（含 `code` 和 `message`） |

### IM 渠道架构

```
┌──────────────────────────────────────────┐
│            ChannelManager                 │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ │
│  │Telegram  │ │  Slack   │ │  飞书    │ │
│  │(长轮询)   │ │(Socket)  │ │(Webhook) │ │
│  └────┬─────┘ └────┬─────┘ └────┬─────┘ │
│       └─────────────┴─────────────┘       │
│                    │                      │
│            MessageBus (pub/sub)           │
│                    │                      │
│         ┌──────────▼──────────┐          │
│         │   Agent Dispatcher  │          │
│         └─────────────────────┘          │
└──────────────────────────────────────────┘
```

**线程映射**: `{channel}:{chatID}` 或 `{channel}:{chatID}:{topicID}`

**支持命令**: `/new`, `/status`, `/models`, `/memory`, `/help`

---

## 2A.10　关键数据流

### 完整请求处理流程

```
1. Client Request (HTTP / IM)
      │
2. Gateway / Channel Manager
      ├─ Parse request / message
      ├─ Resolve thread ID
      ├─ Load thread state from ThreadStore
      └─ Call LeadAgent.Run()
            │
3. BeforeAgent Hooks (顺序)
      ├─ ThreadData: 创建线程目录
      ├─ Uploads: 注入上传文件信息
      └─ Sandbox: 获取沙箱实例
            │
4. Iteration Loop:
      ├─ BeforeModel Hooks (顺序)
      │   ├─ Dangling: 修复被中断的工具调用
      │   ├─ Summarization: 上下文压缩
      │   ├─ Memory: 注入长期记忆
      │   └─ ViewImage: 注入图片数据
      │
      ├─ Model Call (LLM)
      │
      ├─ Tool Execution (if tool_call)
      │   ├─ WrapToolCall Hooks (逆序)
      │   │   ├─ Clarification: 拦截澄清请求
      │   │   ├─ ToolError: 错误重试
      │   │   └─ Guardrail: 策略检查
      │   └─ Tool.Execute()
      │
      ├─ AfterModel Hooks (逆序)
      │   ├─ Todo: 更新任务列表
      │   ├─ Title: 生成标题
      │   ├─ Memory: 排队异步更新
      │   ├─ LoopDetection: 死循环检测
      │   └─ TokenUsage: 统计令牌用量
      │
      └─ Repeat until no tool calls or max iterations
            │
5. AfterAgent Hooks (逆序)
      ├─ Memory: 触发异步事实提取
      └─ Sandbox: 释放沙箱
            │
6. SSE Event Stream → Client
      └─ completed / error (保证终端事件)
```

### 事件流保证

- **Exactly-Once Terminal**: 每个流严格保证一个 `completed` 或 `error` 事件
- **Panic Recovery**: `drainIter` 使用 `recover()` + 非阻塞 channel send
- **Heartbeat**: SSE 流每 15 秒发送心跳，防止代理/负载均衡器超时
- **Buffered Channel**: 事件通道容量 32，避免慢消费者阻塞 Agent

---

## 2A.11　错误处理策略

### 错误码体系

| 前缀 | 类别 | 示例 |
|------|------|------|
| `agent/` | Agent 运行时错误 | `agent/not_initialized`, `agent/run_failed` |
| `validation/` | 输入验证错误 | `validation/missing_thread_id` |
| `auth/` | 认证错误 | `auth/invalid_api_key` |
| `sandbox/` | 沙箱执行错误 | `sandbox/exec_failed`, `sandbox/timeout` |
| `tool/` | 工具执行错误 | `tool/not_found`, `tool/exec_failed` |
| `model/` | 模型 API 错误 | `model/api_error`, `model/rate_limited` |

### 重试与降级策略

| 组件 | 重试策略 | 降级行为 |
|------|---------|---------|
| **LLM 调用** | 指数退避，最多 3 次 | 返回错误事件（非 nil error） |
| **工具调用** | ToolError 中间件包装重试 | 向 LLM 返回错误消息 |
| **MCP 连接** | stdio 自动重启，SSE 重连 | 返回连接错误 |
| **Docker 沙箱** | 文件锁 + 确定性 ID 防竞态 | 回退到 local 沙箱 |
| **记忆持久化** | 原子写入 (tmp + rename) | 记录警告日志，不阻塞 Agent |

---

## 2A.12　并发模型

### Goroutine 生命周期

```
Main Process
  ├─ Gateway Server (Gin, per-request goroutines)
  │   └─ SSE Handler → eventChan consumer
  │
  ├─ Lead Agent (per-run goroutine)
  │   └─ drainIter → eventChan producer
  │
  ├─ Sub-Agent Executor
  │   ├─ Semaphore (限制并发数)
  │   └─ Per-task goroutines (runTask)
  │
  ├─ Background Workers
  │   ├─ Memory UpdateQueue timer (30s debounce)
  │   ├─ Config Watcher (2s poll interval)
  │   ├─ MCP Cache Auto-Refresh (60s interval)
  │   ├─ Task Registry Cleanup (60s interval)
  │   ├─ Docker Watchdog (idle timeout eviction)
  │   └─ Channel Pollers (Telegram/Slack/Feishu)
  │
  └─ Shutdown
      ├─ Signal handling (SIGINT/SIGTERM)
      ├─ Stop background workers
      ├─ Drain active requests
      ├─ Release sandboxes
      └─ Wait for goroutines (WaitGroup)
```

### 关键并发原语

| 机制 | 使用场景 |
|------|---------|
| `sync.Mutex` / `sync.RWMutex` | 工具注册表、线程存储、配置缓存、沙箱提供者 |
| `sync.WaitGroup` | 后台 worker 生命周期、消息总线发布 |
| `sync.Once` | 单例初始化、stop channel 关闭 |
| `sync/atomic` | 线程索引统计、缓存逐出计数 |
| Channel (带缓冲) | SSE 事件流、任务池 work queue |
| Semaphore (channel) | 子代理并发限制 |
| `context.Context` | 超时、取消传播、请求范围值传递 |

---

## 2A.13　扩展点

GoClaw 设计了以下明确的扩展点：

1. **新中间件**: 实现 `Middleware` 接口，在 `builder.go` 中注册
2. **新沙箱后端**: 实现 `Sandbox` + `SandboxProvider` 接口
3. **新工具**: 实现 `Tool` 接口，通过 bootstrap 或 MCP 注册
4. **新 LLM 后端**: 实现 `model.BaseChatModel`，在 `models/factory.go` 注册
5. **新 IM 渠道**: 实现 `Channel` 接口，在 `manager.go` 中注册
6. **Skills 插件**: 通过 `SKILL.md` 定义，动态加载到 Skills Registry
7. **Scene 场景**: 在 `config.yaml` 中定义，通过 `SceneMiddleware` 注入

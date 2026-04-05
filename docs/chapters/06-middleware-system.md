# 第 6 章　中间件系统

## 6.1　设计理念

中间件是 Agent 执行流水线的核心扩展点。每个中间件可以在特定阶段注入自定义逻辑，形成洋葱模型：

```
请求 ──► BeforeAgent ──► Before ──► 模型调用 ──► After ──► AfterAgent ──► 响应
           │              │                          │            │
           │              │                          │            │
           └──────────────┴──────────────────────────┴────────────┘
                              每个中间件可以拦截和修改
```

## 6.2　钩子类型

| 钩子 | 执行时机 | 执行次数 | 顺序 | 用途 |
|------|---------|---------|------|------|
| BeforeAgent | Agent 运行开始 | 1次 | 顺序 | 创建目录、获取资源 |
| Before | 模型调用前 | 多次 | 顺序 | 注入记忆、权限检查 |
| After | 模型调用后 | 多次 | 逆序 | 排队更新、记录日志 |
| AfterAgent | Agent 运行结束 | 1次 | 逆序 | 释放资源、触发任务 |
| WrapToolCall | 工具执行时 | 每次调用 | 顺序 | 拦截、审计、重试 |

## 6.3　State 结构

中间件之间共享的可变状态：

| 字段 | 说明 |
|------|------|
| ThreadID | 线程唯一标识 |
| Messages | 当前消息历史 |
| Title | 自动标题 |
| MemoryFacts | 注入的记忆事实 |
| Todos | 计划任务列表 |
| PlanMode | 是否计划模式 |
| TokenCount | 近似 token 数 |
| ViewedImages | 已查看图片 |
| Artifacts | 输出文件列表 |
| Extra | 扩展元数据 |

## 6.4　Reducer 机制

当多个源同时更新同一状态字段时，Reducer 负责合并：

### MergeArtifacts

```
现有: ["file1.md", "file2.md"]
新增: ["file2.md", "file3.md"]
结果: ["file1.md", "file2.md", "file3.md"]  // 去重，保留顺序
```

### MergeViewedImages

```
现有: {"img1.png": {...}}
新增: {"img2.png": {...}}
结果: {"img1.png": {...}, "img2.png": {...}}

特殊情况：新增为空 map → 清空所有
```

## 6.5　中间件详解

### ThreadDataMiddleware

**职责**：创建线程目录结构

**钩子**：BeforeAgent

**逻辑**：
1. 检查 `.goclaw/threads/{thread_id}/` 是否存在
2. 创建 `workspace/`、`uploads/`、`outputs/` 子目录
3. 将路径存入 State.Extra 供后续使用

### SandboxMiddleware

**职责**：管理沙箱生命周期

**钩子**：BeforeAgent / AfterAgent

**逻辑**：
- BeforeAgent：调用 Provider.Acquire() 获取沙箱，存入 State.Extra
- AfterAgent：调用 Provider.Release() 释放沙箱

### MemoryMiddleware

**职责**：长期记忆的注入和更新

**钩子**：Before / After

**逻辑**：
- Before：
  1. 从存储加载记忆
  2. 获取 Top 15 事实
  3. 格式化并注入到系统提示词
- After：
  1. 过滤消息（只保留用户输入和最终响应）
  2. 排队异步更新

### TitleMiddleware

**职责**：自动生成对话标题

**钩子**：After

**逻辑**：
1. 检查是否已有标题
2. 检查是否首次完整交互（有用户消息和助手响应）
3. 调用 LLM 生成简短标题
4. 截断到最大字符数

### TodoMiddleware

**职责**：计划模式支持

**钩子**：BeforeAgent / After

**逻辑**：
- BeforeAgent：如果是计划模式，注册 `write_todos` 工具
- After：合并 todos 状态更新到 State

### GuardrailMiddleware

**职责**：工具调用守卫

**钩子**：WrapToolCall

**决策结果**：
- Allow：继续执行
- Deny：返回错误，阻止执行
- Ask：需要用户确认（当前实现为拒绝）

### ClarificationMiddleware

**职责**：拦截澄清请求

**钩子**：WrapToolCall

**逻辑**：
1. 检测工具名是否为 `ask_clarification`
2. 如果是，设置标记 `clarification_requested`
3. 返回特殊结果，不实际执行工具

### DanglingToolCallMiddleware

**职责**：处理中断导致的悬空工具调用

**钩子**：Before

**场景**：模型请求调用工具，但用户中断了对话

**逻辑**：
1. 查找最后一条 AI 消息的 tool_calls
2. 检查是否有对应的 tool 响应消息
3. 为未响应的调用添加占位 ToolMessage

### SubagentLimitMiddleware

**职责**：限制并发子代理数量

**钩子**：After

**逻辑**：
1. 统计 AI 响应中的 `task` 工具调用数量
2. 如果超过限制，裁剪多余的调用
3. 确保不会同时启动过多子代理

### LoopDetectionMiddleware

**职责**：检测无限循环

**钩子**：After

**检测条件**：
- 相同工具连续调用超过 N 次
- 相同参数重复调用
- 无进展的循环

### LLMErrorMiddleware

**职责**：LLM 调用错误处理

**钩子**：After

**逻辑**：
- 检测 LLM 返回的错误
- 根据错误类型决定是否重试
- 配置最大重试次数

### ToolErrorMiddleware

**职责**：工具执行错误处理

**钩子**：WrapToolCall

**逻辑**：
- 捕获工具执行错误
- 格式化错误信息返回给模型
- 允许模型根据错误调整策略

## 6.6　中间件执行流

```
BeforeAgent (顺序)
    │
    ├── ThreadData: 创建目录
    ├── Uploads: 注入文件列表
    ├── Sandbox: 获取沙箱
    └── Guardrail: 初始化策略
    │
    ▼
迭代循环
    │
    ├── Before (顺序)
    │   ├── Dangling: 修复悬空调用
    │   ├── Memory: 注入记忆
    │   └── ViewImage: 注入图片
    │
    ├── 模型调用
    │
    ├── 工具执行 (WrapToolCall 包装)
    │   ├── Guardrail: 检查权限
    │   ├── Clarification: 拦截澄清
    │   └── ToolError: 错误处理
    │
    └── After (逆序)
        ├── Title: 生成标题
        ├── Memory: 排队更新
        └── Todo: 更新任务
    │
    ▼
AfterAgent (逆序)
    │
    ├── Memory: 触发异步更新
    └── Sandbox: 释放沙箱
```

## 6.7　编写自定义中间件

### 设计原则

1. **单一职责**：每个中间件只做一件事
2. **幂等性**：重复执行不应产生副作用
3. **错误隔离**：非关键错误应记录日志而非返回
4. **顺序感知**：了解自己在链中的位置和依赖

### 需要实现的接口

```
Name() string                    // 返回中间件名称
BeforeAgent(ctx, state) error    // Agent 开始前
Before(ctx, state) error         // 模型调用前
After(ctx, state, response) error // 模型调用后
AfterAgent(ctx, state, response) error // Agent 结束后
WrapToolCall(ctx, state, toolCall, handler) (result, error) // 工具包装
```

### 注册方式

在 `buildMiddlewares()` 函数中按正确位置插入。

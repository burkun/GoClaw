# 第 8 章　Sub-Agent 架构总览

## 8.1　为什么需要 Sub-Agent

复杂任务往往超出单个 Agent 的能力：

| 挑战 | 单 Agent | 多 Agent |
|------|---------|---------|
| 并行处理 | 串行执行 | 并行执行 |
| 专业分工 | 通用能力 | 专门优化 |
| 上下文隔离 | 共享上下文 | 独立上下文 |
| 错误隔离 | 牵一发动全身 | 独立失败 |

## 8.2　架构设计

```
Lead Agent
    │
    │ 决定委托任务
    ▼
Task Tool
    │
    │ 创建 TaskRequest
    ▼
Executor
    │
    │ 并发调度
    ├───► Worker 1 ──► Sub-Agent Instance 1
    ├───► Worker 2 ──► Sub-Agent Instance 2
    └───► Worker 3 ──► Sub-Agent Instance 3
            │
            │ 执行完成
            ▼
    TaskResult
    │
    │ 事件回调
    ▼
Lead Agent 继续
```

## 8.3　核心概念

### TaskRequest

任务请求的完整描述：

| 字段 | 说明 |
|------|------|
| Description | 任务简述 |
| Prompt | 具体指令 |
| SubagentType | 子代理类型 |
| MaxTurns | 最大轮次 |
| Timeout | 超时时间 |
| ModelName | 指定模型 |
| AllowedTools | 允许的工具 |
| ThreadID | 父线程 ID |
| WorkspacePath | 工作目录 |

### TaskResult

任务执行结果：

| 字段 | 说明 |
|------|------|
| TaskID | 任务唯一 ID |
| Status | 状态（pending/running/completed/failed/timed_out） |
| Output | 输出内容 |
| Error | 错误信息 |
| AIMessages | AI 消息历史 |

### Status 状态机

```
pending ──► queued ──► in_progress ──► completed
                            │
                            ├──► failed
                            │
                            └──► timed_out
```

## 8.4　内置 Sub-Agent

### general-purpose

**特点**：拥有除 task 外的所有工具

**适用场景**：
- 通用任务
- 需要文件操作
- 需要网络访问

**工具集**：bash, ls, read_file, write_file, web_search, web_fetch, ...

### bash

**特点**：只有 bash 工具

**适用场景**：
- 命令执行
- 脚本运行
- 系统操作

**系统提示词**：专注于高效安全的命令执行

## 8.5　执行流程

```
Lead Agent 决定委托
    │
    │ 调用 task 工具
    ▼
Task Tool 执行
    │
    ├── 1. 解析参数
    │   ├── description
    │   ├── prompt
    │   └── subagent_type
    │
    ├── 2. 创建 TaskRequest
    │
    ├── 3. 提交到 Executor
    │   └── 返回 TaskResult（初始状态）
    │
    ├── 4. 注册事件回调
    │   └── 监听状态变化
    │
    ├── 5. 等待完成
    │   ├── 轮询状态
    │   └── 发送 SSE 事件
    │
    └── 6. 返回最终结果
        └── task_completed / task_failed
```

## 8.6　事件类型

| 事件 | 触发时机 |
|------|---------|
| task_started | 任务创建 |
| task_running | 开始执行 |
| task_completed | 成功完成 |
| task_failed | 执行失败 |
| task_timed_out | 执行超时 |

### 事件格式

```
{
  "type": "task_started",
  "thread_id": "thread-123",
  "payload": {
    "task_id": "task-001",
    "subject": "Research API",
    "status": "pending"
  },
  "timestamp": ...
}
```

## 8.7　并发控制

### 限制策略

```
配置：max_concurrent_subagents = 3

场景：模型请求 5 个任务
    │
    ├── 前 3 个立即执行
    │
    └── 后 2 个等待

SubagentLimitMiddleware 处理：
    │
    ├── 检测 task 工具调用数量
    │
    ├── 超过限制 → 裁剪多余调用
    │
    └── 保证不超过并发上限
```

### 资源隔离

每个子代理有独立的：
- 消息历史
- 工具实例
- 沙箱上下文
- 超时控制

## 8.8　与 DeerFlow 的对齐

| 概念 | DeerFlow | GoClaw |
|------|----------|--------|
| 任务工具 | task | task |
| 执行器 | SubagentExecutor | Executor |
| 状态 | 6 状态 | 6 状态 |
| 并发限制 | 3 | 可配置 |
| 超时 | 15 分钟 | 可配置 |
| 事件流 | SSE | SSE |

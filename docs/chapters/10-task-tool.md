# 第 10 章　Task Tool 与事件流

## 10.1　Task Tool 定位

Task Tool 是 Lead Agent 调用 Sub-Agent 的唯一入口，负责：
- 接收任务委托请求
- 创建任务并提交到 Executor
- 监控任务状态并发送事件
- 返回最终结果

## 10.2　工具参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| description | string | 是 | 任务简述 |
| prompt | string | 是 | 详细指令 |
| subagent_type | string | 否 | 子代理类型（默认 general-purpose） |
| max_turns | int | 否 | 最大轮次（默认 50） |

## 10.3　执行流程

```
Run(input)
    │
    ├── 1. 解析参数
    │
    ├── 2. 构建 TaskRequest
    │   ├── 从 State 获取线程信息
    │   └── 设置工作目录
    │
    ├── 3. 注册事件回调
    │   └── 回调函数发送 SSE 事件
    │
    ├── 4. 提交到 Executor
    │
    ├── 5. 等待完成
    │   ├── 轮询状态（500ms 间隔）
    │   └── 检查上下文取消
    │
    └── 6. 返回结果
```

## 10.4　事件流映射

### 状态 → 事件类型

| 状态 | 事件类型 |
|------|---------|
| pending | task_started |
| in_progress | task_running |
| completed | task_completed |
| failed | task_failed |
| timed_out | task_timed_out |

### 事件发送时机

```
Executor 状态变化
    │
    ├── pending → 发送 task_started
    │
    ├── in_progress → 发送 task_running
    │
    └── completed/failed/timed_out → 发送对应事件
```

## 10.5　事件回调函数

```
eventCallback(ctx, taskID, event)
    │
    ├── 构建事件结构
    │   ├── type: 事件类型
    │   ├── thread_id: 父线程 ID
    │   └── payload: 任务信息
    │
    └── 发送到 SSE 通道
        └── 非阻塞发送，满则丢弃
```

## 10.6　SubagentLimitMiddleware

**位置**：After 钩子

**职责**：裁剪超限的 task 工具调用

```
After(ctx, state, response)
    │
    ├── 统计 task 工具调用数量
    │
    ├── 检查是否超过限制
    │   └── max_concurrent_subagents
    │
    └── 裁剪多余调用
        └── 只保留前 N 个
```

## 10.7　完整交互示例

### 请求

```
Lead Agent 决定委托任务：
"I'll delegate this to a sub-agent"

调用 task 工具：
{
  "description": "Research API docs",
  "prompt": "Find authentication section",
  "subagent_type": "general-purpose",
  "max_turns": 20
}
```

### 事件流

```
1. task_started
   └── 任务创建，状态 pending

2. task_running
   └── 开始执行，状态 in_progress

3. task_completed
   └── 执行完成，包含输出结果
```

### 返回结果

```
Task Tool 返回：
"The authentication uses OAuth 2.0. 
Key endpoints: /auth/token, /auth/refresh..."
```

## 10.8　上下文传递

子代理继承父代理的上下文：

| 传递项 | 用途 |
|--------|------|
| ThreadID | 日志关联 |
| WorkspacePath | 文件访问 |
| UploadsPath | 上传文件 |
| OutputsPath | 输出文件 |

## 10.9　错误处理

### 任务失败

```
子代理执行出错
    │
    ├── 返回 task_failed 事件
    │
    └── Task Tool 返回错误信息
        └── Lead Agent 根据错误决定重试或调整
```

### 超时处理

```
任务超时
    │
    ├── Executor 设置状态为 timed_out
    │
    ├── 发送 task_timed_out 事件
    │
    └── Task Tool 返回超时错误
```

### 用户取消

```
用户取消请求
    │
    ├── ctx.Done() 触发
    │
    ├── Task Tool 调用 Executor.Cancel()
    │
    └── 返回取消错误
```

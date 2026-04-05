# 第 17 章　SSE 事件协议

## 17.1　协议格式

### SSE 基本格式

```
data: {"type":"message_delta",...}

```

- 每行以 `data: ` 开头
- JSON 后跟两个换行符
- 空行表示事件结束

### 事件结构

| 字段 | 类型 | 说明 |
|------|------|------|
| type | string | 事件类型 |
| thread_id | string | 线程 ID |
| payload | object | 事件数据 |
| timestamp | int64 | Unix 毫秒时间戳 |

## 17.2　事件类型

### 核心事件

| 类型 | 方向 | 说明 |
|------|------|------|
| message_delta | Server→Client | 增量文本 |
| tool_event | Server→Client | 工具调用 |
| completed | Server→Client | 成功完成 |
| error | Server→Client | 错误终止 |

### 任务事件

| 类型 | 说明 |
|------|------|
| task_started | 任务创建 |
| task_running | 任务运行 |
| task_completed | 任务完成 |
| task_failed | 任务失败 |
| task_timed_out | 任务超时 |

## 17.3　message_delta

**Payload**：

| 字段 | 说明 |
|------|------|
| content | 增量文本 |
| is_thinking | 是否为推理内容 |

**处理逻辑**：

```
客户端：
    │
    ├── is_thinking = true
    │   └── 可选：折叠显示
    │
    └── is_thinking = false
        └── 累积到最终消息
```

## 17.4　tool_event

### 两阶段协议

**阶段 1 - 调用**：

| 字段 | 值 |
|------|-----|
| call_id | 调用 ID |
| tool_name | 工具名称 |
| input | 参数 JSON |
| output | 空 |
| is_error | false |

**阶段 2 - 结果**：

| 字段 | 值 |
|------|-----|
| call_id | 同阶段 1 |
| tool_name | 同阶段 1 |
| input | 空 |
| output | 执行结果 |
| is_error | 是否错误 |

### 客户端处理

```
On tool_event (Phase 1):
    └── 创建工具调用条目，显示 "执行中..."

On tool_event (Phase 2):
    └── 更新条目，显示结果
```

## 17.5　completed

**Payload**：

| 字段 | 说明 |
|------|------|
| final_message | 最终消息文本 |

**语义**：运行成功结束，无后续事件。

## 17.6　error

**Payload**：

| 字段 | 说明 |
|------|------|
| code | 错误码 |
| message | 错误消息 |

**错误码**：

| Code | 含义 | 建议 |
|------|------|------|
| agent/run_error | 运行错误 | 重试 |
| agent/interrupted | 用户中断 | 确认 |
| agent/empty_stream | 流意外关闭 | 报告 bug |
| agent/context_cancelled | 上下文取消 | 重连恢复 |
| agent/not_initialized | 未初始化 | 重启服务 |

## 17.7　P0 契约

每个事件流**必须**以以下之一终止：
- `completed` - 成功
- `error` - 失败

客户端必须收到明确的结束信号。

## 17.8　客户端集成示例

```
EventSource('/api/threads/t1/runs')
    │
    ├── onmessage
    │   ├── message_delta → 累积文本
    │   ├── tool_event → 更新工具状态
    │   ├── completed → 完成
    │   └── error → 显示错误
    │
    └── onerror
        └── 连接关闭，提示重试
```

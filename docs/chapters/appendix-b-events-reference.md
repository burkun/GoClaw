# 附录 B：事件类型速查

## B.1　核心事件

| 类型 | 方向 | 说明 |
|------|------|------|
| `message_delta` | Server → Client | 增量文本输出 |
| `tool_event` | Server → Client | 工具调用事件 |
| `completed` | Server → Client | 成功完成 |
| `error` | Server → Client | 错误终止 |
| `end` | Server → Client | 流结束标记 |

## B.2　任务事件（计划模式）

| 类型 | 说明 |
|------|------|
| `task_started` | 任务创建 |
| `task_running` | 任务运行中 |
| `task_completed` | 任务完成 |
| `task_failed` | 任务失败 |
| `task_timed_out` | 任务超时 |

## B.3　事件字段

### message_delta

```json
{
  "type": "message_delta",
  "thread_id": "string",
  "payload": {
    "content": "string",
    "is_thinking": "boolean"
  },
  "timestamp": 1712145600123
}
```

### tool_event

```json
{
  "type": "tool_event",
  "thread_id": "string",
  "payload": {
    "call_id": "string",
    "tool_name": "string",
    "input": "string",
    "output": "string",
    "is_error": "boolean"
  },
  "timestamp": 1712145600123
}
```

### completed

```json
{
  "type": "completed",
  "thread_id": "string",
  "payload": {
    "final_message": "string"
  },
  "timestamp": 1712145600200
}
```

### error

```json
{
  "type": "error",
  "thread_id": "string",
  "payload": {
    "code": "string",
    "message": "string"
  },
  "timestamp": 1712145600200
}
```

## B.4　错误码

| Code | 含义 | 建议处理 |
|------|------|---------|
| `agent/run_error` | Agent 运行错误 | 重试 |
| `agent/interrupted` | 用户中断 | 确认取消 |
| `agent/empty_stream` | 流意外关闭 | 重试或报告 bug |
| `agent/context_cancelled` | 上下文取消 | 重连恢复 |
| `agent/not_initialized` | Agent 未初始化 | 重启服务 |

## B.5　SSE 格式

```
data: {"type":"message_delta",...}

data: {"type":"completed",...}

```

- 每行以 `data: ` 开头
- JSON 后跟两个换行符
- 空行表示事件结束

# 附录 A：API 契约

## A.1　P0 接口

### GET /health

健康检查。

**响应**：

```json
{
  "status": "ok"
}
```

### GET /api/models

列出可用模型。

**响应**：

```json
{
  "models": [
    {
      "name": "gpt-4",
      "display_name": "GPT-4",
      "supports_thinking": false,
      "supports_vision": false
    }
  ]
}
```

### POST /api/threads/:id/runs

创建运行。

**请求**：

```json
{
  "input": "Hello",
  "config": {
    "model_name": "gpt-4",
    "thinking_enabled": false,
    "is_plan_mode": false,
    "subagent_enabled": true,
    "agent_name": ""
  }
}
```

**响应**：SSE 流

### GET /api/memory

获取记忆。

**响应**：

```json
{
  "user_context": {
    "work_context": "",
    "personal_context": "",
    "top_of_mind": ""
  },
  "history": {
    "recent_months": "",
    "earlier_context": "",
    "long_term_background": ""
  },
  "facts": [
    {
      "id": "fact-1",
      "content": "User prefers Python",
      "category": "preference",
      "confidence": 0.9,
      "created_at": "2024-01-01T00:00:00Z"
    }
  ]
}
```

### POST /api/threads/:id/uploads

上传文件。

**请求**：multipart/form-data

**响应**：

```json
{
  "success": true,
  "files": [
    {
      "name": "document.pdf",
      "virtual_path": "/mnt/user-data/uploads/document.pdf",
      "mime_type": "application/pdf",
      "size_bytes": 1024
    }
  ]
}
```

## A.2　错误响应

```json
{
  "code": "validation/invalid_input",
  "message": "input is required",
  "details": {},
  "request_id": "req-123"
}
```

### 错误码前缀

| 前缀 | 含义 |
|------|------|
| `agent/` | Agent 运行错误 |
| `validation/` | 输入验证错误 |
| `auth/` | 认证错误 |
| `sandbox/` | 沙箱执行错误 |
| `tool/` | 工具执行错误 |
| `model/` | 模型调用错误 |
| `internal/` | 内部错误 |

## A.3　LangGraph 兼容路由

为兼容 DeerFlow 前端：

| DeerFlow 路由 | GoClaw 路由 |
|--------------|-------------|
| `/api/langgraph/threads/:id/runs/stream` | `/api/threads/:id/runs` |
| `/api/langgraph/threads/:id/runs` | `/api/threads/:id/runs` |
| `/api/langgraph/threads/:id/state` | `/api/threads/:id/state` |

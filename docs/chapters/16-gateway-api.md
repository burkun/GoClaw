# 第 16 章　Gateway HTTP API

## 16.1　服务器结构

```
Server
├── router: *gin.Engine      # Gin 路由器
├── cfg: *AppConfig          # 应用配置
├── agent: LeadAgent         # 默认 Agent
└── agents: map[string]LeadAgent # 多 Agent 支持
```

## 16.2　路由总览

### 核心路由

| 路由 | 方法 | 说明 |
|------|------|------|
| /health | GET | 健康检查 |
| /api/models | GET | 模型列表 |
| /api/threads/:id/runs | POST | 创建运行 |
| /api/threads/:id/uploads | POST | 上传文件 |
| /api/memory | GET | 获取记忆 |

### 扩展路由

| 路由 | 方法 | 说明 |
|------|------|------|
| /api/mcp/config | GET/PUT | MCP 配置 |
| /api/skills | GET | Skills 列表 |
| /api/agents | GET | Agent 列表 |
| /api/threads/:id/artifacts/*path | GET | 获取输出文件 |

### LangGraph 兼容路由

为兼容 DeerFlow 前端：

```
/api/langgraph/threads/:id/runs/stream → /api/threads/:id/runs
```

## 16.3　P0 API 详解

### POST /api/threads/:id/runs

**请求体**：

| 字段 | 类型 | 说明 |
|------|------|------|
| input | string | 用户输入 |
| config.model_name | string | 指定模型 |
| config.thinking_enabled | bool | 启用思考 |
| config.is_plan_mode | bool | 计划模式 |
| config.subagent_enabled | bool | 子代理 |
| config.agent_name | string | Agent 名称 |

**响应**：SSE 流

### POST /api/threads/:id/uploads

**请求**：multipart/form-data

**响应**：

| 字段 | 说明 |
|------|------|
| success | 是否成功 |
| files[].name | 文件名 |
| files[].virtual_path | 虚拟路径 |
| files[].mime_type | MIME 类型 |
| files[].size_bytes | 文件大小 |

### GET /api/memory

**响应**：

| 字段 | 说明 |
|------|------|
| user_context | 用户上下文 |
| history | 历史记录 |
| facts[] | 事实列表 |

## 16.4　SSE 响应流程

```
CreateRun handler
    │
    ├── 解析请求参数
    │
    ├── 获取 Agent 实例
    │   └── 根据 agent_name 选择
    │
    ├── 构建 ThreadState
    │
    ├── 构建 RunConfig
    │
    ├── 调用 agent.Run()
    │   └── 返回 Event Channel
    │
    └── 流式写入 SSE 响应
        ├── 设置 Content-Type: text/event-stream
        ├── 设置 Cache-Control: no-cache
        └── 循环发送事件
```

## 16.5　多 Agent 路由

```
GetAgent(name)
    │
    ├── name 为空 → 返回默认 Agent
    │
    ├── agents[name] 存在 → 返回对应 Agent
    │
    └── 不存在 → 返回默认 Agent
```

## 16.6　中间件

| 中间件 | 功能 |
|--------|------|
| Recovery | 恢复 panic |
| Logger | 请求日志 |
| CORS | 跨域支持 |

## 16.7　错误响应格式

```
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
| agent/ | Agent 运行错误 |
| validation/ | 输入验证错误 |
| auth/ | 认证错误 |
| sandbox/ | 沙箱错误 |
| tool/ | 工具错误 |

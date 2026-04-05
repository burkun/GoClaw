# 第 13 章　MCP 集成

## 13.1　MCP 概述

MCP (Model Context Protocol) 是标准化的工具协议，允许 Agent 通过统一接口调用外部服务。

### 传输方式

| 方式 | 特点 | 适用场景 |
|------|------|---------|
| stdio | 本地进程通信 | 本地工具、CLI 程序 |
| SSE | 服务器推送 | 实时更新、Web 服务 |
| HTTP | 请求响应 | REST API、标准服务 |

## 13.2　配置格式

### stdio 服务器

```
{
  "type": "stdio",
  "command": "mcp-filesystem",
  "args": ["/home/user/docs"],
  "env": {"DEBUG": "1"}
}
```

### SSE 服务器

```
{
  "type": "sse",
  "url": "https://api.example.com/mcp",
  "headers": {
    "Authorization": "Bearer $API_KEY"
  }
}
```

### HTTP 服务器

```
{
  "type": "http",
  "url": "https://api.example.com/tools",
  "oauth": {
    "token_url": "https://auth.example.com/token",
    "grant_type": "client_credentials",
    "client_id": "$CLIENT_ID",
    "client_secret": "$CLIENT_SECRET"
  }
}
```

## 13.3　工具发现流程

```
DiscoverMCPTools(serverName, config)
    │
    ├── 根据 type 选择传输方式
    │   ├── stdio → 启动进程
    │   ├── sse → 建立 SSE 连接
    │   └── http → 配置 HTTP 客户端
    │
    ├── 发送 tools/list 请求
    │
    ├── 解析返回的工具列表
    │   └── 名称、描述、参数 schema
    │
    └── 创建工具包装器
```

## 13.4　stdio 传输

### 连接建立

```
启动子进程
    │
    ├── 设置标准输入/输出管道
    │
    └── 进程保持运行
```

### 通信协议

```
请求格式（JSON-RPC 2.0）：
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/list",
  "params": {}
}

响应格式：
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "tools": [...]
  }
}
```

### 工具调用

```
调用流程：
1. 构建 Call 请求
2. 写入进程 stdin
3. 从进程 stdout 读取响应
4. 解析并返回结果
```

## 13.5　SSE/HTTP 传输

### 请求格式

与 stdio 类似，使用 JSON-RPC 2.0 格式，通过 HTTP POST 发送。

### 认证方式

| 方式 | 配置 |
|------|------|
| 静态 Token | headers.Authorization |
| OAuth client_credentials | oauth 配置块 |
| OAuth refresh_token | oauth.grant_type |

### OAuth 流程

```
TokenManager 获取 Token
    │
    ├── 检查缓存的 Token 是否有效
    │   └── 未过期 → 直接返回
    │
    └── 已过期或不存在
        ├── 构建 token 请求
        ├── 发送到 token_url
        └── 缓存新 Token
```

## 13.6　工具缓存

### 缓存策略

```
首次加载：
    ├── 扫描所有 MCP 服务器
    ├── 发现并注册工具
    └── 记录配置文件 mtime

后续请求：
    ├── 检查配置文件 mtime
    ├── 未变化 → 使用缓存
    └── 已变化 → 重新加载
```

### 缓存失效

```
InvalidateMCPConfigCache()
    │
    └── 清空缓存，下次请求重新加载
```

## 13.7　环境变量解析

配置中 `$VAR_NAME` 格式自动解析为环境变量：

```
配置："$OPENAI_API_KEY"
解析：os.Getenv("OPENAI_API_KEY")
```

## 13.8　错误处理

| 错误类型 | 处理方式 |
|---------|---------|
| 连接失败 | 记录日志，跳过该服务器 |
| 工具发现失败 | 记录日志，使用空列表 |
| 调用失败 | 返回错误给 Agent |

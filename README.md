# GoClaw

<div align="center">

**基于 CloudWeGo Eino 框架的 AI Agent 运行时**

Go 语言实现的多功能 AI Agent 平台

[![Go Version](https://img.shields.io/badge/Go-1.21%2B-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

</div>

---

## 项目简介

GoClaw 是 DeerFlow 的 Go 语言实现版本,基于 CloudWeGo 的 Eino 框架构建,提供了一套完整的 AI Agent 运行时基础设施。

### 核心特性

- **多轮对话与工具调用** - 支持 OpenAI、Anthropic、Azure 等多种 LLM 后端
- **沙箱隔离执行** - 通过 Local/Docker 沙箱安全执行命令和操作文件
- **18层中间件流水线** - 实现记忆、标题生成、规划、守卫等能力
- **子代理协作** - 支持将复杂任务委托给专门的子代理并行执行
- **SSE 流式输出** - 兼容 LangGraph API 格式的事件流
- **MCP 工具生态** - 支持 stdio/SSE/HTTP 三种传输方式
- **IM 渠道集成** - 支持 Telegram、Slack、飞书等即时通讯平台

### 为什么选择 Go

| 维度 | Python (DeerFlow) | Go (GoClaw) |
|------|-------------------|-------------|
| 部署形态 | 需要 Python 环境 + 依赖管理 | 单二进制,零依赖部署 |
| 镜像大小 | ~1GB | ~50MB |
| 启动速度 | 秒级 | 毫秒级 |
| 并发模型 | asyncio 协程,需显式 await | goroutine + channel,自然并发 |
| 类型安全 | 运行时检查 | 编译时强类型检查 |
| 内存占用 | 200-500MB | 50-100MB |

---

## 快速入门

### 环境要求

- Go 1.21 或更高版本
- Docker (可选,用于 Docker 沙箱模式)

### 安装

```bash
# 克隆项目
git clone https://github.com/bookerbai/goclaw.git
cd goclaw

# 复制配置文件
cp configs/config.example.yaml config.yaml

# 编辑配置文件,设置 API Key
vim config.yaml
```

### 最小配置

编辑 `config.yaml`:

```yaml
config_version: 1
log_level: info

server:
  address: ":8001"

models:
  - name: gpt-4o
    display_name: GPT-4o
    use: openai
    model: gpt-4o
    api_key: $OPENAI_API_KEY
    max_tokens: 4096

sandbox:
  use: local
```

设置环境变量:

```bash
export OPENAI_API_KEY=your-api-key
```

### 启动服务

```bash
# 方式1: 直接运行
go run ./cmd/goclaw

# 方式2: 编译后运行
make build-bin
./bin/goclaw
```

服务启动后访问 `http://localhost:8001`

### 验证运行

```bash
# 健康检查
curl http://localhost:8001/health
# {"status":"ok"}

# 查看模型列表
curl http://localhost:8001/api/models

# 发送对话请求
curl -X POST http://localhost:8001/api/threads/test-thread/runs \
  -H "Content-Type: application/json" \
  -d '{"input": "Hello, who are you?"}'
```

---

## 基本使用

### 创建对话

```bash
curl -X POST http://localhost:8001/api/threads/my-thread/runs \
  -H "Content-Type: application/json" \
  -d '{
    "input": "帮我分析当前目录的代码结构",
    "config": {
      "model_name": "gpt-4o"
    }
  }'
```

### 上传文件

```bash
curl -X POST http://localhost:8001/api/threads/my-thread/uploads \
  -F "file=@./example.txt"
```

### 使用多Agent

```bash
# 列出所有 Agent
curl http://localhost:8001/api/agents

# 使用特定 Agent
curl -X POST http://localhost:8001/api/threads/my-thread/runs \
  -H "Content-Type: application/json" \
  -d '{
    "input": "执行代码分析任务",
    "config": {
      "agent_name": "researcher"
    }
  }'
```

---

## 架构概述

### 系统架构

```
┌─────────────────────────────────────────────────────────────┐
│                      Gateway API (Gin)                      │
│  REST API + SSE 流式响应                                     │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                       Lead Agent                             │
│                                                              │
│  ┌─────────────────────────────────────────────────────┐   │
│  │              Middleware Chain (18层)                 │   │
│  │                                                      │   │
│  │  ThreadData → Uploads → Sandbox → Dangling →         │   │
│  │  Guardrail → Summarize → Todo → Title → Memory →    │   │
│  │  ViewImage → SubagentLimit → Loop → LLMError →       │   │
│  │  ToolError → Clarification → ...                     │   │
│  └─────────────────────────────────────────────────────┘   │
│                              │                              │
│  ┌─────────────────────────────────────────────────────┐   │
│  │                    Tools                             │   │
│  │  Sandbox Tools | MCP Tools | Built-in Tools         │   │
│  └─────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
      ┌─────────────┐ ┌─────────────┐ ┌─────────────┐
      │ Sub-Agent   │ │   Sandbox   │ │   Skills    │
      │  Executor   │ │  Provider   │ │  Registry   │
      └─────────────┘ └─────────────┘ └─────────────┘
```

### 目录结构

```
goclaw/
├── cmd/goclaw/           # 程序入口
├── internal/             # 私有模块
│   ├── agent/           # Lead Agent 核心
│   ├── middleware/      # 中间件系统 (18层)
│   ├── sandbox/         # 沙箱抽象 (Local/Docker)
│   ├── tools/           # 工具系统
│   ├── models/          # 模型工厂
│   ├── skills/          # Skills 系统
│   └── channels/        # IM 渠道集成
├── pkg/gateway/         # 公开模块
│   └── handlers/        # HTTP API 处理器
├── configs/             # 配置示例
├── docs/                # 文档 (VitePress)
└── config.yaml          # 主配置文件
```

### 核心组件

**Lead Agent** - 核心编排器,协调子代理和工具执行

**Middleware System** - 18层中间件,提供:
- ThreadData: 线程目录管理
- Sandbox: 沙箱实例获取/释放
- Memory: 长期记忆加载/更新
- Title: 自动标题生成
- Summarization: 上下文压缩
- Guardrail: 工具调用守卫
- 等等...

**Sandbox** - 虚拟路径系统隔离 Agent 与主机:
- Agent 视角: `/mnt/user-data/` (工作区/上传/输出), `/mnt/skills/` (只读)
- Local 模式: 每线程独立目录,命令白名单/黑名单
- Docker 模式: 容器隔离,CPU/内存/网络限制

**Sub-Agent System** - 子代理协作:
- `task` 工具: 将任务委托给专门子代理
- `Executor`: 并发调度器,基于信号量限制
- 内置代理: `general-purpose` (全工具), `bash` (仅Shell)

**Gateway** - Gin HTTP 服务器:
- `POST /api/threads/:id/runs` - 创建运行 (SSE 流)
- `POST /api/threads/:id/uploads` - 上传文件
- `GET /api/models` - 列出模型
- `GET /api/memory` - 获取记忆
- LangGraph 兼容路由

---

## 开发指南

### 构建与测试

```bash
# 构建
make build

# 生成可执行文件
make build-bin

# 运行所有测试
make test

# 运行中间件测试
make test-middleware

# 代码格式化
make fmt

# 代码检查
make vet

# 清理构建产物
make clean
```

### 添加新中间件

1. 在 `internal/middleware/` 下创建新目录
2. 实现 Middleware 接口
3. 在 `internal/agent/lead.go` 中注册

示例:

```go
// internal/middleware/my_middleware/middleware.go
package mymiddleware

type MyMiddleware struct {
    middleware.MiddlewareWrapper
}

func (m *MyMiddleware) Name() string {
    return "my_middleware"
}

func (m *MyMiddleware) Before(ctx context.Context, state *middleware.State) error {
    // 在模型调用前执行
    return nil
}

func (m *MyMiddleware) After(ctx context.Context, state *middleware.State, response *middleware.Response) error {
    // 在模型调用后执行
    return nil
}
```

### 添加新工具

1. 在 `internal/tools/` 下创建工具实现
2. 在配置文件中注册工具
3. (可选) 在 Skills 中配置访问控制

---

## 配置说明

### 模型配置

```yaml
models:
  - name: gpt-4o
    display_name: GPT-4o
    use: openai              # openai, anthropic, azure, google
    model: gpt-4o
    api_key: $OPENAI_API_KEY
    base_url: https://...    # 可选,用于 OpenRouter 等
    supports_thinking: true  # 扩展思考 (Anthropic)
    supports_vision: true    # 多模态
    max_tokens: 4096
    temperature: 0.7
```

### 沙箱配置

**Local Sandbox** (默认):

```yaml
sandbox:
  use: local
  bash_output_max_chars: 20000
  read_file_output_max_chars: 50000
```

**Docker Sandbox**:

```yaml
sandbox:
  use: docker
  image: your-sandbox-image:latest
  replicas: 3
  idle_timeout: 600
  mounts:
    - host_path: /path/on/host
      container_path: /path/in/container
      read_only: false
```

### 记忆系统配置

```yaml
memory:
  enabled: true
  storage_path: memory.json
  debounce_seconds: 30
  max_facts: 100
  fact_confidence_threshold: 0.7
```

### MCP 工具配置

在 `extensions_config.json` 中配置:

```json
{
  "mcpServers": {
    "filesystem": {
      "type": "stdio",
      "command": "mcp-filesystem",
      "args": ["/home/user/docs"]
    },
    "api": {
      "type": "http",
      "url": "https://api.example.com/mcp",
      "oauth": {
        "token_url": "https://auth.example.com/token",
        "client_id": "$CLIENT_ID",
        "client_secret": "$CLIENT_SECRET"
      }
    }
  }
}
```

---

## API 文档

详细的 API 文档请参见 [docs/api/](docs/api/) 目录。

### 主要端点

| 路由 | 方法 | 说明 |
|------|------|------|
| `/health` | GET | 健康检查 |
| `/api/models` | GET | 模型列表 |
| `/api/threads/:id/runs` | POST | 创建运行 (SSE流) |
| `/api/threads/:id/uploads` | POST | 上传文件 |
| `/api/threads/:id/artifacts/*path` | GET | 获取输出文件 |
| `/api/memory` | GET | 获取记忆 |
| `/api/agents` | GET | Agent 列表 |
| `/api/skills` | GET | Skills 列表 |
| `/api/mcp/config` | GET/PUT | MCP 配置 |

### SSE 事件类型

- `message_delta` - 增量文本输出
- `tool_event` - 工具调用事件
- `task_started/running/completed/failed` - 子代理生命周期
- `completed` - 成功终止
- `error` - 错误终止

---

## 部署指南

详细的部署文档请参见 [docs/deployment.md](docs/deployment.md)。

### Docker 部署

```bash
# 构建镜像
docker build -t goclaw:latest .

# 运行容器
docker run -d \
  --name goclaw \
  -p 8001:8001 \
  -e OPENAI_API_KEY=your-key \
  -v $(pwd)/.goclaw:/app/.goclaw \
  goclaw:latest
```

### Docker Compose

```bash
docker-compose up -d
```

### Kubernetes

参见 `deployments/` 目录中的 Kubernetes 配置文件。

---

## 更多文档

详细的技术文档请参见 `docs/` 目录:

- [项目概述](docs/chapters/01-what-is-goclaw.md)
- [项目结构](docs/chapters/02-project-structure.md)
- [快速上手](docs/chapters/03-quick-start.md)
- [Eino框架](docs/chapters/04-eino-framework.md)
- [Lead Agent](docs/chapters/05-lead-agent.md)
- [中间件系统](docs/chapters/06-middleware-system.md)
- [子代理系统](docs/chapters/08-subagent-overview.md)
- [沙箱抽象](docs/chapters/11-sandbox-abstraction.md)
- [工具系统](docs/chapters/12-tools-system.md)
- [MCP集成](docs/chapters/13-mcp-integration.md)
- [记忆系统](docs/chapters/14-memory-system.md)
- [Gateway API](docs/chapters/16-gateway-api.md)
- [SSE协议](docs/chapters/17-sse-protocol.md)
- [IM渠道](docs/chapters/18-im-channels.md)
- [配置系统](docs/chapters/19-config-system.md)
- [部署与生产化](docs/chapters/21-deployment.md)

---

## 贡献指南

我们欢迎任何形式的贡献!

### 贡献方式

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'Add amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 创建 Pull Request

### 代码规范

- 遵循 Go 标准代码规范
- 使用 `gofmt` 格式化代码
- 使用 `go vet` 进行静态检查
- 为新功能编写测试
- 更新相关文档

### 提交信息规范

使用清晰的提交信息:

- `feat: 添加新功能`
- `fix: 修复bug`
- `docs: 文档更新`
- `refactor: 代码重构`
- `test: 测试相关`
- `chore: 构建/工具相关`

---

## 许可证

本项目采用 MIT 许可证 - 详见 [LICENSE](LICENSE) 文件

---

## 致谢

- [CloudWeGo Eino](https://github.com/cloudwego/eino) - Agent 框架
- [DeerFlow](https://github.com/bytedance/DeerFlow) - 原始 Python 实现
- [Gin](https://github.com/gin-gonic/gin) - HTTP 框架

# 复用 DeerFlow 前端指南

GoClaw 提供了 LangGraph 兼容的 API，可以直接使用 DeerFlow 官方前端。

## 架构概览

```
┌─────────────────┐      SSE/HTTP      ┌─────────────────┐
│  DeerFlow       │ ◄──────────────────►│  GoClaw Backend │
│  Frontend       │   LangGraph API     │  (Go)           │
│  (Next.js)      │                     │  Port: 8001     │
└─────────────────┘                     └─────────────────┘
```

## 快速开始

### 1. 克隆 DeerFlow 前端

```bash
# 在 GoClaw 项目外克隆
git clone https://github.com/bytedance/deer-flow.git deer-flow-frontend
cd deer-flow-frontend/frontend
```

### 2. 安装依赖

```bash
pnpm install
```

### 3. 配置环境变量

创建 `.env.local`：

```env
# GoClaw 后端地址
NEXT_PUBLIC_API_URL=http://localhost:8001

# LangGraph SDK 配置
LANGGRAPH_API_URL=http://localhost:8001/api/langgraph

# 可选：认证（如果需要）
# NEXT_PUBLIC_AUTH_ENABLED=false
```

### 4. 启动服务

```bash
# 终端 1：启动 GoClaw 后端
cd /path/to/GoClaw
./goclaw

# 终端 2：启动 DeerFlow 前端
cd /path/to/deer-flow-frontend/frontend
pnpm dev
```

前端将在 `http://localhost:3000` 启动。

## API 兼容性

GoClaw 实现的 LangGraph 兼容路由：

| DeerFlow 前端调用 | GoClaw 路由 | 状态 |
|------------------|-------------|------|
| `GET /api/langgraph/assistants` | `/api/langgraph/assistants` | ✅ |
| `POST /api/langgraph/threads` | `/api/langgraph/threads` | ✅ |
| `GET /api/langgraph/threads/:id` | `/api/langgraph/threads/:id` | ✅ |
| `POST /api/langgraph/threads/:id/runs/stream` | `/api/langgraph/threads/:id/runs/stream` | ✅ |
| `GET /api/langgraph/threads/:id/state` | `/api/langgraph/threads/:id/state` | ✅ |

## 功能对比

| 功能 | DeerFlow (Python) | GoClaw (Go) | 兼容性 |
|------|-------------------|-------------|--------|
| 多轮对话 | ✅ | ✅ | 完全兼容 |
| SSE 流式输出 | ✅ | ✅ | 完全兼容 |
| 工具调用 | ✅ | ✅ | 完全兼容 |
| 文件上传 | ✅ | ✅ | 完全兼容 |
| 记忆系统 | ✅ | ✅ | 完全兼容 |
| 子代理 | ✅ | ✅ | 完全兼容 |
| MCP 工具 | ✅ | ✅ | 完全兼容 |

## 自定义前端

如果需要自定义前端样式或功能：

### 修改主题色

编辑 `frontend/src/styles/globals.css`：

```css
:root {
  --primary: 210 100% 50%;  /* Go 蓝色 */
  --background: 0 0% 100%;
}
```

### 修改 Logo

替换 `frontend/public/logo.svg`。

### 添加自定义组件

在 `frontend/src/components/` 下创建新组件。

## 生产部署

### Docker Compose

```yaml
version: '3.8'
services:
  goclaw:
    build: ./GoClaw
    ports:
      - "8001:8001"
    volumes:
      - ./config.yaml:/app/config.yaml
      - goclaw-data:/app/.goclaw
    environment:
      - GOCRAW_CONFIG_PATH=/app/config.yaml

  frontend:
    build: ./deer-flow-frontend/frontend
    ports:
      - "3000:3000"
    environment:
      - NEXT_PUBLIC_API_URL=http://goclaw:8001
      - LANGGRAPH_API_URL=http://goclaw:8001/api/langgraph
    depends_on:
      - goclaw

volumes:
  goclaw-data:
```

### 环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `NEXT_PUBLIC_API_URL` | GoClaw 后端地址 | `http://localhost:8001` |
| `LANGGRAPH_API_URL` | LangGraph API 地址 | `http://localhost:8001/api/langgraph` |

## 技术栈

DeerFlow 前端技术栈：

- **框架**: Next.js 16 (App Router)
- **UI**: React 19 + Tailwind CSS 4
- **组件**: shadcn/ui (Radix UI)
- **状态**: TanStack Query
- **代码编辑器**: CodeMirror 6
- **图表**: React Flow
- **SDK**: @langchain/langgraph-sdk

## 常见问题

### Q: 前端连接不上后端？

检查 CORS 配置，在 GoClaw 的 `config.yaml` 中：

```yaml
server:
  address: ":8001"
  cors:
    enabled: true
    origins: ["http://localhost:3000"]
```

### Q: SSE 流断开？

确保 Nginx/代理不禁用缓冲：

```nginx
proxy_buffering off;
proxy_cache off;
```

### Q: 认证不工作？

GoClaw 默认不启用认证。如需认证，在 DeerFlow 前端配置 `better-auth` 或使用外部认证服务。

## 相关文件

- GoClaw LangGraph 路由: `pkg/gateway/handlers/langgraph_routes.go`
- GoClaw LangGraph 兼容层: `pkg/gateway/handlers/langgraph_compat.go`
- SSE 事件协议: `docs/events.md`

# 第 1 章　GoClaw 是什么

## 1.1　项目定位

GoClaw 是 DeerFlow 的 Go 语言实现版本，复刻自字节跳动开源的 Super Agent Harness。它基于 CloudWeGo 的 Eino 框架构建，提供了一套完整的 AI Agent 运行时基础设施。

**核心能力**：
- **多轮对话与工具调用**：支持 OpenAI、Anthropic、Azure 等多种 LLM 后端
- **沙箱隔离执行**：通过 Local/Docker 沙箱安全执行命令和操作文件
- **中间件流水线**：14 层中间件实现记忆、标题生成、规划、守卫等能力
- **子代理协作**：支持将复杂任务委托给专门的子代理并行执行
- **SSE 流式输出**：兼容 LangGraph API 格式的事件流
- **MCP 工具生态**：支持 stdio/SSE/HTTP 三种传输方式

## 1.2　为什么用 Go 重写

| 维度 | Python (DeerFlow) | Go (GoClaw) |
|------|-------------------|-------------|
| 部署形态 | 需要 Python 环境 + 依赖管理 | 单二进制，零依赖部署 |
| 镜像大小 | ~1GB | ~50MB |
| 启动速度 | 秒级 | 毫秒级 |
| 并发模型 | asyncio 协程，需显式 await | goroutine + channel，自然并发 |
| 类型安全 | 运行时检查 | 编译时强类型检查 |
| 内存占用 | 200-500MB | 50-100MB |

## 1.3　架构概览

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
│  │              Middleware Chain (14层)                 │   │
│  │                                                      │   │
│  │  ThreadData → Uploads → Sandbox → Dangling →         │   │
│  │  Guardrail → Summarize → Todo → Title → Memory →    │   │
│  │  ViewImage → SubagentLimit → Loop → LLMError →       │   │
│  │  ToolError → Clarification                           │   │
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

## 1.4　执行流程

```
用户请求
    │
    ▼
Gateway 解析请求，创建 ThreadState
    │
    ▼
Lead Agent 执行
    │
    ├── BeforeAgent 钩子（所有中间件，顺序执行）
    │   ├── ThreadData: 创建线程目录
    │   ├── Sandbox: 获取沙箱实例
    │   └── Memory: 加载记忆
    │
    ├── 迭代循环
    │   ├── Before 钩子（每次模型调用前）
    │   ├── 模型调用
    │   ├── 工具执行（通过 WrapToolCall）
    │   └── After 钩子（每次模型调用后，逆序）
    │
    ├── AfterAgent 钩子（所有中间件，逆序执行）
    │   ├── Memory: 触发记忆更新
    │   └── Sandbox: 释放沙箱
    │
    ▼
SSE 事件流返回客户端
```

## 1.5　与 DeerFlow 的概念对齐

| DeerFlow | GoClaw | 说明 |
|----------|--------|------|
| `before_agent` | `BeforeAgent()` | Agent 运行前执行一次 |
| `before_model` | `Before()` | 每次模型调用前执行 |
| `after_model` | `After()` | 每次模型调用后执行 |
| `after_agent` | `AfterAgent()` | Agent 运行后执行一次 |
| `ThreadState` | `ThreadState` | 线程状态结构 |
| LangGraph | Eino ADK | Agent 编排框架 |
| FastAPI | Gin | HTTP 框架 |

## 1.6　适用场景

**推荐 GoClaw**：边缘部署、高并发、微服务架构、安全敏感、快速部署

**推荐 DeerFlow**：Python 技术栈、快速原型、LangChain 生态、Jupyter 集成

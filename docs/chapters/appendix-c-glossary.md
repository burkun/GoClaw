# 附录 C：术语表

## C.1　核心概念

| 术语 | 英文 | 定义 |
|------|------|------|
| Agent | Agent | AI 代理，能够执行任务和调用工具 |
| Lead Agent | Lead Agent | 主代理，负责编排子代理和工具调用 |
| Sub-Agent | Sub-Agent | 子代理，由主代理委托执行特定任务 |
| Middleware | Middleware | 中间件，在 Agent 执行过程中注入逻辑 |
| Sandbox | Sandbox | 沙箱，隔离的执行环境 |
| Tool | Tool | 工具，Agent 可调用的能力 |
| Skill | Skill | 技能，预定义的任务模板 |
| Thread | Thread | 线程，一次完整的对话会话 |
| Checkpoint | Checkpoint | 检查点，保存的状态快照 |
| MCP | Model Context Protocol | 模型上下文协议，标准化的工具协议 |

## C.2　中间件

| 术语 | 说明 |
|------|------|
| BeforeAgent | Agent 运行前的钩子 |
| Before | 模型调用前的钩子 |
| After | 模型调用后的钩子 |
| AfterAgent | Agent 运行后的钩子 |
| WrapToolCall | 工具调用包装器 |
| State | 中间件共享的状态 |
| Reducer | 状态合并函数 |

## C.3　沙箱

| 术语 | 说明 |
|------|------|
| Virtual Path | 虚拟路径，Agent 看到的路径 |
| Physical Path | 物理路径，实际的文件系统路径 |
| SandboxProvider | 沙箱提供者，管理沙箱生命周期 |
| LocalSandbox | 本地沙箱，直接在主机执行 |
| DockerSandbox | Docker 沙箱，在容器中执行 |

## C.4　事件

| 术语 | 说明 |
|------|------|
| SSE | Server-Sent Events，服务器推送事件 |
| message_delta | 增量文本事件 |
| tool_event | 工具调用事件 |
| completed | 完成事件 |
| error | 错误事件 |

## C.5　记忆

| 术语 | 说明 |
|------|------|
| User Context | 用户上下文 |
| History | 历史记录 |
| Fact | 事实 |
| Debounce | 防抖，延迟处理更新 |

## C.6　框架

| 术语 | 说明 |
|------|------|
| Eino | CloudWeGo 的 Go Agent 框架 |
| ADK | Agent Development Kit |
| schema | 消息 schema |
| compose | 组合器 |

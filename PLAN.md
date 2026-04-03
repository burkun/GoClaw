# GoClaw Implementation Plan（优化版）

基于 Eino 框架复刻 DeerFlow，构建 Go 语言版本的 AI Agent Harness。

---

## 1. 项目目标

**目标**：实现一个可独立运行、可扩展、可部署的 Go Agent Harness，在核心能力上对齐 DeerFlow：
- 多轮对话与工具调用
- 文件/命令执行能力（受沙箱约束）
- 中间件链（记忆、标题、规划等）
- 子代理任务分解
- HTTP API + SSE 流式输出
- MCP 工具生态接入

**核心价值**：
- Go 的性能与部署优势（单二进制）
- 更清晰的类型边界与并发控制
- 与 Eino 生态对齐，便于复用组件

---

## 2. 范围管理（先做什么，不做什么）

### 2.1 MVP 范围（必须完成）

MVP 到 **Phase 5** 为止：
1. CLI 可运行（基础对话 + 文件工具 + 命令工具）
2. 中间件链最小闭环（Memory / Title / Plan）
3. 子代理基础能力（并发限制、超时、结果回收）
4. 沙箱抽象 + 本地沙箱 + Docker 沙箱最小实现
5. Gateway API + SSE（满足前端/客户端最小可用）

### 2.2 Post-MVP 范围（后续）

- Phase 6: MCP 完整集成
- Phase 7: IM 渠道（Telegram/Slack/Feishu）
- Phase 8: Web 前端与部署增强
- Phase 9: Tauri 桌面应用

### 2.3 非目标（当前不做）

- 在 MVP 阶段同时追求“所有 DeerFlow endpoint 完全对等”
- 在 MVP 阶段完成跨平台桌面安装包体系
- 在 MVP 阶段引入过多实验性工具（先保留接口）

---

## 3. 兼容性契约（避免返工）

## 3.1 API 兼容分级

### P0（MVP 必须兼容）
- `GET /api/models`
- `POST /api/threads/:id/runs`
- `POST /api/threads/:id/uploads`
- `GET /api/memory`
- SSE 基本事件流（消息增量、结束事件、错误事件）

### P1（MVP 后兼容）
- 更完整线程管理接口
- artifacts/skills/mcp/channels 等扩展接口

## 3.2 SSE 事件契约

- 统一事件 envelope：`type`、`thread_id`、`payload`、`timestamp`
- 至少支持：`message_delta`、`tool_event`、`completed`、`error`
- 保证顺序与终止语义（每次 run 必有 completed 或 error）

## 3.3 错误响应契约

- 统一 JSON 错误结构：`code`、`message`、`details`、`request_id`
- 工具错误、沙箱错误、模型错误分别定义稳定 code 前缀

---

## 4. 决策门（Decision Gates）

### Gate A：前端方案（在 Phase 0 决策）
- **A1 复用 deer-flow frontend**：优先 API 契约兼容
- **A2 新建轻量前端**：优先 Go API 可演进性

> 决策输出：`docs/decisions/0001-frontend-strategy.md`

### Gate B：HTTP 框架（在 Phase 0 决策）
- 候选：Gin / Echo
- 依据：路由中间件能力、SSE 体验、基准结果、可维护性

> 决策输出：`docs/decisions/0002-http-framework.md`

### Gate C：默认沙箱模式（在 Phase 1 决策）
- 候选：Local 默认 / Docker 默认
- 依据：安全基线、开发便利性、CI 成本

> 决策输出：`docs/decisions/0003-sandbox-default.md`

---

## 5. 分阶段计划（含验收标准 DoD）

## Phase 0：工程基线与契约先行

### 目标
建立可持续迭代的底座，先锁定关键契约与技术决策。

### 任务
- [ ] 初始化目录与 Go module（`github.com/bookerbai/goclaw`）
- [ ] 建立 Makefile（build/test/lint/run）
- [ ] 接入 golangci-lint + gofmt + go test
- [ ] 建立 CI（lint + unit）
- [ ] 完成 Gate A/B 决策文档
- [ ] 产出 API/SSE/错误响应契约草案

### DoD（验收）
- [ ] `make lint`、`make test` 在本地与 CI 均可通过
- [ ] 3 份决策文档存在且结论明确
- [ ] 契约文档可驱动后续开发，不再口头约定

---

## Phase 1：核心 Agent 与基础工具

### 任务
- [ ] 基于 `deep.New` 实现 LeadAgent
- [ ] CLI 入口（`cmd/goclaw/main.go`）
- [ ] 模型切换（OpenAI/Claude）
- [ ] 基础工具：read/write/edit/ls、shell、grep/glob
- [ ] 配置系统（yaml + env）与热重载
- [ ] Agent Loop 运行时配置：`recursion_limit`（默认 100）
- [ ] 子代理轮次配置：`max_turns`（默认 50）
- [ ] 明确 loop 终止条件（非工具响应或达到上限）

### DoD
- [ ] CLI 可完成一次端到端会话（含至少 1 次工具调用）
- [ ] 配置变更可生效（无需重启或具备明确定义的重载命令）
- [ ] 关键工具具备单元测试（成功/失败/边界路径）
- [ ] `recursion_limit` / `max_turns` 可通过运行时配置覆盖
- [ ] loop 终止语义可验证（达到上限时返回稳定错误码）

---

## Phase 2：中间件系统

### 任务
- [ ] Memory 结构、存储、提取、注入
- [ ] Vision 支持（图片读取 + 多模态消息组装）
- [ ] Web 工具（Tavily / Jina Reader）
- [ ] 上下文压缩、任务规划、标题生成中间件
- [ ] 显式 Hooks 接口：`BeforeModel` / `AfterModel` / `WrapModelCall` / `WrapToolCall`
- [ ] 运行生命周期 Hook：`OnQAStart` / `OnQAEnd` / `OnBeforeReturn`
- [ ] Guardrail Hook：工具执行前策略检查（可阻断）
- [ ] 插件注册入口：支持在 hook 点注入逻辑与工具
- [ ] Hook 执行策略：优先级、超时、失败隔离（单个插件失败不拖垮主流程）

### DoD
- [ ] 中间件顺序可配置且可测试
- [ ] Memory 去重与更新策略可验证
- [ ] 至少 1 条集成测试覆盖“多中间件联动”
- [ ] `WrapToolCall` 拦截可验证（允许/拒绝两条路径）
- [ ] `OnQAStart/OnQAEnd/OnBeforeReturn` 触发顺序可验证
- [ ] 插件可注册自定义 hook 并影响执行流程
- [ ] Hook 超时与失败隔离策略可通过集成测试

---

## Phase 3：子代理系统

### 任务
- [ ] `task` 工具与子代理执行器
- [ ] 内置子代理（general-purpose/code/research）
- [ ] 并发限制、超时、重试策略
- [ ] 子代理运行时配置：`max_concurrent_subagents`（默认 3）、`timeout_seconds`（默认 900）
- [ ] 任务状态机：`pending/running/completed/failed/timed_out`
- [ ] 子代理事件流：`task_started/task_running/task_completed/task_failed/task_timed_out`
- [ ] 后台任务存储（task_id -> result），支持轮询查询与结果回收
- [ ] 主流程降级策略：子代理失败时返回结构化错误，不中断主代理

### 实现要点
- [ ] 执行器采用双层池化架构（scheduler + worker），避免主流程阻塞
- [ ] 使用 context 超时与取消信号统一控制子任务生命周期
- [ ] `SubagentLimitMiddleware` 在模型输出后裁剪超限 `task` 调用
- [ ] 子代理消息与主对话消息隔离存储，最终仅回传摘要结果

### DoD
- [ ] 并发上限生效（超限请求行为明确）
- [ ] 子代理失败不会拖垮主流程（有降级结果）
- [ ] 有回归测试覆盖超时、取消、部分失败
- [ ] 后台任务状态可查询且状态迁移合法
- [ ] 子代理事件可通过 SSE 观察到完整生命周期

---

## Phase 4：沙箱隔离

### 任务
- [ ] Sandbox 接口定义与 provider 抽象
- [ ] 本地沙箱（per-thread 目录隔离）
- [ ] Docker 沙箱（生命周期、资源限制、挂载策略）
- [ ] 路径逃逸与危险命令防护

### DoD
- [ ] 无法跨线程目录访问
- [ ] 危险路径（如 `../`）被拦截并有明确错误
- [ ] Docker 沙箱可稳定执行并回收资源

---

## Phase 5：HTTP API 与 SSE（MVP 完成）

### 任务
- [ ] Gateway API（P0 接口）
- [ ] Run API：`POST /threads/{id}/runs`、`GET /threads/{id}/runs`、`GET /threads/{id}/runs/{run_id}`、`POST /threads/{id}/runs/{run_id}/cancel`
- [ ] Run Stream API：`POST /threads/{id}/runs/stream`、`GET /threads/{id}/runs/{run_id}/join`
- [ ] SSE 流式输出（message/tool/error/completed/end）
- [ ] 断开策略：`on_disconnect=cancel|continue`
- [ ] 恢复能力：`checkpoint_id` 恢复执行 + 线程 state/history 查询接口
- [ ] 文件上传与 artifact 最小链路
- [ ] API 文档与示例

### DoD
- [ ] P0 接口全部可用并有契约测试
- [ ] Run 创建/查询/取消/join 全链路可用
- [ ] SSE 终止语义稳定（必有 completed/error，且最终有 end）
- [ ] cancel 支持 interrupt/rollback 两种语义
- [ ] 客户端断流后可按 event_id 恢复订阅（resumable stream）
- [ ] 能对接选定前端策略（Gate A）并完成一次完整交互

---

## Phase 6：MCP 集成（Post-MVP）

### 任务
- [ ] MCP client（`stdio` / `sse` / `http` transport）
- [ ] 配置、生命周期、工具发现与注册
- [ ] 工具缓存与配置变更检测
- [ ] 延迟工具注册（deferred loading）与按需激活
- [ ] 工具权限模型（server 级 / tool 级）
- [ ] 权限决策优先级（deny > ask > allow）与默认策略

### DoD
- [ ] 3 种 transport 至少 2 种通过集成测试
- [ ] 工具缓存命中/失效路径可观测
- [ ] 延迟注册工具可被搜索并按需激活
- [ ] MCP 权限策略可观测（命中 allow/ask/deny）

---

## Phase 6.5：Skills 与插件系统（Post-MVP）

### 任务
- [ ] Skills 扫描加载（`skills/public`、`skills/custom`）
- [ ] `SKILL.md` frontmatter 解析（name/description/version/allowed-tools）
- [ ] skills 启用状态配置（`extensions.json`）
- [ ] 插件核心接口：hooks 注册、tools 注入、生命周期管理
- [ ] 插件生命周期：`OnLoad` / `OnUnload` / `OnConfigReload`
- [ ] 插件事件面：模型前后、QA 前后、QA 返回前、工具注入阶段
- [ ] 插件隔离策略（权限、目录、可用工具白名单）

### DoD
- [ ] 至少 3 个样例 skill 可加载并执行
- [ ] 插件可注册 hook 并注入 tool
- [ ] 插件生命周期回调可观测（load/unload/reload）
- [ ] 插件权限控制可阻断越权工具调用

---

## Phase 6.6：Cron 定时任务（Post-MVP）

### 任务
- [ ] 实现 `cron_create` / `cron_list` / `cron_delete` 工具
- [ ] 定时调度器（支持 cron 表达式与一次性任务）
- [ ] 会话级生命周期管理（进程退出自动清理）
- [ ] 可选持久化恢复（重启后恢复任务）
- [ ] 时区与 misfire 策略（错过触发后的补偿/跳过）
- [ ] 任务并发与重入保护

### DoD
- [ ] 定时任务可创建、触发、取消
- [ ] 调度器支持并发任务且无重复执行
- [ ] 重启恢复与 misfire 行为符合配置
- [ ] 调度任务失败不影响主会话稳定性

---

## Phase 7：IM 渠道（Post-MVP）

### 任务
- [ ] Telegram（long-polling）
- [ ] Slack（Socket Mode）
- [ ] Feishu/Lark（WebSocket）

### DoD
- [ ] 每个渠道至少完成一条端到端消息链路
- [ ] 渠道故障不影响核心 API 可用性

---

## Phase 8：前端与部署（Post-MVP）

### 任务
- [ ] 完成 Gate A 对应前端落地
- [ ] Docker 镜像、docker-compose、可选 K8s manifests
- [ ] README / API / 部署 / 配置文档

### DoD
- [ ] 一键启动路径清晰
- [ ] 新机器可按文档完成部署

---

## Phase 9：Tauri 桌面应用（Post-MVP）

### 任务
- [ ] Sidecar 集成（GoClaw Server）
- [ ] 桌面核心功能（托盘、后台运行、自动更新）
- [ ] 跨平台构建与分发

### DoD
- [ ] 至少 2 个桌面平台可安装并稳定运行
- [ ] 桌面端可调用本地 GoClaw API 完成完整会话

---

## 6. 测试策略（分层）

### 6.1 Unit
- 工具层、配置层、错误映射、模型适配

### 6.2 Integration
- Agent + Middleware + Hook 生命周期
- SubagentExecutor + task tool + 状态机
- Sandbox + Tool 调用
- Gateway Runs API + SSE + cancel/join/resume
- MCP transport + 权限决策

### 6.3 E2E
- 从请求到流式响应完成的全链路
- 上传文件 -> 工具处理 -> artifact 输出

### 6.4 Contract
- P0 API / SSE / error schema 自动校验
- 与 deer-flow 期望行为的差异报告

---

## 7. 安全基线（从 Phase 0 开始执行）

- [ ] 命令执行策略（allowlist/denylist）
- [ ] 路径规范化与目录越权防护
- [ ] API Key 安全存储与日志脱敏
- [ ] 默认最小权限运行
- [ ] 安全事件与审计日志字段规范

---

## 8. 风险台账（带触发与应对）

| 风险 | 级别 | 触发信号 | 应对动作 | Owner |
|------|------|----------|----------|-------|
| MCP 兼容性不足 | 高 | 主流 server 接入失败率高 | 降级到核心 transport，先保证 stdio | TBD |
| Docker 沙箱不稳定 | 高 | 容器泄漏或超时率高 | 增加 watchdog 与回收策略 | TBD |
| SSE 性能抖动 | 中 | 并发下延迟明显升高 | 优化 flush/缓冲策略，增加压测门禁 | TBD |
| 上游依赖变更 | 中 | Eino API 破坏性更新 | 增加适配层与版本锁定策略 | TBD |

---

## 9. 建议目录结构

```text
goclaw/
├── cmd/
│   ├── goclaw/
│   │   └── main.go
│   └── goclaw-server/
│       └── main.go
├── internal/
│   ├── agent/
│   │   ├── lead.go
│   │   └── subagents/
│   │       ├── executor.go
│   │       ├── registry.go
│   │       ├── config.go
│   │       └── builtins/
│   ├── middleware/
│   │   ├── memory/
│   │   ├── vision/
│   │   └── title/
│   ├── hooks/
│   │   ├── model_hooks.go
│   │   └── tool_hooks.go
│   ├── sandbox/
│   │   ├── sandbox.go
│   │   ├── local.go
│   │   └── docker.go
│   ├── mcp/
│   │   ├── client.go
│   │   └── transport/
│   ├── skills/
│   │   ├── loader.go
│   │   └── parser.go
│   ├── cron/
│   │   └── scheduler.go
│   ├── tools/
│   │   ├── websearch/
│   │   └── webfetch/
│   └── config/
│       └── config.go
├── pkg/
│   ├── gateway/
│   │   ├── server.go
│   │   └── handlers/
│   ├── channels/
│   │   ├── telegram/
│   │   ├── slack/
│   │   └── feishu/
│   └── client/
│       └── client.go
├── docs/
│   ├── decisions/
│   └── contracts/
├── configs/
│   ├── config.yaml
│   └── extensions.json
├── Makefile
├── go.mod
└── README.md
```

---

## 10. 近期执行清单（按优先级）

1. [ ] 建立 Phase 0 工程骨架（module + Makefile + lint + test + CI）
2. [ ] 完成 Gate A/B 决策文档
3. [ ] 输出 P0 API/SSE/error 契约文档
4. [ ] 搭建 CLI 主流程与最小 LeadAgent（含 recursion_limit / max_turns）
5. [ ] 定义 4 个 Hook Point（Before/After/WrapModel/WrapTool）
6. [ ] 接入首批基础工具并补 unit test
7. [ ] 定义 Sandbox 接口并实现 local 最小版本
8. [ ] 设计 Skills loader 与 cron 工具接口
9. [ ] 完成 SubagentExecutor 骨架（状态机 + 事件流 + 超时控制）

---

*更新时间: 2026-04-03*
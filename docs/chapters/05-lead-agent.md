# 第 5 章　Lead Agent：核心循环

## 5.1　职责

Lead Agent 是 GoClaw 的核心编排器，负责：
- 按顺序执行中间件链的钩子
- 组装工具集并处理工具调用
- 维护 ThreadState 状态
- 生成 SSE 事件流
- 支持多 Agent 配置

## 5.2　核心数据结构

### ThreadState

线程级状态，贯穿整个对话生命周期：

| 字段 | 类型 | 说明 |
|------|------|------|
| Messages | []*Message | 完整消息历史 |
| Sandbox | *SandboxState | 沙箱 ID |
| ThreadData | *ThreadDataState | 线程目录路径 |
| Title | string | 自动生成的标题 |
| Artifacts | []string | 输出文件列表 |
| Todos | []map | 计划模式任务 |
| UploadedFiles | []UploadedFile | 上传的文件 |
| ViewedImages | map | 已查看的图片 |

### RunConfig

运行时配置，控制 Agent 行为：

| 字段 | 说明 |
|------|------|
| ThreadID | 线程唯一标识 |
| ModelName | 指定模型（空=默认） |
| ThinkingEnabled | 启用扩展思考 |
| IsPlanMode | 启用计划模式 |
| SubagentEnabled | 启用子代理 |
| AgentName | 自定义 Agent 名称 |

## 5.3　创建流程

```
New() / NewWithName()
    │
    ├── 1. 加载主配置 (config.yaml)
    │
    ├── 2. 加载 Skills
    │   ├── 扫描 skills/ 目录
    │   ├── 解析 SKILL.md frontmatter
    │   └── 注册到 SkillRegistry
    │
    ├── 3. 创建 LLM 模型
    │   └── 根据 use 字段动态创建
    │
    ├── 4. 注册工具
    │   ├── 默认工具（沙箱工具）
    │   ├── MCP 工具（发现并注册）
    │   └── 子代理 task 工具
    │
    ├── 5. 应用 Skills 工具过滤
    │   └── 根据 allowed-tools 过滤
    │
    ├── 6. 构建中间件链
    │   └── 14 层按固定顺序
    │
    ├── 7. 构建系统提示词
    │   ├── 角色
    │   ├── SOUL.md（Agent 个性）
    │   └── Skills 列表
    │
    └── 8. 创建 Eino Agent
        └── 传入模型、工具、中间件
```

## 5.4　中间件链顺序

中间件按固定顺序执行，确保正确的依赖关系：

```
1. ThreadDataMiddleware     - 创建线程目录
2. UploadsMiddleware        - 注入上传文件
3. SandboxMiddleware        - 获取沙箱
4. DanglingToolCallMiddleware - 处理悬空调用
5. GuardrailMiddleware      - 工具调用守卫
6. AuditMiddleware          - 审计日志
7. SummarizationMiddleware  - 上下文压缩（可选）
8. TodoMiddleware           - 计划模式
9. TitleMiddleware          - 自动标题（可选）
10. MemoryMiddleware        - 长期记忆
11. ViewImageMiddleware     - 图片注入
12. SubagentLimitMiddleware - 并发限制
13. LoopDetectionMiddleware - 循环检测
14. DeferredToolFilterMiddleware - 延迟工具过滤
15. TokenUsageMiddleware    - Token 统计（可选）
16. LLMErrorMiddleware      - LLM 错误重试
17. ToolErrorMiddleware     - 工具错误处理
18. ClarificationMiddleware - 澄清拦截
```

**执行顺序**：
- BeforeAgent/Before：顺序执行
- After/AfterAgent：逆序执行
- WrapToolCall：按顺序包装

## 5.5　Run 流程

```
Run(ctx, state, cfg)
    │
    ├── 验证 ThreadID
    │
    ├── 创建事件通道
    │
    ├── 同步 Skills 配置（热重载）
    │
    ├── 准备消息
    │   └── 添加计划模式/子代理提示
    │
    ├── 构建 Session Values
    │   └── 传递给子代理和中间件
    │
    ├── 启动 Runner
    │   └── 返回 EventStream
    │
    └── 异步消费事件流
        ├── 转换 Eino 事件为 GoClaw 事件
        ├── 发送 message_delta
        ├── 发送 tool_event
        └── 发送 completed/error
```

## 5.6　多 Agent 支持

### 目录结构

```
.goclaw/agents/
├── researcher/
│   ├── config.yaml    # Agent 配置
│   └── SOUL.md        # Agent 个性
├── coder/
│   ├── config.yaml
│   └── SOUL.md
```

### Agent 配置

```yaml
name: researcher
model: gpt-4  # 可选模型覆盖
skills:
  - web-search
tool_groups:
  - web
```

### SOUL.md 作用

定义 Agent 的个性和工作方式，会被注入到系统提示词的 `<soul>` 标签中。

### 加载逻辑

```
main.go 启动
    │
    ├── 扫描 .goclaw/agents/ 目录
    │
    ├── 对每个 Agent：
    │   ├── 加载 config.yaml
    │   ├── 加载 SOUL.md
    │   ├── 创建 LeadAgent 实例
    │   └── 存入 agents map
    │
    └── 传递给 Gateway
        └── 根据 agent_name 路由
```

## 5.7　检查点与恢复

**检查点作用**：保存 Agent 运行状态，支持暂停后恢复

**存储方式**：文件存储在 `.goclaw/checkpoints/`

**恢复流程**：
```
Resume(ctx, state, cfg, checkpointID)
    │
    ├── 验证 checkpointID
    │
    ├── 从存储加载状态
    │
    └── 继续执行
```

## 5.8　系统提示词结构

```
<role>
You are GoClaw, an open-source super agent.
</role>

<soul>
{Agent 个性，来自 SOUL.md}
</soul>

<skills>
- skill-name: description
  Path: /mnt/skills/public/skill-name/SKILL.md
</skills>

<filesystem>
- Workspace: /mnt/user-data/workspace/
- Uploads: /mnt/user-data/uploads/
- Outputs: /mnt/user-data/outputs/
</filesystem>

<critical_reminders>
- Clarification First
- Skill First
- Output Files location
</critical_reminders>
```

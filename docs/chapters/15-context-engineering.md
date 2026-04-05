# 第 15 章　上下文工程

## 15.1　上下文压缩

### 触发条件

| 触发类型 | 说明 |
|---------|------|
| tokens | token 数超过阈值 |
| messages | 消息数超过阈值 |
| fraction | token 占模型上限的比例 |

### 压缩流程

```
SummarizationMiddleware.Before
    │
    ├── 检查是否需要压缩
    │   ├── TokenCount > Threshold
    │   ├── len(Messages) > Threshold
    │   └── TokenCount > MaxTokens * Ratio
    │
    ├── 分离消息
    │   ├── 待压缩：较早的消息
    │   └── 保留：最近 N 条 + 系统提示词
    │
    ├── 调用 LLM 生成摘要
    │
    └── 替换消息历史
        └── 摘要 + 保留的消息
```

### 配置项

```
summarization:
  enabled: true
  trigger:
    type: tokens
    threshold: 100000
  keep:
    recent_messages: 5
    system_prompt: true
```

## 15.2　Token 计数

### 估算方法

```
TokenUsageMiddleware.Before
    │
    ├── 遍历所有消息
    │
    └── 累计 content 长度 / 4
        └── 简单估算：约 4 字符 = 1 token
```

### 用途

- 压缩触发判断
- 资源使用统计
- 日志记录

## 15.3　图片注入

### 触发条件

```
ViewImageMiddleware.Before
    │
    ├── 检查是否有 ViewedImages
    │
    └── 检查模型是否支持 vision
```

### 注入逻辑

```
处理流程：
    │
    ├── 找到最后一条用户消息
    │
    ├── 构建多模态内容
    │   ├── 文本部分（原内容）
    │   └── 图片部分（base64 + MIME）
    │
    └── 替换消息内容
```

### 清空机制

```
注入后清空 ViewedImages
    └── 避免重复注入
```

## 15.4　悬空工具调用处理

### 产生原因

```
场景：
1. 模型请求调用工具
2. 用户中断对话
3. 没有对应的 ToolMessage 响应

结果：消息历史中存在未完成的工具调用
```

### 处理逻辑

```
DanglingToolCallMiddleware.Before
    │
    ├── 找到最后一条 AI 消息
    │
    ├── 检查 tool_calls 列表
    │
    ├── 查找已响应的 tool_call_id
    │
    └── 为未响应的添加占位 ToolMessage
        └── "Tool execution was interrupted..."
```

## 15.5　系统提示词构建

### 结构

```
<role>
Agent 身份定义
</role>

<soul>
Agent 个性（来自 SOUL.md）
</soul>

<skills>
启用的 Skills 列表
</skills>

<memory>
注入的记忆
</memory>

<filesystem>
虚拟路径说明
</filesystem>

<critical_reminders>
关键提醒
</critical_reminders>
```

### 动态内容

| 部分 | 来源 |
|------|------|
| soul | Agent 的 SOUL.md |
| skills | Skills Registry |
| memory | Memory Store |

## 15.6　计划模式提示

当 `is_plan_mode = true` 时，添加：

```
Plan mode is enabled. Keep task tracking explicit.
```

并注册 `write_todos` 工具。

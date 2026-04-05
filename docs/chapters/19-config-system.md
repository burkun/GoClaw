# 第 19 章　配置体系

## 19.1　配置文件

### 主配置文件 (config.yaml)

| 部分 | 说明 |
|------|------|
| models | LLM 模型配置 |
| sandbox | 沙箱配置 |
| memory | 记忆系统配置 |
| title | 标题生成配置 |
| summarization | 上下文压缩配置 |
| subagents | 子代理配置 |
| skills | Skills 配置 |
| channels | IM 渠道配置 |

### 扩展配置文件 (extensions_config.json)

| 部分 | 说明 |
|------|------|
| mcpServers | MCP 服务器配置 |
| skills | Skills 启用状态 |

## 19.2　配置优先级

### 配置文件查找

```
1. 环境变量 GOCRAW_CONFIG_PATH
2. 当前目录 config.yaml
3. 父目录 config.yaml
```

### 环境变量解析

配置值 `$VAR_NAME` 自动解析为环境变量：

```
api_key: $OPENAI_API_KEY
  ↓
api_key: "sk-xxx..."
```

## 19.3　模型配置

### 基本字段

| 字段 | 必填 | 说明 |
|------|------|------|
| name | 是 | 内部标识 |
| display_name | 是 | 显示名称 |
| use | 是 | 提供者类路径 |
| model | 是 | 模型标识 |
| api_key | 是 | API Key |
| base_url | 否 | 自定义端点 |

### 扩展字段

| 字段 | 说明 |
|------|------|
| supports_thinking | 是否支持扩展思考 |
| supports_vision | 是否支持视觉 |
| max_tokens | 最大输出 token |
| temperature | 温度参数 |

### 提供者类型

| use | 说明 |
|-----|------|
| openai | OpenAI API |
| anthropic | Anthropic API |
| azure | Azure OpenAI |

## 19.4　沙箱配置

### Local 模式

```
sandbox:
  type: local
  work_dir: .goclaw
  allowed_commands: [...]
  denied_commands: [...]
```

### Docker 模式

```
sandbox:
  type: docker
  docker:
    image: goclaw-sandbox:latest
    cpu_quota: 100000
    memory_bytes: 536870912
    network_disabled: true
```

## 19.5　记忆配置

| 配置 | 默认值 | 说明 |
|------|-------|------|
| enabled | true | 是否启用 |
| injection_enabled | true | 是否注入 |
| storage_path | memory.json | 存储路径 |
| debounce_seconds | 30 | 防抖秒数 |
| max_facts | 100 | 最大事实数 |
| fact_confidence_threshold | 0.7 | 置信度阈值 |

## 19.6　子代理配置

| 配置 | 默认值 | 说明 |
|------|-------|------|
| enabled | true | 是否启用 |
| max_concurrent | 3 | 最大并发 |
| timeout_seconds | 900 | 超时秒数 |

## 19.7　配置热重载

### 检测机制

```
CheckConfigReload()
    │
    ├── 比较配置文件 mtime
    │
    └── 变化 → 重新加载
```

### 生效范围

- 新请求立即使用新配置
- 运行中的请求不受影响

## 19.8　多 Agent 配置

### 目录结构

```
.goclaw/agents/
└── {agent_name}/
    ├── config.yaml
    └── SOUL.md
```

### Agent 配置字段

| 字段 | 说明 |
|------|------|
| name | Agent 名称 |
| model | 指定模型 |
| skills | 启用的 Skills |
| tool_groups | 工具组 |

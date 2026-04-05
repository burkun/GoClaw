# 第 12 章　工具系统

## 12.1　工具分类

```
工具体系
│
├── Sandbox Tools（沙箱工具）
│   ├── bash      - 命令执行
│   ├── ls        - 目录列表
│   ├── read_file - 文件读取
│   ├── write_file - 文件写入
│   └── str_replace - 内容替换
│
├── Built-in Tools（内置工具）
│   ├── task          - 子代理委托
│   ├── present_files - 展示输出文件
│   ├── view_image    - 查看图片
│   └── ask_clarification - 请求澄清
│
├── MCP Tools（MCP 工具）
│   └── 动态发现和加载
│
└── Community Tools（社区工具）
    ├── web_search  - 网页搜索
    ├── web_fetch   - 网页抓取
    └── image_search - 图片搜索
```

## 12.2　工具注册流程

```
Agent 创建时
    │
    ├── 1. 注册默认工具
    │   └── 沙箱工具 + 内置工具
    │
    ├── 2. 发现 MCP 工具
    │   ├── 读取 extensions_config.json
    │   ├── 连接 MCP 服务器
    │   └── 注册发现的工具
    │
    ├── 3. 添加 task 工具
    │   └── 如果子代理启用
    │
    └── 4. 应用 Skills 过滤
        └── 根据 allowed-tools 白名单
```

## 12.3　Sandbox Tools

### bash

**功能**：执行 shell 命令

**参数**：
- command: 要执行的命令

**返回**：标准输出 + 标准错误 + 退出码

**安全机制**：
- 命令白名单/黑名单过滤
- 虚拟路径转换
- 超时控制

### read_file

**功能**：读取文件内容

**参数**：
- path: 文件路径（虚拟路径）
- start_line: 起始行（可选）
- end_line: 结束行（可选）

**返回**：文件内容

### write_file

**功能**：写入文件

**参数**：
- path: 文件路径
- content: 写入内容
- append: 是否追加

**返回**：成功/失败

### ls

**功能**：列出目录内容

**参数**：
- path: 目录路径
- max_depth: 最大深度（默认 2）

**返回**：文件/目录列表（树形结构）

### str_replace

**功能**：替换文件内容

**参数**：
- path: 文件路径
- old_str: 要替换的内容
- new_str: 新内容
- replace_all: 是否替换全部

**返回**：成功/失败

## 12.4　Built-in Tools

### present_files

**功能**：展示输出文件给用户

**参数**：
- paths: 文件路径列表

**限制**：只能展示 `/mnt/user-data/outputs/` 下的文件

**副作用**：设置 StateUpdates["artifacts"]

### view_image

**功能**：查看图片（多模态）

**参数**：
- path: 图片路径

**处理**：
1. 读取图片文件
2. 转为 base64
3. 检测 MIME 类型
4. 设置 StateUpdates["viewed_images"]

### ask_clarification

**功能**：请求用户澄清

**参数**：
- question: 问题内容
- options: 选项列表（可选）

**处理**：被 ClarificationMiddleware 拦截，不实际执行

### task

**功能**：委托给子代理

详见第 10 章

## 12.5　MCP Tools

### 发现机制

```
BuildDiscoveredMCPTools(config)
    │
    ├── 遍历 extensions_config.json 中的 mcpServers
    │
    ├── 对每个启用的服务器：
    │   ├── 根据类型连接（stdio/sse/http）
    │   ├── 调用 tools/list 获取工具列表
    │   └── 为每个工具创建包装器
    │
    └── 返回工具列表
```

### 缓存策略

```
首次加载 → 缓存工具列表
    │
    ├── 检测配置文件 mtime 变化
    │
    └── 变化时重新加载
```

## 12.6　工具过滤

### Skills 过滤

```
Skills 定义 allowed-tools
    │
    └── 只保留白名单中的工具
```

### Tool Groups 过滤

```
Agent 配置 tool_groups: ["web", "search"]
    │
    └── 只保留这些组的工具
```

## 12.7　工具组装最终流程

```
buildTools(ctx, cfg)
    │
    ├── 1. 默认工具（始终包含）
    │
    ├── 2. MCP 工具（动态）
    │
    ├── 3. task 工具（条件）
    │   └── subagent_enabled = true
    │
    ├── 4. view_image 工具（条件）
    │   └── model.supports_vision = true
    │
    └── 5. 应用过滤
```

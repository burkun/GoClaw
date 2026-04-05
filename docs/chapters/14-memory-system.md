# 第 14 章　长期记忆系统

## 14.1　设计目标

让 Agent 在跨会话中保持对用户的了解：
- 用户画像：工作背景、偏好
- 时间线：近期动态、长期背景
- 事实库：离散的事实点

## 14.2　数据结构

### Memory 层次

```
Memory
├── UserContext           # 用户上下文
│   ├── WorkContext       # 工作背景
│   ├── PersonalContext   # 个人背景
│   └── TopOfMind         # 当前关注点
│
├── History               # 历史记录
│   ├── RecentMonths      # 近期动态
│   ├── EarlierContext    # 较早背景
│   └── LongTermBackground # 长期背景
│
└── Facts[]               # 事实库
    ├── ID
    ├── Content
    ├── Category          # preference/knowledge/context/behavior/goal
    ├── Confidence        # 0-1 置信度
    └── CreatedAt
```

## 14.3　存储机制

### 文件存储

```
存储路径：.goclaw/memory.json

格式：JSON
操作：原子写入（临时文件 + rename）
```

### 加载缓存

```
Load()
    │
    ├── 检查内存缓存
    │   └── 存在 → 直接返回
    │
    └── 不存在
        ├── 读取文件
        ├── 解析 JSON
        └── 缓存到内存
```

## 14.4　记忆注入

### Before 钩子逻辑

```
MemoryMiddleware.Before
    │
    ├── 检查注入是否启用
    │
    ├── 加载记忆
    │
    ├── 获取 Top N 事实
    │   └── 按置信度排序，取前 15
    │
    ├── 格式化为文本
    │   └── <memory> 标签包裹
    │
    └── 注入到系统提示词
```

### 注入格式

```
<memory>
<work_context>软件工程师，专注于后端开发</work_context>
<top_of_mind>正在准备技术架构评审</top_of_mind>
<facts>
- 偏好使用 Python
- 熟悉 PostgreSQL
- 最近在学习 Rust
</facts>
</memory>
```

## 14.5　记忆更新

### 触发时机

After 钩子，每次模型调用后

### 更新队列

```
UpdateQueue
├── debounce: 30s         # 防抖延迟
├── queue: []UpdateRequest # 待处理队列
└── extractor: LLMExtractor # LLM 提取器
```

### 防抖机制

```
QueueUpdate(threadID, messages, response)
    │
    ├── 移除同线程的旧请求
    │   └── 只保留最新
    │
    ├── 添加新请求
    │
    └── 等待 debounce 时间后处理
```

### 批处理

```
processLoop()
    │
    ├── 每 30s 检查队列
    │
    └── 处理所有待处理请求
        ├── 调用 LLM 提取更新
        ├── 应用更新到 Memory
        └── 原子保存
```

## 14.6　LLM 提取

### 提取流程

```
Extract(ctx, messages, response)
    │
    ├── 构建提取提示词
    │   ├── 现有记忆
    │   └── 新对话内容
    │
    ├── 调用 LLM
    │
    └── 解析响应
        ├── 更新的上下文
        └── 新事实列表
```

### 事实去重

```
应用新事实时：
    │
    ├── 检查内容是否已存在
    │   └── 空白字符归一化后比较
    │
    └── 不存在才添加
```

## 14.7　配置项

| 配置 | 默认值 | 说明 |
|------|-------|------|
| enabled | true | 是否启用 |
| injection_enabled | true | 是否注入到提示词 |
| storage_path | .goclaw/memory.json | 存储路径 |
| debounce_seconds | 30 | 防抖秒数 |
| max_facts | 100 | 最大事实数 |
| fact_confidence_threshold | 0.7 | 事实置信度阈值 |
| max_injection_tokens | 2000 | 最大注入 token 数 |

## 14.8　事实生命周期

```
事实产生
    │
    ├── LLM 从对话提取
    │
    ├── 设置置信度
    │
    └── 添加到 Facts

事实存储
    │
    ├── 超过 max_facts → 淘汰低置信度
    │
    └── 保存到文件

事实使用
    │
    ├── 加载记忆
    │
    ├── 按置信度排序
    │
    └── 取 Top N 注入
```

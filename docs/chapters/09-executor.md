# 第 9 章　Executor 执行引擎

## 9.1　职责

Executor 是 Sub-Agent 系统的核心调度器：
- 管理任务队列
- 控制并发数量
- 执行超时控制
- 回调事件通知

## 9.2　设计

### 核心结构

```
Executor
├── cfg: ExecutorConfig          # 配置
├── sem: channel                  # 信号量（并发控制）
├── tasks: map[taskID]*record     # 任务记录
└── callbacks: map[taskID][]func  # 事件回调
```

### 配置项

| 配置 | 默认值 | 说明 |
|------|-------|------|
| MaxConcurrent | 3 | 最大并发数 |
| DefaultTimeout | 15m | 默认超时 |

## 9.3　任务提交流程

```
Submit(ctx, request)
    │
    ├── 1. 生成 TaskID
    │
    ├── 2. 创建任务记录
    │   ├── 状态：pending
    │   └── 创建时间
    │
    ├── 3. 存入 tasks map
    │
    ├── 4. 发送 task_started 事件
    │
    ├── 5. 异步执行
    │   └── go execute(taskID, request)
    │
    └── 6. 返回 TaskResult
```

## 9.4　执行流程

```
execute(taskID, request)
    │
    ├── 1. 获取信号量
    │   └── 阻塞直到有空位
    │
    ├── 2. 设置超时
    │   └── ctx, cancel := WithTimeout(...)
    │
    ├── 3. 更新状态
    │   └── status = in_progress
    │
    ├── 4. 发送 task_running 事件
    │
    ├── 5. 执行 Worker
    │   ├── 获取 Sub-Agent 定义
    │   ├── 创建 Agent 实例
    │   └── 执行任务
    │
    ├── 6. 处理结果
    │   ├── 成功 → status = completed
    │   ├── 错误 → status = failed
    │   └── 超时 → status = timed_out
    │
    ├── 7. 发送完成事件
    │
    └── 8. 释放信号量
```

## 9.5　并发控制原理

```
信号量模式：

sem = make(chan struct{}, 3)  // 容量为 3

任务 1 到达 ──► sem ← struct{}{} ──► 执行
任务 2 到达 ──► sem ← struct{}{} ──► 执行
任务 3 到达 ──► sem ← struct{}{} ──► 执行
任务 4 到达 ──► sem ← ... 阻塞 ...
任务 5 到达 ──► sem ← ... 阻塞 ...

任务 1 完成 ──► <-sem ──► 释放
任务 4 开始执行
```

## 9.6　事件回调机制

### 注册回调

```
RegisterCallback(taskID, callback)
    │
    ├── 存储回调函数
    │
    └── 返回回调 ID（用于取消）
```

### 触发回调

```
emitEvent(taskID, event)
    │
    ├── 获取该任务的所有回调
    │
    └── 异步调用每个回调
```

## 9.7　任务查询

| 方法 | 说明 |
|------|------|
| Get(taskID) | 获取单个任务结果 |
| List() | 列出所有任务 |
| Cancel(taskID) | 取消正在执行的任务 |

## 9.8　超时处理

```
WithTimeout 场景：

任务执行中
    │
    ├── 正常完成 → 返回结果
    │
    └── 超时触发
        ├── ctx.Done() 被触发
        ├── 状态更新为 timed_out
        └── 返回超时错误
```

## 9.9　错误隔离

单个子代理失败不影响主流程：

```
子代理执行失败
    │
    ├── 记录错误信息
    │
    ├── 更新状态为 failed
    │
    ├── 发送 task_failed 事件
    │
    └── 返回结构化错误
        └── Lead Agent 根据错误决定下一步
```

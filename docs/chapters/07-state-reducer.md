# 第 7 章　State 与 Reducer

## 7.1　State 设计理念

State 是中间件之间共享数据的核心载体。它贯穿整个 Agent 执行过程，各中间件读取和修改它来协调工作。

### State 与 ThreadState 的关系

```
ThreadState (持久化层)
    │
    │ 运行时转换
    ▼
State (中间件层)
    │
    │ 中间件处理
    ▼
Response (输出层)
    │
    │ Reducer 合并
    ▼
更新后的 ThreadState
```

### 职责分离

| 结构 | 层级 | 生命周期 | 用途 |
|------|------|---------|------|
| ThreadState | 持久化 | 跨会话 | 状态存储和恢复 |
| State | 运行时 | 单次运行 | 中间件协作 |
| Response | 输出 | 单次迭代 | 结果传递 |

## 7.2　State 字段分类

### 基础字段

| 字段 | 类型 | 来源 |
|------|------|------|
| ThreadID | string | 请求参数 |
| Messages | []map | ThreadState 转换 |
| PlanMode | bool | RunConfig |

### 中间件注入字段

| 字段 | 注入中间件 | 用途 |
|------|-----------|------|
| MemoryFacts | MemoryMiddleware | 注入到系统提示词 |
| Todos | TodoMiddleware | 计划任务管理 |
| TokenCount | TokenUsageMiddleware | 上下文压缩决策 |
| ViewedImages | ViewImageMiddleware | 多模态内容 |
| Artifacts | 各工具 | 输出文件列表 |
| Title | TitleMiddleware | 对话标题 |

### 扩展字段

| 字段 | 存储内容 |
|------|---------|
| Extra["sandbox_id"] | 沙箱 ID |
| Extra["thread_data"] | 目录路径 |
| Extra["uploaded_files"] | 上传文件列表 |
| Extra["agent_name"] | Agent 名称 |
| Extra["is_subagent"] | 是否子代理 |

## 7.3　Reducer 机制

### 为什么需要 Reducer

多个工具可能同时产生相同字段的状态更新：

```
工具 A 执行 → 返回 artifacts: ["file1.md"]
工具 B 执行 → 返回 artifacts: ["file2.md"]

需要合并 → ["file1.md", "file2.md"]
```

### Reducer 定义

```
Reducer(existing, new) → merged

输入：
  existing - 当前值
  new - 待合并值

输出：
  merged - 合并后的值
```

## 7.4　内置 Reducer

### MergeArtifacts

**用途**：合并输出文件列表

**逻辑**：
1. 合并两个列表
2. 去重（保持首次出现顺序）
3. 返回新列表

**示例**：
```
现有: ["report.md", "data.json"]
新增: ["summary.md", "report.md"]  // report.md 重复

结果: ["report.md", "data.json", "summary.md"]
```

### MergeViewedImages

**用途**：合并图片字典

**逻辑**：
1. 新值覆盖旧值（相同 key）
2. 特殊：空 map 表示清空所有

**示例**：
```
现有: {"img1.png": {...}}
新增: {"img2.png": {...}}

结果: {"img1.png": {...}, "img2.png": {...}}

新增: {}  // 空 map
结果: {}  // 清空
```

## 7.5　StateUpdates 产生流程

### 工具产生更新

```
工具执行
    │
    ├── 执行工具逻辑
    │
    ├── 需要更新状态
    │   └── 设置 StateUpdates
    │       例如: {"artifacts": ["/mnt/user-data/outputs/report.md"]}
    │
    └── 返回 ToolResult
```

### 中间件处理更新

```
After 钩子
    │
    ├── 读取 response.StateUpdates
    │
    ├── 应用 Reducer
    │   ├── MergeArtifacts(state.Artifacts, updates["artifacts"])
    │   └── MergeViewedImages(state.ViewedImages, updates["viewed_images"])
    │
    └── 更新 State
```

## 7.6　ApplyReducers 流程

```
ApplyReducers(state, pendingUpdates)
    │
    ├── 遍历 pendingUpdates
    │
    ├── 对每个字段：
    │   ├── 检查是否有对应 Reducer
    │   │   ├── artifacts → MergeArtifacts
    │   │   └── viewed_images → MergeViewedImages
    │   │
    │   └── 调用 Reducer 合并
    │
    └── 更新 state 字段
```

## 7.7　自定义 Reducer

### 设计考虑

1. **幂等性**：重复应用不产生副作用
2. **交换性**：合并顺序不影响结果
3. **类型安全**：处理类型不匹配情况

### 注册方式

在中间件的 After 钩子中手动应用：

```
After(ctx, state, response)
    │
    ├── 获取更新值
    │   value := response.StateUpdates["custom_field"]
    │
    ├── 应用自定义 Reducer
    │   state.CustomField = MyReducer(state.CustomField, value)
    │
    └── 返回
```

## 7.8　状态流转完整示例

```
1. 用户上传文件 report.pdf
   │
   ▼
2. UploadsMiddleware 设置 state.UploadedFiles
   │
   ▼
3. 用户请求生成报告
   │
   ▼
4. Agent 执行工具
   │   ├── write_file("report.md", ...)
   │   └── present_files(["/mnt/user-data/outputs/report.md"])
   │
   ▼
5. present_files 返回 StateUpdates
   │   └── {"artifacts": [...]}
   │
   ▼
6. After 钩子应用 Reducer
   │   └── state.Artifacts = MergeArtifacts(...)
   │
   ▼
7. 响应返回客户端
   │   └── artifacts 包含在响应中
```

# 第 11 章　Sandbox 抽象层

## 11.1　设计理念

Sandbox 提供隔离的执行环境，让 Agent 能够安全地执行命令和操作文件。

**核心原则**：
- **虚拟路径**：Agent 只看到虚拟路径，实际映射到物理路径
- **Provider 模式**：统一管理沙箱生命周期
- **线程隔离**：每个线程独立的文件空间

**为什么需要沙箱**：

| 风险 | 无沙箱 | 有沙箱 |
|------|--------|--------|
| 删除系统文件 | 可能执行 `rm -rf /` | 只能操作虚拟目录 |
| 访问敏感数据 | 可读取 ~/.ssh/ | 完全隔离 |
| 恶意命令 | 无限制 | 命令白名单/黑名单 |
| 资源滥用 | 无限制 | CPU/内存/网络限制 |

## 11.2　虚拟路径系统

### 路径映射

```
Agent 视角                    实际物理路径
────────────────────────────────────────────────────
/mnt/user-data/workspace/ → .goclaw/threads/{id}/workspace/
/mnt/user-data/uploads/   → .goclaw/threads/{id}/uploads/
/mnt/user-data/outputs/   → .goclaw/threads/{id}/outputs/
/mnt/skills/              → skills/public/
```

### 路径转换逻辑

```
虚拟路径 → 物理路径
    │
    ├── 检查前缀
    │   ├── /mnt/user-data/ → 替换为线程目录
    │   └── /mnt/skills/ → 替换为 skills 目录
    │
    └── 安全验证
        ├── 检查路径是否在允许范围内
        └── 检测路径遍历攻击 (..)
```

## 11.3　Sandbox 接口

### 核心能力

| 方法 | 说明 |
|------|------|
| ID() | 返回沙箱唯一标识 |
| Execute(command) | 执行 shell 命令 |
| ReadFile(path, start, end) | 读取文件（支持行范围） |
| WriteFile(path, content, append) | 写入文件 |
| ListDir(path, depth) | 列出目录内容 |
| StrReplace(path, old, new, all) | 替换文件内容 |
| Glob(path, pattern) | 匹配文件路径 |
| Grep(path, pattern) | 搜索文件内容 |
| UpdateFile(path, bytes) | 写入二进制内容 |

### ExecuteResult 结构

| 字段 | 说明 |
|------|------|
| Stdout | 标准输出 |
| Stderr | 标准错误 |
| ExitCode | 退出码（0=成功） |
| Error | 系统级错误（区别于非零退出码） |

## 11.4　SandboxProvider 接口

### 生命周期管理

| 方法 | 说明 |
|------|------|
| Acquire(threadID) | 获取或创建沙箱 |
| Get(sandboxID) | 获取已有沙箱 |
| Release(sandboxID) | 释放沙箱 |
| Shutdown() | 关闭所有沙箱 |

### 获取逻辑

```
Acquire(threadID)
    │
    ├── 检查是否已存在（线程 ID 映射）
    │   └── 存在 → 返回现有沙箱 ID
    │
    ├── 不存在 → 创建新沙箱
    │   ├── 生成唯一 ID
    │   ├── 创建目录结构
    │   └── [Docker 模式] 启动容器
    │
    └── 存入缓存，返回 ID
```

## 11.5　Local Sandbox

### 特点

- 直接在主机文件系统执行
- 轻量级，无额外开销
- 依赖命令白名单保证安全

### 命令过滤逻辑

```
Execute(command)
    │
    ├── 检查黑名单 (denied_commands)
    │   └── 匹配 → 返回拒绝错误
    │
    ├── 检查白名单 (allowed_commands)
    │   └── 不匹配 → 返回拒绝错误
    │
    ├── 转换命令中的虚拟路径
    │
    └── 执行命令，收集输出
```

### 安全验证

```
validatePath(physicalPath)
    │
    ├── 转换为绝对路径
    │
    ├── 检查是否在用户数据目录内
    │   └── 不在 → 拒绝
    │
    └── 检查是否包含 ".."
        └── 包含 → 拒绝（路径遍历攻击）
```

## 11.6　Docker Sandbox

### 特点

- 完全隔离的容器环境
- 资源限制（CPU/内存/网络）
- 更强的安全边界

### 容器创建流程

```
Acquire(threadID)
    │
    ├── 生成容器名称
    │
    ├── 构建挂载点
    │   ├── 线程目录 → /mnt/user-data (读写)
    │   ├── Skills 目录 → /mnt/skills (只读)
    │   └── 额外挂载点
    │
    ├── 创建容器
    │   ├── 设置镜像
    │   ├── 设置资源限制
    │   └── 设置网络模式
    │
    └── 启动容器
```

### 命令执行流程

```
Execute(command)
    │
    ├── 创建 exec 实例
    │
    ├── 附加到 exec
    │
    ├── 读取输出
    │
    └── 获取退出码
```

### 资源配置

| 配置项 | 说明 |
|--------|------|
| Image | Docker 镜像 |
| CPUQuota | CPU 限制（微秒/100ms） |
| MemoryBytes | 内存限制 |
| NetworkDisabled | 是否禁用网络 |
| ContainerTTL | 空闲容器存活时间 |
| Replicas | 最大容器数 |

## 11.7　沙箱选择策略

### Local 模式适用

- 开发环境
- 信任的工作负载
- 需要最佳性能
- 无 Docker 环境

### Docker 模式适用

- 生产环境
- 不信任的工作负载
- 需要资源限制
- 多租户场景

## 11.8　安全最佳实践

### 命令过滤

```
推荐黑名单：
- rm -rf /
- sudo / su
- chmod 777
- dd if=
- mkfs / fdisk

推荐白名单：
- ls, cat, head, tail
- grep, find
- python, node, go
```

### 路径安全

- 始终转换为绝对路径
- 检查路径前缀
- 检测路径遍历

### 资源限制

```
Docker 模式推荐：
- CPU: 1 核
- 内存: 512MB
- 网络: 禁用
- 超时: 30s
```

## 11.9　全局 Provider

进程级别的沙箱提供者，供工具层使用：

```
启动时：
    SetDefaultProvider(provider)

工具使用：
    sandbox := DefaultProvider().Get(sandboxID)
    result := sandbox.Execute(ctx, command)
```

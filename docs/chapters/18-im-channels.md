# 第 18 章　IM 渠道系统

## 18.1　架构设计

```
外部平台                    内部处理
─────────────────────────────────────────────
Telegram ──┐
           │     ┌──────────┐     ┌─────────┐
Slack     ──┼────►│MessageBus│────►│ Channel │────► Agent
           │     └──────────┘     │ Manager │
Feishu    ──┘                      └─────────┘
```

### 组件职责

| 组件 | 职责 |
|------|------|
| Channel | 平台适配（收发消息） |
| MessageBus | 消息发布/订阅 |
| ChannelManager | 调度与线程管理 |

## 18.2　Channel 接口

### 核心方法

| 方法 | 说明 |
|------|------|
| Start(ctx) | 启动渠道 |
| Stop(ctx) | 停止渠道 |
| Name() | 渠道名称 |
| Send(ctx, chatID, message) | 发送消息 |

### InboundMessage 结构

| 字段 | 说明 |
|------|------|
| Channel | 渠道名称 |
| ChatID | 聊天 ID |
| TopicID | 话题 ID（可选） |
| UserID | 用户 ID |
| Content | 消息内容 |
| Timestamp | 时间戳 |

## 18.3　MessageBus

### 发布/订阅

```
PublishInbound(msg)
    └── 写入 inbound channel

SubscribeInbound()
    └── 返回 inbound channel 只读副本

PublishOutbound(msg)
    └── 写入 outbound channel，触发回调
```

### 回调注册

```
RegisterOutboundHandler(channel, handler)
    │
    └── 存储到 handlers[channel]
```

## 18.4　ChannelManager

### 启动流程

```
Start(ctx)
    │
    ├── 启动所有已配置渠道
    │   │
    │   └── 对每个渠道：
    │       ├── 调用 channel.Start()
    │       └── 注册出站处理器
    │
    └── 启动调度循环
        └── go dispatchLoop()
```

### 调度循环

```
dispatchLoop()
    │
    ├── 监听 inbound channel
    │
    └── 收到消息 → handleInbound()
```

## 18.5　消息处理

### 命令处理

| 命令 | 功能 |
|------|------|
| /new | 创建新线程 |
| /status | 显示当前线程 |
| /models | 列出模型 |
| /memory | 查看记忆 |
| /help | 帮助信息 |

### 对话处理

```
handleInbound(msg)
    │
    ├── 检查是否是命令
    │   └── 是 → handleCommand()
    │
    ├── 获取或创建线程
    │   └── store.GetThreadID(key)
    │
    ├── 构建 ThreadState
    │
    ├── 调用 Agent
    │   └── agent.Run(ctx, state, cfg)
    │
    └── 发送响应
        └── bus.PublishOutbound()
```

## 18.6　平台特性

### Telegram

- 传输：Bot API 长轮询
- 权限：allowed_users 白名单

### Slack

- 传输：Socket Mode
- 权限：allowed_users 白名单

### Feishu

- 传输：WebSocket
- 特点：支持增量更新（非最终消息也可更新卡片）

## 18.7　线程映射

### 存储结构

```
key: "{channel}:{chatID}" 或 "{channel}:{chatID}:{topicID}"
value: threadID
```

### 映射逻辑

```
buildThreadKey(msg)
    │
    ├── 无 topicID
    │   └── "{channel}:{chatID}"
    │
    └── 有 topicID
        └── "{channel}:{chatID}:{topicID}"
```

## 18.8　配置示例

```
channels:
  telegram:
    enabled: true
    bot_token: $TELEGRAM_BOT_TOKEN
    
  slack:
    enabled: true
    bot_token: $SLACK_BOT_TOKEN
    app_token: $SLACK_APP_TOKEN
    
  feishu:
    enabled: true
    app_id: $FEISHU_APP_ID
    app_secret: $FEISHU_APP_SECRET
```

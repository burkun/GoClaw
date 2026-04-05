# 第 3 章　快速上手

## 3.1　环境准备

**系统要求**：
- Go 1.21+
- Docker（可选，用于 Docker 沙箱模式）

**验证环境**：

```bash
go version
# go version go1.22.x darwin/amd64
```

## 3.2　安装与配置

### 1. 克隆项目

```bash
git clone https://github.com/bookerbai/goclaw.git
cd goclaw
```

### 2. 创建配置文件

```bash
cp config.yaml.example config.yaml  # 如果有示例
# 或直接编辑 config.yaml
```

### 3. 最小配置

创建 `config.yaml`：

```yaml
config_version: 1
log_level: info

server:
  address: ":8001"

models:
  - name: gpt-4
    display_name: GPT-4
    use: openai
    model: gpt-4
    api_key: $OPENAI_API_KEY
    max_tokens: 4096

sandbox:
  type: local
  work_dir: ./.goclaw/sandbox

memory:
  enabled: false

title:
  enabled: false

summarization:
  enabled: false
```

### 4. 设置 API Key

```bash
export OPENAI_API_KEY=your-api-key
```

## 3.3　启动服务

```bash
go run ./cmd/goclaw
```

输出示例：

```
[INFO] loaded agent: default
[INFO] loaded 1 agents: [default]
goclaw gateway listening on :8001
```

## 3.4　验证运行

### 检查健康状态

```bash
curl http://localhost:8001/health
# {"status":"ok"}
```

### 查看模型列表

```bash
curl http://localhost:8001/api/models
# {"models":[{"name":"gpt-4","display_name":"GPT-4",...}]}
```

### 发送对话请求

```bash
curl -X POST http://localhost:8001/api/threads/test-thread/runs \
  -H "Content-Type: application/json" \
  -d '{"input": "Hello, who are you?"}'
```

响应（SSE 流）：

```
data: {"type":"message_delta","thread_id":"test-thread","payload":{"content":"I am","is_thinking":false},"timestamp":...}

data: {"type":"message_delta","thread_id":"test-thread","payload":{"content":" GoClaw","is_thinking":false},"timestamp":...}

data: {"type":"completed","thread_id":"test-thread","payload":{"final_message":"I am GoClaw, an AI agent."},"timestamp":...}
```

## 3.5　开发流程

### 运行测试

```bash
# 运行所有测试
go test ./...

# 运行特定包的测试
go test ./internal/middleware/...

# 运行带覆盖率的测试
go test -cover ./...
```

### 代码检查

```bash
# 安装 golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# 运行 lint
golangci-lint run
```

### 添加新中间件

1. 在 `internal/middleware/` 下创建新目录
2. 实现 Middleware 接口
3. 在 `internal/agent/lead.go` 中注册

```go
// internal/middleware/my_middleware/middleware.go
package mymiddleware

type MyMiddleware struct {
    middleware.MiddlewareWrapper
}

func (m *MyMiddleware) Name() string {
    return "my_middleware"
}

func (m *MyMiddleware) Before(ctx context.Context, state *middleware.State) error {
    // 在模型调用前执行
    return nil
}

func (m *MyMiddleware) After(ctx context.Context, state *middleware.State, response *middleware.Response) error {
    // 在模型调用后执行
    return nil
}
```

## 3.6　常见问题

### Q: 配置文件找不到？

确保 `config.yaml` 在工作目录下，或设置环境变量：

```bash
export GOCRAW_CONFIG_PATH=/path/to/config.yaml
```

### Q: API Key 无效？

确保环境变量已设置，或在配置文件中直接指定（不推荐）：

```yaml
models:
  - name: gpt-4
    api_key: "your-actual-key"  # 不推荐明文存储
```

### Q: 端口被占用？

修改 `config.yaml` 中的端口：

```yaml
server:
  address: ":8080"
```

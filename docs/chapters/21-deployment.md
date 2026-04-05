# 第 21 章　部署与生产化

## 21.1　构建

### 本地构建

```bash
# 构建
go build -o goclaw ./cmd/goclaw

# 带版本信息
go build -ldflags "-X main.Version=$(git describe --tags)" -o goclaw ./cmd/goclaw
```

### 交叉编译

```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o goclaw-linux ./cmd/goclaw

# macOS
GOOS=darwin GOARCH=amd64 go build -o goclaw-darwin ./cmd/goclaw
GOOS=darwin GOARCH=arm64 go build -o goclaw-darwin-arm64 ./cmd/goclaw

# Windows
GOOS=windows GOARCH=amd64 go build -o goclaw.exe ./cmd/goclaw
```

## 21.2　Docker 部署

### Dockerfile

```dockerfile
# 构建阶段
FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o goclaw ./cmd/goclaw

# 运行阶段
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/goclaw .
COPY config.yaml .

EXPOSE 8001

CMD ["./goclaw"]
```

### 构建与运行

```bash
# 构建镜像
docker build -t goclaw:latest .

# 运行
docker run -d \
  --name goclaw \
  -p 8001:8001 \
  -e OPENAI_API_KEY=your-key \
  -v $(pwd)/.goclaw:/app/.goclaw \
  goclaw:latest
```

## 21.3　Docker Compose

```yaml
# docker-compose.yml
version: '3.8'

services:
  goclaw:
    build: .
    ports:
      - "8001:8001"
    environment:
      - OPENAI_API_KEY=${OPENAI_API_KEY}
    volumes:
      - ./config.yaml:/app/config.yaml
      - goclaw-data:/app/.goclaw
    restart: unless-stopped

  # Docker 沙箱模式
  docker-proxy:
    image: docker:dind
    privileged: true
    volumes:
      - goclaw-docker:/var/lib/docker

volumes:
  goclaw-data:
  goclaw-docker:
```

## 21.4　Kubernetes 部署

### Deployment

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: goclaw
spec:
  replicas: 1
  selector:
    matchLabels:
      app: goclaw
  template:
    metadata:
      labels:
        app: goclaw
    spec:
      containers:
      - name: goclaw
        image: goclaw:latest
        ports:
        - containerPort: 8001
        env:
        - name: OPENAI_API_KEY
          valueFrom:
            secretKeyRef:
              name: goclaw-secrets
              key: openai-api-key
        volumeMounts:
        - name: config
          mountPath: /app/config.yaml
          subPath: config.yaml
        - name: data
          mountPath: /app/.goclaw
        resources:
          requests:
            memory: "256Mi"
            cpu: "100m"
          limits:
            memory: "512Mi"
            cpu: "500m"
      volumes:
      - name: config
        configMap:
          name: goclaw-config
      - name: data
        persistentVolumeClaim:
          claimName: goclaw-pvc
```

### Service

```yaml
# service.yaml
apiVersion: v1
kind: Service
metadata:
  name: goclaw
spec:
  selector:
    app: goclaw
  ports:
  - port: 80
    targetPort: 8001
  type: LoadBalancer
```

## 21.5　配置管理

### Secret

```yaml
# secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: goclaw-secrets
type: Opaque
stringData:
  openai-api-key: your-api-key
  telegram-bot-token: your-bot-token
```

### ConfigMap

```yaml
# configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: goclaw-config
data:
  config.yaml: |
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
    # ...
```

## 21.6　健康检查

### Liveness Probe

```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 8001
  initialDelaySeconds: 5
  periodSeconds: 10
```

### Readiness Probe

```yaml
readinessProbe:
  httpGet:
    path: /health
    port: 8001
  initialDelaySeconds: 5
  periodSeconds: 5
```

## 21.7　日志配置

```yaml
# config.yaml
log_level: info  # debug, info, warn, error
```

### 结构化日志

```go
import "log/slog"

func setupLogger(level string) {
    var lvl slog.Level
    switch level {
    case "debug":
        lvl = slog.LevelDebug
    case "info":
        lvl = slog.LevelInfo
    case "warn":
        lvl = slog.LevelWarn
    case "error":
        lvl = slog.LevelError
    default:
        lvl = slog.LevelInfo
    }

    logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
        Level: lvl,
    }))

    slog.SetDefault(logger)
}
```

## 21.8　监控

### Prometheus 指标

```go
import "github.com/prometheus/client_golang/prometheus"

var (
    requestDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "goclaw_request_duration_seconds",
            Help:    "Request duration in seconds",
            Buckets: prometheus.DefBuckets,
        },
        []string{"method", "path", "status"},
    )

    activeConnections = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "goclaw_active_connections",
            Help: "Number of active connections",
        },
    )
)

func init() {
    prometheus.MustRegister(requestDuration)
    prometheus.MustRegister(activeConnections)
}
```

### 指标端点

```go
import "github.com/prometheus/client_golang/prometheus/promhttp"

func (s *Server) registerRoutes() {
    // ...

    // Prometheus 指标
    s.router.GET("/metrics", gin.WrapH(promhttp.Handler()))
}
```

## 21.9　安全配置

### TLS

```go
func (s *Server) RunTLS(addr, certFile, keyFile string) error {
    return s.router.RunTLS(addr, certFile, keyFile)
}
```

### 速率限制

```go
import "github.com/ulule/limiter/v3"

func (s *Server) registerMiddleware() {
    // ...

    // 速率限制
    store := memory.NewStore()
    rate := limiter.Rate{
        Period: 1 * time.Minute,
        Limit:  60,
    }
    instance := limiter.New(store, rate)

    s.router.Use(middleware.NewLimiter(instance))
}
```

### 认证

```go
func AuthMiddleware(apiKeys []string) gin.HandlerFunc {
    return func(c *gin.Context) {
        apiKey := c.GetHeader("X-API-Key")
        if apiKey == "" {
            apiKey = c.Query("api_key")
        }

        valid := false
        for _, key := range apiKeys {
            if apiKey == key {
                valid = true
                break
            }
        }

        if !valid {
            c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
            return
        }

        c.Next()
    }
}
```

## 21.10　生产清单

- [ ] 配置日志级别为 `info` 或 `warn`
- [ ] 启用 TLS
- [ ] 配置速率限制
- [ ] 设置 API Key 认证
- [ ] 配置健康检查
- [ ] 设置资源限制
- [ ] 配置持久化存储
- [ ] 启用监控指标
- [ ] 配置备份策略
- [ ] 审查沙箱安全配置

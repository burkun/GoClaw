# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

GoClaw is a Go-based AI Agent Harness built on the CloudWeGo Eino framework. It is a Go reimplementation of DeerFlow, providing:
- Multi-turn conversation with tool execution (OpenAI, Anthropic, Azure backends)
- Sandbox-isolated command execution (Local/Docker modes)
- 18-layer middleware pipeline (memory, title generation, planning, guardrails)
- Sub-agent task delegation with concurrent execution
- SSE streaming compatible with LangGraph API format
- MCP tool ecosystem integration (stdio/SSE/HTTP transports)

**Why Go**: Single binary deployment (~50MB vs ~1GB Python), millisecond startup, goroutine concurrency, compile-time type safety.

## Build and Test Commands

```bash
# Build
go build -o goclaw ./cmd/goclaw

# Run server
go run ./cmd/goclaw

# Run all tests
go test ./...

# Run tests for a specific package
go test ./internal/middleware/...

# Run tests with coverage
go test -cover ./...

# Run a single test
go test -run TestFunctionName ./path/to/package

# Lint
golangci-lint run
```

## Documentation (VitePress)

The `docs/` directory contains a VitePress-based documentation site with detailed source code analysis:

```bash
cd docs

# Install dependencies
npm install

# Start dev server
npm run docs:dev

# Build for production
npm run docs:build

# Preview production build
npm run docs:preview
```

Documentation structure:
- Part 1 (宏观认知): Project overview, structure, quick start
- Part 2 (核心引擎): Eino framework, Lead Agent, middleware, State/Reducer
- Part 3 (Sub-Agent): Architecture, Executor, Task Tool
- Part 4 (沙箱与工具): Sandbox abstraction, tools system, MCP integration
- Part 5 (记忆与上下文): Memory system, context engineering
- Part 6 (Gateway与通信): HTTP API, SSE protocol, IM channels
- Part 7 (配置与部署): Config system, model config, deployment

## Configuration

- Main config: `config.yaml` (set via `GOCRAW_CONFIG_PATH` env var, defaults to `./config.yaml`)
- Environment variables in config use `$VAR_NAME` syntax for automatic expansion
- Multi-agent configs: `.goclaw/agents/{agent_name}/config.yaml` with optional `SOUL.md` for personality
- Extensions config: `extensions_config.json` for MCP servers and skills

## Architecture

### Dependency Graph

```
pkg/gateway → internal/agent → internal/middleware → internal/tools → internal/sandbox → internal/config
                                  ↓
                           internal/subagents
```

- `internal/`: Private modules (agent, middleware, sandbox, tools, skills, channels, subagents)
- `pkg/`: Public modules (gateway HTTP API)
- `cmd/goclaw/`: Application entry point

### Execution Flow

```
User Request → Gateway → LeadAgent
                          │
                          ├── BeforeAgent hooks (all middlewares, sequential)
                          │   ├── ThreadData: create thread directories
                          │   ├── Sandbox: acquire sandbox instance
                          │   └── Memory: load memory
                          │
                          ├── Iteration Loop
                          │   ├── BeforeModel hooks (per model call)
                          │   ├── Model call
                          │   ├── Tool execution (via WrapToolCall)
                          │   └── AfterModel hooks (per model call, reverse order)
                          │
                          ├── AfterAgent hooks (all middlewares, reverse order)
                          │   ├── Memory: trigger async update
                          │   └── Sandbox: release sandbox
                          │
                          └── SSE Event Stream → Client
```

### Key Components

**LeadAgent** (`internal/agent/lead.go`): Core orchestrator. Created via `New(ctx)` or `NewWithName(ctx, agentName)`.

**Middleware System** (`internal/middleware/middleware.go`): 18 layers, execution order:

| Order | Middleware | Hook Points | Purpose |
|-------|------------|-------------|---------|
| 1 | ThreadData | BeforeAgent | Create thread directories |
| 2 | Uploads | BeforeAgent | Inject uploaded files |
| 3 | Sandbox | BeforeAgent/AfterAgent | Acquire/release sandbox |
| 4 | DanglingToolCall | BeforeModel | Fix interrupted tool calls |
| 5 | Guardrail | WrapToolCall | Tool call policy check |
| 6 | Audit | All hooks | Audit logging |
| 7 | Summarization | BeforeModel | Context compression |
| 8 | Todo | BeforeAgent/AfterModel | Plan mode support |
| 9 | Title | AfterModel | Auto-generate title |
| 10 | Memory | BeforeModel/AfterModel | Long-term memory |
| 11 | ViewImage | BeforeModel | Image injection |
| 12 | SubagentLimit | AfterModel | Limit concurrent subagents |
| 13 | LoopDetection | AfterModel | Detect infinite loops |
| 14 | DeferredToolFilter | BeforeModel | Filter deferred tools |
| 15 | TokenUsage | AfterModel | Token statistics |
| 16 | LLMError | AfterModel | LLM error retry |
| 17 | ToolError | WrapToolCall | Tool error handling |
| 18 | Clarification | WrapToolCall | Intercept clarification requests |

**Sandbox** (`internal/sandbox/`): Virtual path system isolates agent from host:
- Agent sees: `/mnt/user-data/` (workspace/uploads/outputs), `/mnt/skills/` (read-only)
- Local mode: Per-thread directories on host, command allowlist/denylist
- Docker mode: Container isolation with CPU/memory/network limits

**Sub-Agent System** (`internal/agent/subagents/`):
- `task` tool: Delegate tasks to specialized sub-agents
- `Executor`: Concurrent scheduler with semaphore-based limiting
- Built-in agents: `general-purpose` (all tools), `bash` (shell only)
- State machine: pending → queued → in_progress → completed/failed/timed_out

**Gateway** (`pkg/gateway/`): Gin-based HTTP server:
- `POST /api/threads/:id/runs` - Create run (SSE stream)
- `POST /api/threads/:id/uploads` - Upload files
- `GET /api/models` - List models
- `GET /api/memory` - Get memory
- LangGraph compat routes for DeerFlow frontend

### State and Reducer

State is shared across middleware layers. Reducers handle merging updates from multiple sources:
- `MergeArtifacts`: Deduplicates file paths, preserves order
- `MergeViewedImages`: Merges image maps (empty map = clear all)

StateUpdates flow: Tool execution → `StateUpdates` map → After hooks apply Reducers → State updated

### Tool System

Registration order at agent creation:
1. Default sandbox tools (bash, ls, read_file, write_file, str_replace, glob, grep)
2. MCP tools (discovered from `extensions_config.json` → `tools/list`)
3. Built-in tools (task, present_files, view_image, ask_clarification, tool_search)
4. Apply Skills filtering (`allowed-tools` whitelist)

### SSE Event Protocol

Every stream terminates with `completed` or `error`:

| Event | Purpose |
|-------|---------|
| `message_delta` | Incremental text (with `is_thinking` flag) |
| `tool_event` | Two-phase: call (input only) → result (output) |
| `task_started/running/completed/failed/timed_out` | Sub-agent lifecycle |
| `completed` | Success termination |
| `error` | Failure with error code |

Event envelope: `{type, thread_id, payload, timestamp}`

### System Prompt Structure

```
<role>Agent identity</role>
<soul>Personality from SOUL.md</soul>
<skills>Enabled skills list</skills>
<memory>Injected memory facts</memory>
<filesystem>Virtual path explanations</filesystem>
<critical_reminders>Key reminders</critical_reminders>
```

## Adding New Middleware

1. Create `internal/middleware/{name}/{name}.go`
2. Implement `Middleware` interface (embed `MiddlewareWrapper` for defaults)
3. Register in `buildMiddlewares()` in `internal/agent/lead.go`
4. Consider execution order for dependencies

```go
type MyMiddleware struct{ middleware.MiddlewareWrapper }

func (m *MyMiddleware) Name() string { return "my_middleware" }
func (m *MyMiddleware) BeforeModel(ctx context.Context, state *middleware.State) error {
    // Hook logic
    return nil
}
```

## Model Configuration

```yaml
models:
  - name: gpt-4
    display_name: GPT-4
    use: openai              # openai, anthropic, azure
    model: gpt-4
    api_key: $OPENAI_API_KEY
    base_url: https://...    # optional for OpenRouter etc.
    supports_thinking: true  # extended thinking (Anthropic)
    supports_vision: true    # multimodal
    max_tokens: 4096
    temperature: 0.7
```

Model factory (`internal/models/factory.go`) creates models based on `use` field. Supports OpenAI, Anthropic, Azure, and extensible providers.

## MCP Integration

`extensions_config.json`:
```json
{
  "mcpServers": {
    "filesystem": {
      "type": "stdio",
      "command": "mcp-filesystem",
      "args": ["/home/user/docs"]
    },
    "api": {
      "type": "http",
      "url": "https://api.example.com/mcp",
      "oauth": {
        "token_url": "https://auth.example.com/token",
        "grant_type": "client_credentials",
        "client_id": "$CLIENT_ID",
        "client_secret": "$CLIENT_SECRET"
      }
    }
  }
}
```

Transport types: `stdio`, `sse`, `http`. Tools cached with mtime-based invalidation.

## Memory System

- Storage: `.goclaw/memory.json` (atomic writes)
- Injection: Top 15 facts by confidence, formatted in `<memory>` tags
- Update: Async with 30s debounce, LLM extraction from conversations
- Facts: Categories (preference/knowledge/context/behavior/goal), confidence threshold 0.7
- Config: `max_facts: 100`, `debounce_seconds: 30`

## Context Engineering

- **Summarization**: Triggered by token count or message count thresholds
- **Token counting**: Simple estimation (~4 chars = 1 token)
- **Dangling tool calls**: Fixed by adding placeholder ToolMessage
- **Image injection**: ViewImageMiddleware converts to base64 for multimodal models

## IM Channels

`internal/channels/`: Telegram (long-polling), Slack (Socket Mode), Feishu (WebSocket)

Messages flow: Channel → MessageBus → ChannelManager → Agent → Response

Thread mapping: `{channel}:{chatID}` or `{channel}:{chatID}:{topicID}`

Commands: `/new`, `/status`, `/models`, `/memory`, `/help`

## Deployment

```bash
# Build with version
go build -ldflags "-X main.Version=$(git describe --tags)" -o goclaw ./cmd/goclaw

# Cross-compile
GOOS=linux GOARCH=amd64 go build -o goclaw-linux ./cmd/goclaw

# Docker
docker build -t goclaw:latest .
docker run -d -p 8001:8001 -e OPENAI_API_KEY=your-key goclaw:latest
```

Production checklist:
- Set log level to `info` or `warn`
- Enable TLS, rate limiting, API key auth
- Configure health checks (`/health` endpoint)
- Set resource limits
- Configure persistent storage for `.goclaw/`

## Error Codes

| Prefix | Category |
|--------|----------|
| `agent/` | Agent runtime errors |
| `validation/` | Input validation errors |
| `auth/` | Authentication errors |
| `sandbox/` | Sandbox execution errors |
| `tool/` | Tool execution errors |
| `model/` | Model API errors |

## Glossary (术语表)

| Term | Definition |
|------|------------|
| Lead Agent | Main orchestrator that coordinates sub-agents and tools |
| Sub-Agent | Specialized agent delegated by Lead Agent |
| Middleware | Hook-based pipeline for injecting logic |
| Sandbox | Isolated execution environment |
| Thread | Complete conversation session |
| Checkpoint | Saved state snapshot for resumption |
| MCP | Model Context Protocol - standardized tool protocol |
| Reducer | Function for merging state updates |

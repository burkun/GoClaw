package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	lcTool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/bookerbai/goclaw/internal/agent/subagents"
	"github.com/bookerbai/goclaw/internal/config"
	einoruntime "github.com/bookerbai/goclaw/internal/eino"
	basemw "github.com/bookerbai/goclaw/internal/middleware"
	memorymw "github.com/bookerbai/goclaw/internal/middleware/memory"
	summarizemw "github.com/bookerbai/goclaw/internal/middleware/summarize"
	titlemw "github.com/bookerbai/goclaw/internal/middleware/title"
	todomw "github.com/bookerbai/goclaw/internal/middleware/todo"
	"github.com/bookerbai/goclaw/internal/models"
	skillsruntime "github.com/bookerbai/goclaw/internal/skills"
	toolruntime "github.com/bookerbai/goclaw/internal/tools"
)

type UploadedFile struct {
	Name        string `json:"name"`
	VirtualPath string `json:"virtual_path"`
	MIMEType    string `json:"mime_type,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
}

type ViewedImageData struct {
	Base64   string `json:"base64"`
	MIMEType string `json:"mime_type"`
}

type ThreadDataState struct {
	WorkspacePath string `json:"workspace_path,omitempty"`
	UploadsPath   string `json:"uploads_path,omitempty"`
	OutputsPath   string `json:"outputs_path,omitempty"`
}

type SandboxState struct {
	SandboxID string `json:"sandbox_id,omitempty"`
}

type ThreadState struct {
	Messages      []*schema.Message          `json:"messages"`
	Sandbox       *SandboxState              `json:"sandbox,omitempty"`
	ThreadData    *ThreadDataState           `json:"thread_data,omitempty"`
	Title         string                     `json:"title,omitempty"`
	Artifacts     []string                   `json:"artifacts,omitempty"`
	Todos         []map[string]any           `json:"todos,omitempty"`
	UploadedFiles []UploadedFile             `json:"uploaded_files,omitempty"`
	ViewedImages  map[string]ViewedImageData `json:"viewed_images,omitempty"`
}

type RunConfig struct {
	ThreadID               string
	ModelName              string
	ThinkingEnabled        bool
	IsPlanMode             bool
	SubagentEnabled        bool
	MaxConcurrentSubagents int
	CheckpointID           string
	AgentName              string
}

type LeadAgent interface {
	Run(ctx context.Context, state *ThreadState, cfg RunConfig) (<-chan Event, error)
	Resume(ctx context.Context, state *ThreadState, cfg RunConfig, checkpointID string) (<-chan Event, error)
}

type leadAgent struct {
	einoAgent   adk.Agent
	tools       []lcTool.BaseTool
	middlewares []adk.AgentMiddleware
	runner      *einoruntime.Runner
	skills      *skillsruntime.Registry
}

var getAppConfig = config.GetAppConfig

func New(ctx context.Context) (*leadAgent, error) {
	appCfg, err := config.GetAppConfig()
	if err != nil {
		return nil, fmt.Errorf("agent.New: load config failed: %w", err)
	}

	skillLoader := skillsruntime.NewLoader()
	loadedSkills, err := skillLoader.Load(appCfg.Skills.Path, appCfg.Extensions)
	if err != nil {
		return nil, fmt.Errorf("agent.New: load skills failed: %w", err)
	}
	skillRegistry := skillsruntime.NewRegistry()
	for _, skill := range loadedSkills {
		if err := skillRegistry.Register(skill); err != nil {
			return nil, fmt.Errorf("agent.New: register skills failed: %w", err)
		}
	}
	if err := skillRegistry.OnLoad(ctx, appCfg); err != nil {
		return nil, fmt.Errorf("agent.New: skills on_load failed: %w", err)
	}

	req := models.CreateRequest{}
	if dm := appCfg.DefaultModel(); dm != nil {
		req.ModelName = dm.Name
	}

	chatModel, err := models.CreateChatModel(ctx, appCfg, req)
	if err != nil {
		return nil, fmt.Errorf("agent.New: create model failed: %w", err)
	}

	tools := toolruntime.AdaptDefaultRegistryToEinoTools()
	for _, mcpTool := range toolruntime.BuildMCPDynamicTools(appCfg) {
		tools = append(tools, toolruntime.AdaptToEinoTool(mcpTool))
	}

	// Phase7B: add subagent task tool with bounded executor.
	subagentTimeout := 900 * time.Second
	if appCfg.Subagents.TimeoutSeconds > 0 {
		subagentTimeout = time.Duration(appCfg.Subagents.TimeoutSeconds) * time.Second
	}
	maxConcurrentSubagents := 3
	if appCfg.Subagents.MaxConcurrent > 0 {
		maxConcurrentSubagents = appCfg.Subagents.MaxConcurrent
	}
	subagentExec := subagents.NewExecutor(subagents.ExecutorConfig{
		MaxConcurrent:  maxConcurrentSubagents,
		DefaultTimeout: subagentTimeout,
	})
	taskTool := subagents.NewTaskTool(subagents.TaskToolConfig{
		Executor: subagentExec,
	})
	tools = append(tools, toolruntime.AdaptToEinoTool(taskTool))

	allowedTools := skillRegistry.AllowedToolSet()
	if len(allowedTools) > 0 {
		tools, err = filterToolsByAllowed(ctx, tools, allowedTools)
		if err != nil {
			return nil, fmt.Errorf("agent.New: apply skills allowed-tools failed: %w", err)
		}
	}

	mws := buildMiddlewares(RunConfig{})

	a, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "lead_agent",
		Description: "GoClaw lead agent",
		Instruction: "You are GoClaw lead agent.",
		Model:       chatModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
		},
		MaxIterations: 100,
		Middlewares:   mws,
	})
	if err != nil {
		return nil, fmt.Errorf("agent.New: build chat model agent failed: %w", err)
	}

	checkpointStore, err := newCheckPointStore(appCfg)
	if err != nil {
		return nil, err
	}

	r, err := einoruntime.NewRunner(ctx, einoruntime.RunnerConfig{
		Agent:           a,
		EnableStreaming: true,
		CheckPointStore: checkpointStore,
	})
	if err != nil {
		return nil, fmt.Errorf("agent.New: create runner failed: %w", err)
	}

	return &leadAgent{einoAgent: a, tools: tools, middlewares: mws, runner: r, skills: skillRegistry}, nil
}

func (a *leadAgent) Run(ctx context.Context, state *ThreadState, cfg RunConfig) (<-chan Event, error) {
	if cfg.ThreadID == "" {
		return nil, fmt.Errorf("thread_id is required")
	}

	ch := make(chan Event, 32)
	if a == nil || a.runner == nil {
		go func() {
			defer close(ch)
			ch <- Event{Type: EventError, ThreadID: cfg.ThreadID, Payload: ErrorPayload{Code: ErrorCodeNotInitialized, Message: "lead agent is not initialized"}, Timestamp: timeUnixMilli()}
		}()
		return ch, nil
	}

	if state == nil {
		state = &ThreadState{}
	}
	if err := a.syncSkillsOnConfigReload(); err != nil {
		return nil, fmt.Errorf("sync skills config failed: %w", err)
	}

	messages := prepareRunMessages(state.Messages, cfg)
	opts := []adk.AgentRunOption{adk.WithSessionValues(map[string]any{
		"thread_id":                cfg.ThreadID,
		"plan_mode":                cfg.IsPlanMode,
		"subagent_enabled":         cfg.SubagentEnabled,
		"max_concurrent_subagents": cfg.MaxConcurrentSubagents,
	})}
	if strings.TrimSpace(cfg.CheckpointID) != "" {
		opts = append(opts, adk.WithCheckPointID(cfg.CheckpointID))
	}
	stream := a.runner.Run(ctx, messages, opts...)

	go func() {
		defer close(ch)
		drainIter(ctx, stream, cfg.ThreadID, ch)
	}()
	return ch, nil
}

func (a *leadAgent) Resume(ctx context.Context, _ *ThreadState, cfg RunConfig, checkpointID string) (<-chan Event, error) {
	if cfg.ThreadID == "" {
		return nil, fmt.Errorf("thread_id is required")
	}
	if strings.TrimSpace(checkpointID) == "" {
		return nil, fmt.Errorf("checkpoint_id is required")
	}

	ch := make(chan Event, 32)
	if a == nil || a.runner == nil {
		go func() {
			defer close(ch)
			ch <- Event{Type: EventError, ThreadID: cfg.ThreadID, Payload: ErrorPayload{Code: ErrorCodeNotInitialized, Message: "lead agent is not initialized"}, Timestamp: timeUnixMilli()}
		}()
		return ch, nil
	}

	if err := a.syncSkillsOnConfigReload(); err != nil {
		return nil, fmt.Errorf("sync skills config failed: %w", err)
	}

	stream, err := a.runner.Resume(ctx, checkpointID, adk.WithSessionValues(map[string]any{
		"thread_id":                cfg.ThreadID,
		"plan_mode":                cfg.IsPlanMode,
		"subagent_enabled":         cfg.SubagentEnabled,
		"max_concurrent_subagents": cfg.MaxConcurrentSubagents,
	}))
	if err != nil {
		return nil, fmt.Errorf("resume from checkpoint failed: %w", err)
	}

	go func() {
		defer close(ch)
		drainIter(ctx, stream, cfg.ThreadID, ch)
	}()
	return ch, nil
}

func filterToolsByAllowed(ctx context.Context, tools []lcTool.BaseTool, allowed map[string]struct{}) ([]lcTool.BaseTool, error) {
	if len(allowed) == 0 {
		return tools, nil
	}
	out := make([]lcTool.BaseTool, 0, len(tools))
	for _, t := range tools {
		info, err := t.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("read tool info failed: %w", err)
		}
		if _, ok := allowed[info.Name]; ok {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no tools matched skills allowed-tools")
	}
	return out, nil
}

func (a *leadAgent) syncSkillsOnConfigReload() error {
	if a == nil || a.skills == nil {
		return nil
	}
	cfg, err := getAppConfig()
	if err != nil {
		return err
	}
	return a.skills.OnConfigReload(cfg)
}

func prepareRunMessages(messages []*schema.Message, cfg RunConfig) []*schema.Message {
	out := make([]*schema.Message, 0, len(messages)+1)
	hints := make([]string, 0, 2)
	if cfg.IsPlanMode {
		hints = append(hints, "Plan mode is enabled. Keep task tracking explicit.")
	}
	if cfg.SubagentEnabled {
		hints = append(hints, "Subagent delegation is enabled for this run.")
	}
	if len(hints) > 0 {
		out = append(out, schema.SystemMessage(strings.Join(hints, "\n")))
	}
	if len(messages) == 0 {
		out = append(out, schema.UserMessage(""))
		return out
	}
	out = append(out, messages...)
	return out
}

func drainIter(ctx context.Context, iter *einoruntime.EventStream, threadID string, ch chan<- Event) {
	if iter == nil {
		ch <- Event{Type: EventError, ThreadID: threadID, Payload: ErrorPayload{Code: ErrorCodeEmptyStream, Message: "empty event stream"}, Timestamp: timeUnixMilli()}
		return
	}

	terminal := false
	var finalMessages []string
	emit := func(ev Event) {
		if terminal {
			return
		}
		if ev.Type == EventMessageDelta {
			if p, ok := ev.Payload.(MessageDeltaPayload); ok && !p.IsThinking {
				finalMessages = append(finalMessages, p.Content)
			}
		}
		ch <- ev
		if ev.Type == EventError || ev.Type == EventCompleted {
			terminal = true
		}
	}

	for {
		select {
		case <-ctx.Done():
			emit(Event{Type: EventError, ThreadID: threadID, Payload: ErrorPayload{Code: ErrorCodeContextCancelled, Message: ctx.Err().Error()}, Timestamp: timeUnixMilli()})
			return
		default:
		}

		ae, ok := iter.Next()
		if !ok {
			break
		}
		for _, ev := range convertAgentEvent(ae, threadID) {
			emit(ev)
			if terminal {
				return
			}
		}
	}

	finalMessage := strings.Join(finalMessages, "")
	emit(Event{Type: EventCompleted, ThreadID: threadID, Payload: CompletedPayload{FinalMessage: finalMessage}, Timestamp: timeUnixMilli()})
}

func buildMiddlewares(cfg RunConfig) []adk.AgentMiddleware {
	_ = cfg

	appCfg, _ := config.GetAppConfig()
	legacy := make([]basemw.Middleware, 0, 4)

	memoryEnabled := true
	titleEnabled := true
	summarizeEnabled := true
	memoryPath := "memory.json"
	memoryDebounce := 30 * time.Second

	if appCfg != nil {
		memoryEnabled = appCfg.Memory.Enabled
		titleEnabled = appCfg.Title.Enabled
		summarizeEnabled = appCfg.Summarization.Enabled
		if strings.TrimSpace(appCfg.Memory.StoragePath) != "" {
			memoryPath = appCfg.Memory.StoragePath
		}
		if appCfg.Memory.DebounceSeconds > 0 {
			memoryDebounce = time.Duration(appCfg.Memory.DebounceSeconds) * time.Second
		}
	}

	if memoryEnabled {
		store := memorymw.NewJSONFileStore(memoryPath)
		queue := memorymw.GetGlobalQueue(filepath.Dir(memoryPath))
		queue.DebounceDelay = memoryDebounce
		legacy = append(legacy, memorymw.NewMemoryMiddleware(store, queue, ""))
	}
	if summarizeEnabled {
		legacy = append(legacy, summarizemw.NewSummarizationMiddleware(summarizemw.DefaultConfig(), nil))
	}
	legacy = append(legacy, todomw.NewTodoMiddleware())
	if titleEnabled {
		legacy = append(legacy, titlemw.NewTitleMiddleware(titlemw.DefaultConfig(), nil))
	}

	if len(legacy) == 0 {
		return nil
	}

	return []adk.AgentMiddleware{{
		BeforeChatModel: func(ctx context.Context, st *adk.ChatModelAgentState) error {
			mwState := toMiddlewareState(ctx, st)
			for _, mw := range legacy {
				if err := mw.Before(ctx, mwState); err != nil {
					return err
				}
			}
			applyMiddlewareState(mwState, st)
			return nil
		},
		AfterChatModel: func(ctx context.Context, st *adk.ChatModelAgentState) error {
			mwState := toMiddlewareState(ctx, st)
			resp := toMiddlewareResponse(st)
			for i := len(legacy) - 1; i >= 0; i-- {
				_ = legacy[i].After(ctx, mwState, resp)
			}
			applyMiddlewareState(mwState, st)
			return nil
		},
	}}
}

func toMiddlewareState(ctx context.Context, st *adk.ChatModelAgentState) *basemw.State {
	vals := adk.GetSessionValues(ctx)
	threadID, _ := vals["thread_id"].(string)
	planMode, _ := vals["plan_mode"].(bool)

	msgs := make([]map[string]any, 0, len(st.Messages))
	for _, m := range st.Messages {
		msgs = append(msgs, toLegacyMessage(m))
	}

	return &basemw.State{
		ThreadID: threadID,
		Messages: msgs,
		PlanMode: planMode,
		Extra:    map[string]any{},
	}
}

func applyMiddlewareState(ms *basemw.State, st *adk.ChatModelAgentState) {
	if ms == nil || st == nil {
		return
	}
	converted := make([]*schema.Message, 0, len(ms.Messages))
	for _, m := range ms.Messages {
		converted = append(converted, fromLegacyMessage(m))
	}
	st.Messages = converted
}

func toLegacyMessage(m *schema.Message) map[string]any {
	out := map[string]any{
		"role":    roleToLegacy(m.Role),
		"content": m.Content,
	}
	if m.Name != "" {
		out["name"] = m.Name
	}
	if len(m.ToolCalls) > 0 {
		calls := make([]map[string]any, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			calls = append(calls, map[string]any{
				"id":        tc.ID,
				"name":      tc.Function.Name,
				"arguments": tc.Function.Arguments,
				"input":     tc.Function.Arguments,
			})
		}
		out["tool_calls"] = calls
	}
	if m.ToolCallID != "" {
		out["tool_call_id"] = m.ToolCallID
	}
	if m.ToolName != "" {
		out["tool_name"] = m.ToolName
	}
	return out
}

func fromLegacyMessage(m map[string]any) *schema.Message {
	role, _ := m["role"].(string)
	content, _ := m["content"].(string)

	switch role {
	case "system":
		return schema.SystemMessage(content)
	case "assistant":
		calls := toToolCalls(m["tool_calls"])
		msg := schema.AssistantMessage(content, calls)
		if name, ok := m["name"].(string); ok {
			msg.Name = name
		}
		return msg
	case "tool":
		toolCallID, _ := m["tool_call_id"].(string)
		toolName, _ := m["tool_name"].(string)
		opts := make([]schema.ToolMessageOption, 0, 1)
		if toolName != "" {
			opts = append(opts, schema.WithToolName(toolName))
		}
		return schema.ToolMessage(content, toolCallID, opts...)
	default:
		return schema.UserMessage(content)
	}
}

func toToolCalls(raw any) []schema.ToolCall {
	toCall := func(v map[string]any) schema.ToolCall {
		id, _ := v["id"].(string)
		name, _ := v["name"].(string)
		args, _ := v["arguments"].(string)
		if args == "" {
			args, _ = v["input"].(string)
		}
		return schema.ToolCall{ID: id, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}
	}

	out := make([]schema.ToolCall, 0)
	switch vv := raw.(type) {
	case []map[string]any:
		for _, v := range vv {
			out = append(out, toCall(v))
		}
	case []any:
		for _, item := range vv {
			if m, ok := item.(map[string]any); ok {
				out = append(out, toCall(m))
			}
		}
	}
	return out
}

func roleToLegacy(role schema.RoleType) string {
	switch role {
	case schema.User:
		return "human"
	case schema.Assistant:
		return "assistant"
	case schema.Tool:
		return "tool"
	case schema.System:
		return "system"
	default:
		return string(role)
	}
}

func toMiddlewareResponse(st *adk.ChatModelAgentState) *basemw.Response {
	resp := &basemw.Response{ToolCalls: make([]map[string]any, 0)}
	if st == nil || len(st.Messages) == 0 {
		return resp
	}

	last := st.Messages[len(st.Messages)-1]
	resp.FinalMessage = last.Content
	for _, tc := range last.ToolCalls {
		resp.ToolCalls = append(resp.ToolCalls, map[string]any{
			"id":    tc.ID,
			"name":  tc.Function.Name,
			"input": tc.Function.Arguments,
		})
	}
	return resp
}

func convertAgentEvent(event *adk.AgentEvent, threadID string) []Event {
	if event == nil {
		return nil
	}

	now := timeUnixMilli()
	out := make([]Event, 0, 4)

	if event.Err != nil {
		out = append(out, Event{Type: EventError, ThreadID: threadID, Payload: ErrorPayload{Code: ErrorCodeRunFailed, Message: event.Err.Error()}, Timestamp: now})
		return out
	}
	if event.Action != nil && event.Action.Interrupted != nil {
		out = append(out, Event{Type: EventError, ThreadID: threadID, Payload: ErrorPayload{Code: ErrorCodeInterrupted, Message: "run interrupted"}, Timestamp: now})
		return out
	}
	if event.Output == nil || event.Output.MessageOutput == nil {
		return out
	}

	msg, err := event.Output.MessageOutput.GetMessage()
	if err != nil || msg == nil {
		return out
	}

	if strings.TrimSpace(msg.ReasoningContent) != "" {
		out = append(out, Event{Type: EventMessageDelta, ThreadID: threadID, Payload: MessageDeltaPayload{Content: msg.ReasoningContent, IsThinking: true}, Timestamp: now})
	}
	if strings.TrimSpace(msg.Content) != "" {
		out = append(out, Event{Type: EventMessageDelta, ThreadID: threadID, Payload: MessageDeltaPayload{Content: msg.Content}, Timestamp: now})
	}
	for _, tc := range msg.ToolCalls {
		out = append(out, Event{Type: EventToolEvent, ThreadID: threadID, Payload: ToolEventPayload{CallID: tc.ID, ToolName: tc.Function.Name, Input: tc.Function.Arguments}, Timestamp: now})
	}
	if msg.Role == schema.Tool {
		out = append(out, Event{Type: EventToolEvent, ThreadID: threadID, Payload: ToolEventPayload{CallID: msg.ToolCallID, ToolName: msg.ToolName, Output: msg.Content, IsError: isToolError(msg)}, Timestamp: now})
		if msg.ToolName == subagents.TaskToolName {
			if taskEv := toTaskEvent(threadID, msg.Content, now); taskEv != nil {
				out = append(out, *taskEv)
			}
		}
	}
	return out
}

// isToolError detects if a tool message represents an error result.
func isToolError(msg *schema.Message) bool {
	if msg == nil || msg.Role != schema.Tool {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(msg.Content))
	return strings.HasPrefix(lower, "error") || strings.HasPrefix(lower, "failed")
}

func toTaskEvent(threadID, raw string, ts int64) *Event {
	var payload struct {
		TaskID  string `json:"task_id"`
		Subject string `json:"subject"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	if payload.TaskID == "" || payload.Status == "" {
		return nil
	}

	evType := EventTaskRunning
	switch payload.Status {
	case string(subagents.StatusPending), string(subagents.StatusQueued):
		evType = EventTaskStarted
	case string(subagents.StatusInProgress):
		evType = EventTaskRunning
	case string(subagents.StatusCompleted):
		evType = EventTaskCompleted
	case string(subagents.StatusFailed), string(subagents.StatusTimedOut):
		evType = EventTaskFailed
	}

	return &Event{
		Type:     evType,
		ThreadID: threadID,
		Payload: TaskPayload{
			TaskID:  payload.TaskID,
			Subject: payload.Subject,
			Status:  payload.Status,
		},
		Timestamp: ts,
	}
}

// timeUnixMilli returns current time in Unix milliseconds.
func timeUnixMilli() int64 {
	return time.Now().UnixMilli()
}

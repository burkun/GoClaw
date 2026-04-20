package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"goclaw/internal/agent/subagents"
	einoruntime "goclaw/internal/eino"
	basemw "goclaw/internal/middleware"
)

// Run starts a new agent run with the given state and configuration.
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

	// Build session values for subagent state passing
	sessionValues := map[string]any{
		"thread_id":                cfg.ThreadID,
		"plan_mode":                cfg.IsPlanMode,
		"subagent_enabled":         cfg.SubagentEnabled,
		"max_concurrent_subagents": cfg.MaxConcurrentSubagents,
		"uploaded_files":           state.UploadedFiles,
		"viewed_images":            state.ViewedImages,
		"agent_name":               cfg.AgentName,
		"is_subagent":              strings.TrimSpace(cfg.AgentName) != "",
	}
	// Pass thread data paths for subagent access
	if state.ThreadData != nil {
		sessionValues["workspace_path"] = state.ThreadData.WorkspacePath
		sessionValues["uploads_path"] = state.ThreadData.UploadsPath
		sessionValues["outputs_path"] = state.ThreadData.OutputsPath
	}

	opts := []adk.AgentRunOption{adk.WithSessionValues(sessionValues)}
	if strings.TrimSpace(cfg.CheckpointID) != "" {
		opts = append(opts, adk.WithCheckPointID(cfg.CheckpointID))
	}
	stream := a.runner.Run(ctx, messages, opts...)

	go func() {
		defer close(ch)
		drainIter(ctx, stream, cfg.ThreadID, cfg.RunID, ch)
	}()
	return ch, nil
}

// Resume resumes an agent run from a checkpoint.
func (a *leadAgent) Resume(ctx context.Context, state *ThreadState, cfg RunConfig, checkpointID string) (<-chan Event, error) {
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

	if state == nil {
		state = &ThreadState{}
	}
	stream, err := a.runner.Resume(ctx, checkpointID, adk.WithSessionValues(map[string]any{
		"thread_id":                cfg.ThreadID,
		"plan_mode":                cfg.IsPlanMode,
		"subagent_enabled":         cfg.SubagentEnabled,
		"max_concurrent_subagents": cfg.MaxConcurrentSubagents,
		"uploaded_files":           state.UploadedFiles,
		"viewed_images":            state.ViewedImages,
		"agent_name":               cfg.AgentName,
		"is_subagent":              strings.TrimSpace(cfg.AgentName) != "",
	}))
	if err != nil {
		return nil, fmt.Errorf("resume from checkpoint failed: %w", err)
	}

	go func() {
		defer close(ch)
		drainIter(ctx, stream, cfg.ThreadID, cfg.RunID, ch)
	}()
	return ch, nil
}

func (a *leadAgent) syncSkillsOnConfigReload() error {
	if a == nil {
		return nil
	}
	cfg, err := getAppConfig()
	if err != nil {
		return err
	}
	modelCfg := cfg.DefaultModel()
	if err := registerDefaultTools(cfg, modelCfg); err != nil {
		return fmt.Errorf("reload tools failed: %w", err)
	}
	invalidateMCPConfigCache()
	if a.skills == nil {
		return nil
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

func drainIter(ctx context.Context, iter *einoruntime.EventStream, threadID, runID string, ch chan<- Event) {
	if iter == nil {
		ch <- Event{Type: EventError, ThreadID: threadID, RunID: runID, Payload: ErrorPayload{Code: ErrorCodeEmptyStream, Message: "empty event stream"}, Timestamp: timeUnixMilli()}
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
		ev.RunID = runID
		ch <- ev
		if ev.Type == EventError || ev.Type == EventCompleted {
			terminal = true
		}
	}

	for {
		select {
		case <-ctx.Done():
			emit(Event{Type: EventError, ThreadID: threadID, RunID: runID, Payload: ErrorPayload{Code: ErrorCodeContextCancelled, Message: ctx.Err().Error()}, Timestamp: timeUnixMilli()})
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
	emit(Event{Type: EventCompleted, ThreadID: threadID, RunID: runID, Payload: CompletedPayload{FinalMessage: finalMessage}, Timestamp: timeUnixMilli()})
}

// toMiddlewareState converts adk state to middleware state.
func toMiddlewareState(ctx context.Context, st *adk.ChatModelAgentState) *basemw.State {
	vals := adk.GetSessionValues(ctx)
	threadID, _ := vals["thread_id"].(string)
	planMode, _ := vals["plan_mode"].(bool)
	isSubagent, _ := vals["is_subagent"].(bool)

	msgs := make([]map[string]any, 0, len(st.Messages))
	for _, m := range st.Messages {
		msgs = append(msgs, toLegacyMessage(m))
	}

	extra := map[string]any{
		"is_subagent": isSubagent,
	}
	if uploaded, ok := vals["uploaded_files"]; ok {
		extra["uploaded_files"] = uploaded
	}
	if agentName, ok := vals["agent_name"].(string); ok && strings.TrimSpace(agentName) != "" {
		extra["agent_name"] = strings.TrimSpace(agentName)
	}

	// Pre-seed pending_tool_calls from the latest assistant message if present.
	for i := len(msgs) - 1; i >= 0; i-- {
		if role, _ := msgs[i]["role"].(string); role == "assistant" {
			if tcs := parseLegacyToolCalls(msgs[i]["tool_calls"]); len(tcs) > 0 {
				extra["pending_tool_calls"] = tcs
			}
			break
		}
	}

	mwState := &basemw.State{
		ThreadID:     threadID,
		Messages:     msgs,
		PlanMode:     planMode,
		ViewedImages: map[string]basemw.ViewedImage{},
		Extra:        extra,
	}
	if rawImages, ok := vals["viewed_images"]; ok {
		mwState.ViewedImages = parseViewedImages(rawImages)
	}
	return mwState
}

func syncMiddlewareStateToSession(ctx context.Context, state *basemw.State) {
	if state == nil {
		return
	}
	vals := adk.GetSessionValues(ctx)
	if vals == nil {
		return
	}
	vals[middlewareStateSessionKey] = state
	if state.Extra == nil {
		return
	}
	for _, k := range []string{"task_tool_calls_count", "clarification_request", "interrupt", "pending_tool_calls"} {
		if v, ok := state.Extra[k]; ok {
			vals[k] = v
		}
	}
}

func toToolMiddlewareState(ctx context.Context) *basemw.State {
	vals := adk.GetSessionValues(ctx)
	if vals != nil {
		if cached, ok := vals[middlewareStateSessionKey].(*basemw.State); ok && cached != nil {
			if cached.Extra == nil {
				cached.Extra = map[string]any{}
			}
			return cached
		}
	}

	threadID := ""
	planMode := false
	extra := map[string]any{}
	viewedImages := map[string]basemw.ViewedImage{}
	if vals != nil {
		threadID, _ = vals["thread_id"].(string)
		planMode, _ = vals["plan_mode"].(bool)
		isSubagent, _ := vals["is_subagent"].(bool)
		extra["is_subagent"] = isSubagent
		if uploaded, ok := vals["uploaded_files"]; ok {
			extra["uploaded_files"] = uploaded
		}
		if agentName, ok := vals["agent_name"].(string); ok && strings.TrimSpace(agentName) != "" {
			extra["agent_name"] = strings.TrimSpace(agentName)
		}
		for _, k := range []string{"task_tool_calls_count", "clarification_request", "interrupt", "pending_tool_calls"} {
			if v, ok := vals[k]; ok {
				extra[k] = v
			}
		}
		viewedImages = parseViewedImages(vals["viewed_images"])
	}

	state := &basemw.State{
		ThreadID:     threadID,
		PlanMode:     planMode,
		ViewedImages: viewedImages,
		Extra:        extra,
	}
	if vals != nil {
		vals[middlewareStateSessionKey] = state
	}
	return state
}

func toToolMiddlewareToolCall(input *compose.ToolInput) *basemw.ToolCall {
	if input == nil {
		return &basemw.ToolCall{Input: map[string]any{}}
	}
	return &basemw.ToolCall{
		ID:    input.CallID,
		Name:  input.Name,
		Input: parseToolInputArguments(input.Arguments),
	}
}

func toComposeToolInput(original *compose.ToolInput, toolCall *basemw.ToolCall) *compose.ToolInput {
	if original == nil {
		return &compose.ToolInput{}
	}
	if toolCall == nil {
		return original
	}
	arguments := original.Arguments
	if len(toolCall.Input) > 0 {
		if bs, err := json.Marshal(toolCall.Input); err == nil {
			arguments = string(bs)
		}
	}
	return &compose.ToolInput{
		Name:        toolCall.Name,
		Arguments:   arguments,
		CallID:      toolCall.ID,
		CallOptions: original.CallOptions,
	}
}

func parseToolInputArguments(arguments string) map[string]any {
	args := strings.TrimSpace(arguments)
	if args == "" {
		return map[string]any{}
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(args), &obj); err == nil {
		if obj == nil {
			return map[string]any{}
		}
		return obj
	}
	var raw any
	if err := json.Unmarshal([]byte(args), &raw); err == nil {
		if obj, ok := raw.(map[string]any); ok {
			return obj
		}
		return map[string]any{"input": raw}
	}
	return map[string]any{"input": args}
}

func middlewareToolOutputToString(output any) string {
	switch v := output.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	default:
		if bs, err := json.Marshal(v); err == nil {
			return string(bs)
		}
		return fmt.Sprint(v)
	}
}

func runMiddlewareToolChain(ctx context.Context, middlewares []basemw.Middleware, state *basemw.State, toolCall *basemw.ToolCall, base basemw.ToolHandler) (*basemw.ToolResult, error) {
	handler := base
	for i := len(middlewares) - 1; i >= 0; i-- {
		mw := middlewares[i]
		nextHandler := handler
		handler = func(callCtx context.Context, call *basemw.ToolCall) (*basemw.ToolResult, error) {
			return mw.WrapToolCall(callCtx, state, call, nextHandler)
		}
	}
	return handler(ctx, toolCall)
}

func parseLegacyToolCalls(raw any) []map[string]any {
	out := make([]map[string]any, 0)
	switch vv := raw.(type) {
	case []map[string]any:
		out = append(out, vv...)
	case []any:
		for _, item := range vv {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

func parseViewedImages(raw any) map[string]basemw.ViewedImage {
	out := make(map[string]basemw.ViewedImage)
	switch vv := raw.(type) {
	case map[string]ViewedImageData:
		for path, img := range vv {
			out[path] = basemw.ViewedImage{Base64: img.Base64, MIMEType: img.MIMEType}
		}
	case map[string]any:
		for path, v := range vv {
			switch img := v.(type) {
			case map[string]any:
				base64Data, _ := img["base64"].(string)
				mimeType, _ := img["mime_type"].(string)
				out[path] = basemw.ViewedImage{Base64: base64Data, MIMEType: mimeType}
			}
		}
	}
	return out
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

	// Handle CustomizedAction for task events
	if event.Action != nil && event.Action.CustomizedAction != nil {
		if data, ok := event.Action.CustomizedAction.(map[string]any); ok {
			eventType, _ := data["type"].(string)
			taskID, _ := data["task_id"].(string)
			if eventType != "" && taskID != "" {
				out = append(out, Event{
					Type:      EventType(eventType),
					ThreadID:  threadID,
					Payload:   data,
					Timestamp: now,
				})
			}
		}
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

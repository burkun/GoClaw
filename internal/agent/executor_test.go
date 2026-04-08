package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	lctool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/bookerbai/goclaw/internal/agent/subagents"
	"github.com/bookerbai/goclaw/internal/config"
	basemw "github.com/bookerbai/goclaw/internal/middleware"
)

// --- Tests for prepareRunMessages ---

func TestPrepareRunMessages_EmptyMessages(t *testing.T) {
	cfg := RunConfig{}
	result := prepareRunMessages(nil, cfg)

	// When no hints and no messages, should return 1 empty user message
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	if result[0].Role != schema.User {
		t.Errorf("expected user message, got %v", result[0].Role)
	}
}

func TestPrepareRunMessages_WithPlanMode(t *testing.T) {
	cfg := RunConfig{IsPlanMode: true}
	messages := []*schema.Message{schema.UserMessage("Hello")}

	result := prepareRunMessages(messages, cfg)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}

	// First should be system message with plan mode hint
	if result[0].Role != schema.System {
		t.Errorf("expected system message first, got %v", result[0].Role)
	}
	if !strings.Contains(result[0].Content, "Plan mode") {
		t.Error("expected plan mode hint in system message")
	}
}

func TestPrepareRunMessages_WithSubagentEnabled(t *testing.T) {
	cfg := RunConfig{SubagentEnabled: true}
	messages := []*schema.Message{schema.UserMessage("Hello")}

	result := prepareRunMessages(messages, cfg)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}

	// First should be system message with subagent hint
	if result[0].Role != schema.System {
		t.Errorf("expected system message first, got %v", result[0].Role)
	}
	if !strings.Contains(result[0].Content, "Subagent") {
		t.Error("expected subagent hint in system message")
	}
}

func TestPrepareRunMessages_BothHints(t *testing.T) {
	cfg := RunConfig{IsPlanMode: true, SubagentEnabled: true}
	messages := []*schema.Message{schema.UserMessage("Hello")}

	result := prepareRunMessages(messages, cfg)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}

	if result[0].Role != schema.System {
		t.Errorf("expected system message first, got %v", result[0].Role)
	}
	if !strings.Contains(result[0].Content, "Plan mode") || !strings.Contains(result[0].Content, "Subagent") {
		t.Error("expected both hints in system message")
	}
}

func TestPrepareRunMessages_NoHints(t *testing.T) {
	cfg := RunConfig{}
	messages := []*schema.Message{schema.UserMessage("Hello")}

	result := prepareRunMessages(messages, cfg)

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	if result[0].Content != "Hello" {
		t.Errorf("expected original message, got: %s", result[0].Content)
	}
}

func TestPrepareRunMessages_MultipleMessages(t *testing.T) {
	cfg := RunConfig{IsPlanMode: true}
	messages := []*schema.Message{
		schema.UserMessage("Hello"),
		schema.AssistantMessage("Hi there", nil),
		schema.UserMessage("How are you?"),
	}

	result := prepareRunMessages(messages, cfg)

	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}

	// First is system, rest are original messages
	if result[0].Role != schema.System {
		t.Error("expected system message first")
	}
	if result[1].Content != "Hello" {
		t.Error("expected original messages preserved")
	}
}

// --- Tests for toMiddlewareState ---

func TestToMiddlewareState_EmptyState(t *testing.T) {
	ctx := context.Background()
	st := &adk.ChatModelAgentState{Messages: []*schema.Message{}}

	state := toMiddlewareState(ctx, st)

	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if state.ThreadID != "" {
		t.Error("expected empty thread ID")
	}
	if state.PlanMode {
		t.Error("expected plan mode false")
	}
}

func TestToMiddlewareState_WithSessionValues(t *testing.T) {
	ctx := context.Background()
	st := &adk.ChatModelAgentState{
		Messages: []*schema.Message{
			schema.UserMessage("test"),
		},
	}

	// Note: Session values require adk context setup
	state := toMiddlewareState(ctx, st)
	if state == nil {
		t.Fatal("expected non-nil state")
	}
}

func TestToMiddlewareState_WithToolCalls(t *testing.T) {
	st := &adk.ChatModelAgentState{
		Messages: []*schema.Message{
			schema.UserMessage("test"),
			schema.AssistantMessage("Let me help", []schema.ToolCall{
				{ID: "call_1", Type: "function", Function: schema.FunctionCall{Name: "read", Arguments: `{}`}},
			}),
		},
	}

	ctx := context.Background()
	state := toMiddlewareState(ctx, st)

	if state == nil {
		t.Fatal("expected non-nil state")
	}

	// Should have pending_tool_calls in Extra
	if state.Extra == nil {
		t.Fatal("expected Extra to be set")
	}
}

// --- Tests for toToolMiddlewareState ---

func TestToToolMiddlewareState_NoCache(t *testing.T) {
	ctx := context.Background()
	state := toToolMiddlewareState(ctx)

	if state == nil {
		t.Fatal("expected non-nil state")
	}
}

// --- Tests for toToolMiddlewareToolCall ---

func TestToToolMiddlewareToolCall_NilInput(t *testing.T) {
	result := toToolMiddlewareToolCall(nil)

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Input) != 0 {
		t.Error("expected empty input for nil")
	}
}

func TestToToolMiddlewareToolCall_WithInput(t *testing.T) {
	input := &compose.ToolInput{
		CallID:    "call_1",
		Name:      "test_tool",
		Arguments: `{"key": "value"}`,
	}

	result := toToolMiddlewareToolCall(input)

	if result.ID != "call_1" {
		t.Errorf("expected call_1, got %s", result.ID)
	}
	if result.Name != "test_tool" {
		t.Errorf("expected test_tool, got %s", result.Name)
	}
	if result.Input["key"] != "value" {
		t.Error("expected input to be parsed")
	}
}

func TestToToolMiddlewareToolCall_EmptyArguments(t *testing.T) {
	input := &compose.ToolInput{
		CallID:    "call_1",
		Name:      "test_tool",
		Arguments: "",
	}

	result := toToolMiddlewareToolCall(input)

	if len(result.Input) != 0 {
		t.Errorf("expected empty input, got %v", result.Input)
	}
}

// --- Tests for parseToolInputArguments ---

func TestParseToolInputArguments_Empty(t *testing.T) {
	result := parseToolInputArguments("")
	if len(result) != 0 {
		t.Error("expected empty map for empty input")
	}
}

func TestParseToolInputArguments_Whitespace(t *testing.T) {
	result := parseToolInputArguments("   ")
	if len(result) != 0 {
		t.Error("expected empty map for whitespace input")
	}
}

func TestParseToolInputArguments_ValidJSON(t *testing.T) {
	result := parseToolInputArguments(`{"key": "value", "num": 42}`)

	if result["key"] != "value" {
		t.Errorf("expected 'value', got %v", result["key"])
	}
	if result["num"] != float64(42) {
		t.Errorf("expected 42, got %v", result["num"])
	}
}

func TestParseToolInputArguments_InvalidJSON(t *testing.T) {
	result := parseToolInputArguments("not valid json")

	// Should wrap raw string as input
	if result["input"] != "not valid json" {
		t.Errorf("expected raw string as input, got %v", result["input"])
	}
}

func TestParseToolInputArguments_NonObjectJSON(t *testing.T) {
	result := parseToolInputArguments(`"just a string"`)

	// Should wrap as input
	if result["input"] != "just a string" {
		t.Errorf("expected wrapped string, got %v", result["input"])
	}
}

func TestParseToolInputArguments_ArrayJSON(t *testing.T) {
	result := parseToolInputArguments(`[1, 2, 3]`)

	// Should wrap array as input
	if result["input"] == nil {
		t.Error("expected wrapped array")
	}
}

func TestParseToolInputArguments_NestedObject(t *testing.T) {
	result := parseToolInputArguments(`{"outer": {"inner": "value"}}`)

	outer, ok := result["outer"].(map[string]any)
	if !ok {
		t.Fatal("expected nested object")
	}
	if outer["inner"] != "value" {
		t.Error("expected nested value")
	}
}

// --- Tests for middlewareToolOutputToString ---

func TestMiddlewareToolOutputToString(t *testing.T) {
	tests := []struct {
		input    any
		expected string
	}{
		{nil, ""},
		{"", ""},
		{"hello", "hello"},
		{[]byte("bytes"), "bytes"},
		{123, "123"},
		{map[string]string{"key": "value"}, `{"key":"value"}`},
		{[]string{"a", "b"}, `["a","b"]`},
		{struct{ Name string }{Name: "test"}, `{"Name":"test"}`},
	}

	for _, tt := range tests {
		result := middlewareToolOutputToString(tt.input)
		if result != tt.expected {
			t.Errorf("middlewareToolOutputToString(%v) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestMiddlewareToolOutputToString_InvalidJSON(t *testing.T) {
	// Channel cannot be marshaled to JSON
	ch := make(chan int)
	result := middlewareToolOutputToString(ch)
	if result == "" {
		t.Error("expected string representation even for invalid JSON")
	}
}

// --- Tests for toComposeToolInput ---

func TestToComposeToolInput_NilOriginal(t *testing.T) {
	result := toComposeToolInput(nil, nil)

	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestToComposeToolInput_NilToolCall(t *testing.T) {
	original := &compose.ToolInput{
		CallID:    "original_id",
		Name:      "original_name",
		Arguments: `{"original": "args"}`,
	}

	result := toComposeToolInput(original, nil)

	if result.CallID != "original_id" {
		t.Error("expected original values preserved")
	}
}

func TestToComposeToolInput_WithToolCall(t *testing.T) {
	original := &compose.ToolInput{
		CallID:      "original_id",
		Name:        "original_name",
		Arguments:   `{"original": "args"}`,
		CallOptions: []lctool.Option{},
	}

	toolCall := &basemw.ToolCall{
		ID:    "new_id",
		Name:  "new_name",
		Input: map[string]any{"new": "input"},
	}

	result := toComposeToolInput(original, toolCall)

	if result.Name != "new_name" {
		t.Errorf("expected name 'new_name', got %s", result.Name)
	}
	if result.CallID != "new_id" {
		t.Errorf("expected call_id 'new_id', got %s", result.CallID)
	}
}

// --- Tests for runMiddlewareToolChain ---

func TestRunMiddlewareToolChain_NoMiddleware(t *testing.T) {
	baseCalled := false
	base := func(_ context.Context, _ *basemw.ToolCall) (*basemw.ToolResult, error) {
		baseCalled = true
		return &basemw.ToolResult{Output: "ok"}, nil
	}

	result, err := runMiddlewareToolChain(context.Background(), nil, &basemw.State{}, &basemw.ToolCall{}, base)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !baseCalled {
		t.Error("expected base handler to be called")
	}
	if result.Output != "ok" {
		t.Error("expected result from base handler")
	}
}

func TestRunMiddlewareToolChain_WithMiddleware(t *testing.T) {
	middlewareCalled := false
	baseCalled := false

	middleware := &mockMiddleware{
		wrapToolCall: func(ctx context.Context, state *basemw.State, call *basemw.ToolCall, next basemw.ToolHandler) (*basemw.ToolResult, error) {
			middlewareCalled = true
			return next(ctx, call)
		},
	}

	base := func(_ context.Context, _ *basemw.ToolCall) (*basemw.ToolResult, error) {
		baseCalled = true
		return &basemw.ToolResult{Output: "ok"}, nil
	}

	_, _ = runMiddlewareToolChain(context.Background(), []basemw.Middleware{middleware}, &basemw.State{}, &basemw.ToolCall{}, base)

	if !middlewareCalled {
		t.Error("expected middleware to be called")
	}
	if !baseCalled {
		t.Error("expected base handler to be called")
	}
}

func TestRunMiddlewareToolChain_MultipleMiddleware(t *testing.T) {
	order := []string{}

	mw1 := &mockMiddleware{
		wrapToolCall: func(ctx context.Context, state *basemw.State, call *basemw.ToolCall, next basemw.ToolHandler) (*basemw.ToolResult, error) {
			order = append(order, "mw1-before")
			result, err := next(ctx, call)
			order = append(order, "mw1-after")
			return result, err
		},
	}

	mw2 := &mockMiddleware{
		wrapToolCall: func(ctx context.Context, state *basemw.State, call *basemw.ToolCall, next basemw.ToolHandler) (*basemw.ToolResult, error) {
			order = append(order, "mw2-before")
			result, err := next(ctx, call)
			order = append(order, "mw2-after")
			return result, err
		},
	}

	base := func(_ context.Context, _ *basemw.ToolCall) (*basemw.ToolResult, error) {
		order = append(order, "base")
		return &basemw.ToolResult{Output: "ok"}, nil
	}

	_, _ = runMiddlewareToolChain(context.Background(), []basemw.Middleware{mw1, mw2}, &basemw.State{}, &basemw.ToolCall{}, base)

	// Middlewares should wrap in reverse order
	expected := []string{"mw1-before", "mw2-before", "base", "mw2-after", "mw1-after"}
	if len(order) != len(expected) {
		t.Fatalf("expected order %v, got %v", expected, order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("at %d: expected %s, got %s", i, v, order[i])
		}
	}
}

// mockMiddleware is a test middleware that allows customizing WrapToolCall
type mockMiddleware struct {
	name         string
	beforeAgent  func(ctx context.Context, state *basemw.State) error
	beforeModel  func(ctx context.Context, state *basemw.State) error
	afterModel   func(ctx context.Context, state *basemw.State, response *basemw.Response) error
	afterAgent   func(ctx context.Context, state *basemw.State, response *basemw.Response) error
	wrapToolCall func(ctx context.Context, state *basemw.State, toolCall *basemw.ToolCall, handler basemw.ToolHandler) (*basemw.ToolResult, error)
}

func (m *mockMiddleware) Name() string {
	if m.name != "" {
		return m.name
	}
	return "mock-middleware"
}

func (m *mockMiddleware) BeforeAgent(ctx context.Context, state *basemw.State) error {
	if m.beforeAgent != nil {
		return m.beforeAgent(ctx, state)
	}
	return nil
}

func (m *mockMiddleware) BeforeModel(ctx context.Context, state *basemw.State) error {
	if m.beforeModel != nil {
		return m.beforeModel(ctx, state)
	}
	return nil
}

func (m *mockMiddleware) AfterModel(ctx context.Context, state *basemw.State, response *basemw.Response) error {
	if m.afterModel != nil {
		return m.afterModel(ctx, state, response)
	}
	return nil
}

func (m *mockMiddleware) AfterAgent(ctx context.Context, state *basemw.State, response *basemw.Response) error {
	if m.afterAgent != nil {
		return m.afterAgent(ctx, state, response)
	}
	return nil
}

func (m *mockMiddleware) WrapToolCall(ctx context.Context, state *basemw.State, toolCall *basemw.ToolCall, handler basemw.ToolHandler) (*basemw.ToolResult, error) {
	if m.wrapToolCall != nil {
		return m.wrapToolCall(ctx, state, toolCall, handler)
	}
	return handler(ctx, toolCall)
}

// --- Tests for parseLegacyToolCalls ---

func TestParseLegacyToolCalls_Empty(t *testing.T) {
	result := parseLegacyToolCalls(nil)
	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
}

func TestParseLegacyToolCalls_SliceMap(t *testing.T) {
	input := []map[string]any{
		{"id": "1", "name": "tool1"},
		{"id": "2", "name": "tool2"},
	}

	result := parseLegacyToolCalls(input)

	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}
}

func TestParseLegacyToolCalls_SliceAny(t *testing.T) {
	input := []any{
		map[string]any{"id": "1", "name": "tool1"},
		map[string]any{"id": "2", "name": "tool2"},
	}

	result := parseLegacyToolCalls(input)

	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}
}

func TestParseLegacyToolCalls_InvalidItems(t *testing.T) {
	input := []any{
		map[string]any{"id": "1"},
		"invalid",
		42,
	}

	result := parseLegacyToolCalls(input)

	if len(result) != 1 {
		t.Fatalf("expected 1 valid item, got %d", len(result))
	}
}

// --- Tests for parseViewedImages ---

func TestParseViewedImages_Empty(t *testing.T) {
	result := parseViewedImages(nil)
	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
}

func TestParseViewedImages_MapViewedImageData(t *testing.T) {
	input := map[string]ViewedImageData{
		"/path/to/image.png": {Base64: "abc123", MIMEType: "image/png"},
	}

	result := parseViewedImages(input)

	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if result["/path/to/image.png"].Base64 != "abc123" {
		t.Error("expected base64 data")
	}
}

func TestParseViewedImages_MapAny(t *testing.T) {
	input := map[string]any{
		"/path/to/image.png": map[string]any{
			"base64":   "abc123",
			"mime_type": "image/png",
		},
	}

	result := parseViewedImages(input)

	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
}

func TestParseViewedImages_InvalidItems(t *testing.T) {
	input := map[string]any{
		"/valid.png": map[string]any{
			"base64":   "abc",
			"mime_type": "image/png",
		},
		"/invalid": "not a map",
		"/also_invalid": 42,
	}

	result := parseViewedImages(input)

	if len(result) != 1 {
		t.Fatalf("expected 1 valid item, got %d", len(result))
	}
}

// --- Tests for applyMiddlewareState ---

func TestApplyMiddlewareState_NilInputs(t *testing.T) {
	// Should not panic
	applyMiddlewareState(nil, nil)
	applyMiddlewareState(&basemw.State{}, nil)
	applyMiddlewareState(nil, &adk.ChatModelAgentState{})
}

func TestApplyMiddlewareState_WithMessages(t *testing.T) {
	ms := &basemw.State{
		Messages: []map[string]any{
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi"},
		},
	}
	st := &adk.ChatModelAgentState{Messages: []*schema.Message{}}

	applyMiddlewareState(ms, st)

	if len(st.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(st.Messages))
	}
}

// --- Tests for toLegacyMessage ---

func TestToLegacyMessage_User(t *testing.T) {
	msg := schema.UserMessage("Hello")
	result := toLegacyMessage(msg)

	if result["role"] != "human" {
		t.Errorf("expected 'human', got %v", result["role"])
	}
	if result["content"] != "Hello" {
		t.Errorf("expected 'Hello', got %v", result["content"])
	}
}

func TestToLegacyMessage_Assistant(t *testing.T) {
	msg := schema.AssistantMessage("I can help", nil)
	result := toLegacyMessage(msg)

	if result["role"] != "assistant" {
		t.Errorf("expected 'assistant', got %v", result["role"])
	}
}

func TestToLegacyMessage_AssistantWithToolCalls(t *testing.T) {
	toolCalls := []schema.ToolCall{
		{ID: "call_1", Type: "function", Function: schema.FunctionCall{Name: "read", Arguments: `{}`}},
	}
	msg := schema.AssistantMessage("Let me help", toolCalls)
	result := toLegacyMessage(msg)

	toolCallsResult, ok := result["tool_calls"].([]map[string]any)
	if !ok {
		t.Fatal("expected tool_calls to be set")
	}
	if len(toolCallsResult) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(toolCallsResult))
	}
}

func TestToLegacyMessage_AssistantWithName(t *testing.T) {
	msg := schema.AssistantMessage("Hello", nil)
	msg.Name = "test-agent"
	result := toLegacyMessage(msg)

	if result["name"] != "test-agent" {
		t.Errorf("expected name 'test-agent', got %v", result["name"])
	}
}

func TestToLegacyMessage_Tool(t *testing.T) {
	msg := schema.ToolMessage("result", "call_1")
	msg.ToolName = "read_file"
	result := toLegacyMessage(msg)

	if result["role"] != "tool" {
		t.Errorf("expected 'tool', got %v", result["role"])
	}
	if result["tool_call_id"] != "call_1" {
		t.Errorf("expected tool_call_id 'call_1', got %v", result["tool_call_id"])
	}
	if result["tool_name"] != "read_file" {
		t.Errorf("expected tool_name 'read_file', got %v", result["tool_name"])
	}
}

func TestToLegacyMessage_System(t *testing.T) {
	msg := schema.SystemMessage("You are helpful")
	result := toLegacyMessage(msg)

	if result["role"] != "system" {
		t.Errorf("expected 'system', got %v", result["role"])
	}
}

// --- Tests for fromLegacyMessage ---

func TestFromLegacyMessage_User(t *testing.T) {
	input := map[string]any{"role": "human", "content": "Hello"}
	result := fromLegacyMessage(input)

	if result.Role != schema.User {
		t.Errorf("expected User role, got %v", result.Role)
	}
	if result.Content != "Hello" {
		t.Errorf("expected 'Hello', got %s", result.Content)
	}
}

func TestFromLegacyMessage_Assistant(t *testing.T) {
	input := map[string]any{
		"role":    "assistant",
		"content": "I can help",
	}
	result := fromLegacyMessage(input)

	if result.Role != schema.Assistant {
		t.Errorf("expected Assistant role, got %v", result.Role)
	}
}

func TestFromLegacyMessage_AssistantWithToolCalls(t *testing.T) {
	input := map[string]any{
		"role":    "assistant",
		"content": "Let me help",
		"tool_calls": []map[string]any{
			{"id": "call_1", "name": "read", "arguments": `{}`},
		},
	}
	result := fromLegacyMessage(input)

	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
}

func TestFromLegacyMessage_AssistantWithName(t *testing.T) {
	input := map[string]any{
		"role":    "assistant",
		"content": "Hello",
		"name":    "test-agent",
	}
	result := fromLegacyMessage(input)

	if result.Name != "test-agent" {
		t.Errorf("expected name 'test-agent', got %s", result.Name)
	}
}

func TestFromLegacyMessage_Tool(t *testing.T) {
	input := map[string]any{
		"role":          "tool",
		"content":       "result",
		"tool_call_id":  "call_1",
		"tool_name":     "read_file",
	}
	result := fromLegacyMessage(input)

	if result.Role != schema.Tool {
		t.Errorf("expected Tool role, got %v", result.Role)
	}
	if result.ToolCallID != "call_1" {
		t.Errorf("expected ToolCallID 'call_1', got %s", result.ToolCallID)
	}
	if result.ToolName != "read_file" {
		t.Errorf("expected ToolName 'read_file', got %s", result.ToolName)
	}
}

func TestFromLegacyMessage_System(t *testing.T) {
	input := map[string]any{"role": "system", "content": "You are helpful"}
	result := fromLegacyMessage(input)

	if result.Role != schema.System {
		t.Errorf("expected System role, got %v", result.Role)
	}
}

func TestFromLegacyMessage_DefaultToUser(t *testing.T) {
	input := map[string]any{"role": "unknown", "content": "Hello"}
	result := fromLegacyMessage(input)

	if result.Role != schema.User {
		t.Errorf("expected User role for unknown, got %v", result.Role)
	}
}

// --- Tests for toToolCalls ---

func TestToToolCalls_Empty(t *testing.T) {
	result := toToolCalls(nil)
	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
}

func TestToToolCalls_SliceMap(t *testing.T) {
	input := []map[string]any{
		{"id": "1", "name": "tool1", "arguments": `{"a": 1}`},
	}

	result := toToolCalls(input)

	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if result[0].ID != "1" {
		t.Errorf("expected ID '1', got %s", result[0].ID)
	}
	if result[0].Function.Name != "tool1" {
		t.Errorf("expected name 'tool1', got %s", result[0].Function.Name)
	}
}

func TestToToolCalls_SliceAny(t *testing.T) {
	input := []any{
		map[string]any{"id": "1", "name": "tool1", "arguments": `{"a": 1}`},
	}

	result := toToolCalls(input)

	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
}

func TestToToolCalls_FallbackInput(t *testing.T) {
	// Test using "input" field as fallback
	input := []map[string]any{
		{"id": "1", "name": "tool1", "input": `{"a": 1}`},
	}

	result := toToolCalls(input)

	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if result[0].Function.Arguments != `{"a": 1}` {
		t.Errorf("expected arguments from 'input' field, got %s", result[0].Function.Arguments)
	}
}

// --- Tests for roleToLegacy ---

func TestRoleToLegacy(t *testing.T) {
	tests := []struct {
		input    schema.RoleType
		expected string
	}{
		{schema.User, "human"},
		{schema.Assistant, "assistant"},
		{schema.Tool, "tool"},
		{schema.System, "system"},
		{schema.RoleType("unknown"), "unknown"},
	}

	for _, tt := range tests {
		result := roleToLegacy(tt.input)
		if result != tt.expected {
			t.Errorf("roleToLegacy(%v) = %s, want %s", tt.input, result, tt.expected)
		}
	}
}

// --- Tests for toMiddlewareResponse ---

func TestToMiddlewareResponse_Empty(t *testing.T) {
	result := toMiddlewareResponse(nil)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.ToolCalls) != 0 {
		t.Error("expected no tool calls")
	}
}

func TestToMiddlewareResponse_NoMessages(t *testing.T) {
	st := &adk.ChatModelAgentState{Messages: []*schema.Message{}}
	result := toMiddlewareResponse(st)

	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestToMiddlewareResponse_WithLastMessage(t *testing.T) {
	st := &adk.ChatModelAgentState{
		Messages: []*schema.Message{
			schema.UserMessage("Hello"),
			schema.AssistantMessage("I can help", []schema.ToolCall{
				{ID: "call_1", Type: "function", Function: schema.FunctionCall{Name: "read", Arguments: `{}`}},
			}),
		},
	}

	result := toMiddlewareResponse(st)

	if result.FinalMessage != "I can help" {
		t.Errorf("expected 'I can help', got %s", result.FinalMessage)
	}
	if len(result.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
}

// --- Tests for convertAgentEvent ---

func TestConvertAgentEvent_NilEvent(t *testing.T) {
	result := convertAgentEvent(nil, "thread-1")
	if result != nil {
		t.Error("expected nil for nil event")
	}
}

func TestConvertAgentEvent_WithError(t *testing.T) {
	event := &adk.AgentEvent{
		Err: errors.New("something went wrong"),
	}

	result := convertAgentEvent(event, "thread-1")

	if len(result) != 1 {
		t.Fatalf("expected 1 event, got %d", len(result))
	}
	if result[0].Type != EventError {
		t.Errorf("expected error event, got %v", result[0].Type)
	}
	payload, ok := result[0].Payload.(ErrorPayload)
	if !ok {
		t.Fatal("expected ErrorPayload")
	}
	if payload.Code != ErrorCodeRunFailed {
		t.Errorf("expected error code %s, got %s", ErrorCodeRunFailed, payload.Code)
	}
}

func TestConvertAgentEvent_Interrupted(t *testing.T) {
	event := &adk.AgentEvent{
		Action: &adk.AgentAction{
			Interrupted: &adk.InterruptInfo{Data: "test"},
		},
	}

	result := convertAgentEvent(event, "thread-1")

	if len(result) != 1 {
		t.Fatalf("expected 1 event, got %d", len(result))
	}
	if result[0].Type != EventError {
		t.Errorf("expected error event, got %v", result[0].Type)
	}
	payload, ok := result[0].Payload.(ErrorPayload)
	if !ok {
		t.Fatal("expected ErrorPayload")
	}
	if payload.Code != ErrorCodeInterrupted {
		t.Errorf("expected error code %s, got %s", ErrorCodeInterrupted, payload.Code)
	}
}

func TestConvertAgentEvent_NoOutput(t *testing.T) {
	event := &adk.AgentEvent{
		Output: nil,
	}

	result := convertAgentEvent(event, "thread-1")

	// Should return empty or minimal events
	_ = result
}

func TestConvertAgentEvent_WithReasoning(t *testing.T) {
	msgOutput := createMessageVariant(&schema.Message{
		Role:             schema.Assistant,
		ReasoningContent: "Thinking...",
		Content:          "Result",
	})

	event := &adk.AgentEvent{
		Output: &adk.AgentOutput{
			MessageOutput: msgOutput,
		},
	}

	result := convertAgentEvent(event, "thread-1")

	// Should have reasoning and content events
	hasReasoning := false
	hasContent := false
	for _, ev := range result {
		if ev.Type == EventMessageDelta {
			if payload, ok := ev.Payload.(MessageDeltaPayload); ok {
				if payload.IsThinking {
					hasReasoning = true
				} else {
					hasContent = true
				}
			}
		}
	}

	if !hasReasoning {
		t.Error("expected reasoning event")
	}
	if !hasContent {
		t.Error("expected content event")
	}
}

func TestConvertAgentEvent_WithToolCalls(t *testing.T) {
	msgOutput := createMessageVariant(&schema.Message{
		Role:    schema.Assistant,
		Content: "Let me help",
		ToolCalls: []schema.ToolCall{
			{ID: "call_1", Type: "function", Function: schema.FunctionCall{Name: "read", Arguments: `{}`}},
		},
	})

	event := &adk.AgentEvent{
		Output: &adk.AgentOutput{
			MessageOutput: msgOutput,
		},
	}

	result := convertAgentEvent(event, "thread-1")

	hasToolEvent := false
	for _, ev := range result {
		if ev.Type == EventToolEvent {
			hasToolEvent = true
			break
		}
	}

	if !hasToolEvent {
		t.Error("expected tool event")
	}
}

// --- Tests for isToolError ---

func TestIsToolError(t *testing.T) {
	tests := []struct {
		name     string
		msg      *schema.Message
		expected bool
	}{
		{"nil message", nil, false},
		{"non-tool message", schema.UserMessage("hello"), false},
		{"tool message with success", schema.ToolMessage("success", "call_1"), false},
		{"tool message with error", schema.ToolMessage("error: failed", "call_1"), true},
		{"tool message with ERROR", schema.ToolMessage("ERROR: failed", "call_1"), true},
		{"tool message with failed", schema.ToolMessage("failed to execute", "call_1"), true},
		{"tool message with Failed", schema.ToolMessage("Failed to execute", "call_1"), true},
		{"tool message with whitespace error", schema.ToolMessage("  error: timeout", "call_1"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isToolError(tt.msg)
			if result != tt.expected {
				t.Errorf("isToolError() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// --- Tests for toTaskEvent ---

func TestToTaskEvent(t *testing.T) {
	tests := []struct {
		name         string
		status       string
		expectedType EventType
	}{
		{"pending", "pending", EventTaskStarted},
		{"queued", "queued", EventTaskStarted},
		{"in_progress", "in_progress", EventTaskRunning},
		{"completed", "completed", EventTaskCompleted},
		{"failed", "failed", EventTaskFailed},
		{"timed_out", "timed_out", EventTaskFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := map[string]string{
				"task_id": "task-1",
				"subject": "Test Task",
				"status":  tt.status,
			}
			payloadBytes, _ := json.Marshal(payload)

			event := toTaskEvent("thread-1", string(payloadBytes), 1234567890)

			if event == nil {
				t.Fatal("expected non-nil event")
			}
			if event.Type != tt.expectedType {
				t.Errorf("expected type %v, got %v", tt.expectedType, event.Type)
			}
			if event.ThreadID != "thread-1" {
				t.Errorf("expected thread-1, got %s", event.ThreadID)
			}
			if event.Timestamp != 1234567890 {
				t.Errorf("expected timestamp 1234567890, got %d", event.Timestamp)
			}
		})
	}
}

func TestToTaskEvent_InvalidPayload(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{"invalid json", "not json"},
		{"missing task_id", `{"subject": "Test", "status": "completed"}`},
		{"missing status", `{"task_id": "task-1", "subject": "Test"}`},
		{"empty payload", `{}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := toTaskEvent("thread-1", tt.json, 1)
			if event != nil {
				t.Error("expected nil for invalid payload")
			}
		})
	}
}

// --- Tests for syncMiddlewareStateToSession ---

func TestSyncMiddlewareStateToSession_NilState(t *testing.T) {
	// Should not panic
	syncMiddlewareStateToSession(context.Background(), nil)
}

func TestSyncMiddlewareStateToSession_EmptyExtra(t *testing.T) {
	state := &basemw.State{
		ThreadID: "thread-1",
		Extra:    nil,
	}

	// Should not panic
	syncMiddlewareStateToSession(context.Background(), state)
}

func TestSyncMiddlewareStateToSession_WithExtra(t *testing.T) {
	state := &basemw.State{
		ThreadID: "thread-1",
		Extra: map[string]any{
			"task_tool_calls_count": 5,
			"clarification_request": "need more info",
			"interrupt":             true,
		},
	}

	// Should not panic
	syncMiddlewareStateToSession(context.Background(), state)
}

// --- Tests for timeUnixMilli ---

func TestTimeUnixMilli(t *testing.T) {
	before := time.Now().UnixMilli()
	result := timeUnixMilli()
	after := time.Now().UnixMilli()

	if result < before || result > after {
		t.Errorf("timeUnixMilli() = %d, expected between %d and %d", result, before, after)
	}
}

// --- Mock types for testing ---

// createMessageVariant creates an adk.MessageVariant from a schema.Message
func createMessageVariant(msg *schema.Message) *adk.MessageVariant {
	if msg == nil {
		return &adk.MessageVariant{}
	}
	return &adk.MessageVariant{
		Message: msg,
		Role:    msg.Role,
	}
}

// --- Table-driven tests for complex scenarios ---

func TestPrepareRunMessages_TableDriven(t *testing.T) {
	tests := []struct {
		name         string
		messages     []*schema.Message
		cfg          RunConfig
		expectCount  int
		firstIsSystem bool
	}{
		{
			name:         "empty messages no hints",
			messages:     nil,
			cfg:          RunConfig{},
			expectCount:  1,
			firstIsSystem: false,
		},
		{
			name:         "empty messages with plan mode",
			messages:     nil,
			cfg:          RunConfig{IsPlanMode: true},
			expectCount:  2,
			firstIsSystem: true,
		},
		{
			name:         "with messages no hints",
			messages:     []*schema.Message{schema.UserMessage("Hello")},
			cfg:          RunConfig{},
			expectCount:  1,
			firstIsSystem: false,
		},
		{
			name:         "with messages with plan mode",
			messages:     []*schema.Message{schema.UserMessage("Hello")},
			cfg:          RunConfig{IsPlanMode: true},
			expectCount:  2,
			firstIsSystem: true,
		},
		{
			name:         "with messages with subagent",
			messages:     []*schema.Message{schema.UserMessage("Hello")},
			cfg:          RunConfig{SubagentEnabled: true},
			expectCount:  2,
			firstIsSystem: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prepareRunMessages(tt.messages, tt.cfg)

			if len(result) != tt.expectCount {
				t.Errorf("expected %d messages, got %d", tt.expectCount, len(result))
			}

			if tt.firstIsSystem && len(result) > 0 && result[0].Role != schema.System {
				t.Errorf("expected first message to be system, got %v", result[0].Role)
			}
		})
	}
}

// --- Benchmark Tests ---

func BenchmarkPrepareRunMessages(b *testing.B) {
	messages := make([]*schema.Message, 10)
	for i := 0; i < 10; i++ {
		messages[i] = schema.UserMessage("Message content")
	}
	cfg := RunConfig{IsPlanMode: true, SubagentEnabled: true}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = prepareRunMessages(messages, cfg)
	}
}

func BenchmarkParseToolInputArguments(b *testing.B) {
	json := `{"key": "value", "nested": {"inner": "data"}, "array": [1, 2, 3]}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parseToolInputArguments(json)
	}
}

func BenchmarkToLegacyMessage(b *testing.B) {
	msg := schema.AssistantMessage("Hello", []schema.ToolCall{
		{ID: "call_1", Type: "function", Function: schema.FunctionCall{Name: "read", Arguments: `{}`}},
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = toLegacyMessage(msg)
	}
}

func BenchmarkFromLegacyMessage(b *testing.B) {
	input := map[string]any{
		"role":    "assistant",
		"content": "Hello",
		"tool_calls": []map[string]any{
			{"id": "call_1", "name": "read", "arguments": `{}`},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fromLegacyMessage(input)
	}
}

// --- Edge Case Tests ---

func TestParseToolInputArguments_NilJSON(t *testing.T) {
	// Test that null JSON returns empty map
	result := parseToolInputArguments("null")
	if len(result) != 0 {
		t.Errorf("expected empty map for null, got %v", result)
	}
}

func TestToLegacyMessage_NilToolCalls(t *testing.T) {
	msg := schema.AssistantMessage("Hello", nil)
	result := toLegacyMessage(msg)

	if _, ok := result["tool_calls"]; ok {
		t.Error("tool_calls should not be present when nil")
	}
}

func TestFromLegacyMessage_MissingFields(t *testing.T) {
	// Test with missing fields
	input := map[string]any{}
	result := fromLegacyMessage(input)

	if result.Role != schema.User {
		t.Error("expected default to user role")
	}
}

func TestParseViewedImages_EmptyMap(t *testing.T) {
	input := map[string]any{}
	result := parseViewedImages(input)

	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
}

func TestConvertAgentEvent_CustomizedAction(t *testing.T) {
	event := &adk.AgentEvent{
		Action: &adk.AgentAction{
			CustomizedAction: map[string]any{
				"type":    "custom_event",
				"task_id": "task-1",
			},
		},
	}

	result := convertAgentEvent(event, "thread-1")

	// Should handle customized action
	_ = result
}

func TestConvertAgentEvent_ToolMessage(t *testing.T) {
	toolMsg := schema.ToolMessage("tool result", "call_1")
	toolMsg.ToolName = "read_file"
	msgOutput := createMessageVariant(toolMsg)

	event := &adk.AgentEvent{
		Output: &adk.AgentOutput{
			MessageOutput: msgOutput,
		},
	}

	result := convertAgentEvent(event, "thread-1")

	hasToolEvent := false
	for _, ev := range result {
		if ev.Type == EventToolEvent {
			hasToolEvent = true
			payload, ok := ev.Payload.(ToolEventPayload)
			if ok {
				if payload.ToolName != "read_file" {
					t.Errorf("expected tool name 'read_file', got %s", payload.ToolName)
				}
			}
		}
	}

	if !hasToolEvent {
		t.Error("expected tool event")
	}
}

// Test for TaskToolName integration
func TestConvertAgentEvent_TaskToolEvent(t *testing.T) {
	taskJSON, _ := json.Marshal(map[string]string{
		"task_id": "task-123",
		"subject": "Test Task",
		"status":  "completed",
	})

	toolMsg := schema.ToolMessage(string(taskJSON), "call_1")
	toolMsg.ToolName = subagents.TaskToolName
	msgOutput := createMessageVariant(toolMsg)

	event := &adk.AgentEvent{
		Output: &adk.AgentOutput{
			MessageOutput: msgOutput,
		},
	}

	result := convertAgentEvent(event, "thread-1")

	// Should have both tool event and task event
	hasTaskEvent := false
	for _, ev := range result {
		if ev.Type == EventTaskCompleted {
			hasTaskEvent = true
			break
		}
	}

	if !hasTaskEvent {
		t.Error("expected task event for task tool")
	}
}

// Test for GetMessage error
func TestConvertAgentEvent_GetMessageError(t *testing.T) {
	// Create a variant with an error - use empty variant which will return error
	msgOutput := &adk.MessageVariant{}

	event := &adk.AgentEvent{
		Output: &adk.AgentOutput{
			MessageOutput: msgOutput,
		},
	}

	result := convertAgentEvent(event, "thread-1")

	// Should return empty or handle gracefully
	_ = result
}

// --- Tests for drainIter ---
// Note: drainIter is tested indirectly through Run/Resume tests
// because it requires einoruntime.EventStream which is a struct not an interface.
// Direct unit testing would require complex mock setup that's not practical.

func TestDrainIter_NilIterator(t *testing.T) {
	ch := make(chan Event, 1)
	ctx := context.Background()

	drainIter(ctx, nil, "thread-1", "run-1", ch)
	close(ch)

	// Should emit error event
	select {
	case ev := <-ch:
		if ev.Type != EventError {
			t.Errorf("expected error event, got %v", ev.Type)
		}
		if ev.ThreadID != "thread-1" {
			t.Errorf("expected thread-1, got %s", ev.ThreadID)
		}
		if ev.RunID != "run-1" {
			t.Errorf("expected run-1, got %s", ev.RunID)
		}
		payload, ok := ev.Payload.(ErrorPayload)
		if !ok {
			t.Fatal("expected ErrorPayload")
		}
		if payload.Code != ErrorCodeEmptyStream {
			t.Errorf("expected error code %s, got %s", ErrorCodeEmptyStream, payload.Code)
		}
	default:
		t.Error("expected error event from nil iterator")
	}
}

// --- Tests for Run ---

func TestRun_RequiresThreadID(t *testing.T) {
	agent := &leadAgent{}
	ctx := context.Background()
	state := &ThreadState{}
	cfg := RunConfig{ThreadID: ""}

	_, err := agent.Run(ctx, state, cfg)
	if err == nil {
		t.Error("expected error when thread_id is empty")
	}
	if !strings.Contains(err.Error(), "thread_id is required") {
		t.Errorf("expected 'thread_id is required' error, got: %v", err)
	}
}

func TestRun_NilAgent(t *testing.T) {
	var agent *leadAgent
	ctx := context.Background()
	state := &ThreadState{}
	cfg := RunConfig{ThreadID: "thread-1"}

	// Should return channel with error event
	ch, err := agent.Run(ctx, state, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read error event
	ev := <-ch
	if ev.Type != EventError {
		t.Errorf("expected error event, got %v", ev.Type)
	}
	payload, ok := ev.Payload.(ErrorPayload)
	if !ok {
		t.Fatal("expected ErrorPayload")
	}
	if payload.Code != ErrorCodeNotInitialized {
		t.Errorf("expected error code %s, got %s", ErrorCodeNotInitialized, payload.Code)
	}
}

func TestRun_NilRunner(t *testing.T) {
	agent := &leadAgent{runner: nil}
	ctx := context.Background()
	state := &ThreadState{}
	cfg := RunConfig{ThreadID: "thread-1"}

	// Should return channel with error event
	ch, err := agent.Run(ctx, state, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read error event
	ev := <-ch
	if ev.Type != EventError {
		t.Errorf("expected error event, got %v", ev.Type)
	}
	payload, ok := ev.Payload.(ErrorPayload)
	if !ok {
		t.Fatal("expected ErrorPayload")
	}
	if payload.Code != ErrorCodeNotInitialized {
		t.Errorf("expected error code %s, got %s", ErrorCodeNotInitialized, payload.Code)
	}
}

func TestRun_NilState(t *testing.T) {
	agent := &leadAgent{runner: nil}
	ctx := context.Background()
	cfg := RunConfig{ThreadID: "thread-1"}

	// Should handle nil state gracefully
	ch, err := agent.Run(ctx, nil, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read error event (from nil runner)
	ev := <-ch
	if ev.Type != EventError {
		t.Errorf("expected error event, got %v", ev.Type)
	}
}

func TestRun_WithCheckpointID(t *testing.T) {
	agent := &leadAgent{runner: nil}
	ctx := context.Background()
	state := &ThreadState{}
	cfg := RunConfig{
		ThreadID:    "thread-1",
		CheckpointID: "cp-123",
	}

	// Should accept checkpoint ID (though runner is nil)
	ch, err := agent.Run(ctx, state, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read error event (from nil runner)
	ev := <-ch
	if ev.Type != EventError {
		t.Errorf("expected error event from nil runner, got %v", ev.Type)
	}
}

func TestRun_SessionValues(t *testing.T) {
	agent := &leadAgent{runner: nil}
	ctx := context.Background()
	state := &ThreadState{
		UploadedFiles: []UploadedFile{
			{Name: "file1.txt", VirtualPath: "/tmp/file1.txt"},
		},
		ViewedImages: map[string]ViewedImageData{
			"/tmp/img.png": {Base64: "abc", MIMEType: "image/png"},
		},
		ThreadData: &ThreadDataState{
			WorkspacePath: "/workspace",
			UploadsPath:   "/uploads",
			OutputsPath:   "/outputs",
		},
	}
	cfg := RunConfig{
		ThreadID:               "thread-1",
		IsPlanMode:             true,
		SubagentEnabled:        true,
		MaxConcurrentSubagents: 5,
		AgentName:              "test-agent",
	}

	// Should accept all session values
	ch, err := agent.Run(ctx, state, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read error event (from nil runner)
	ev := <-ch
	if ev.Type != EventError {
		t.Errorf("expected error event from nil runner, got %v", ev.Type)
	}
}

// --- Tests for Resume ---

func TestResume_RequiresThreadID(t *testing.T) {
	agent := &leadAgent{}
	ctx := context.Background()
	state := &ThreadState{}
	cfg := RunConfig{ThreadID: ""}

	_, err := agent.Resume(ctx, state, cfg, "cp-123")
	if err == nil {
		t.Error("expected error when thread_id is empty")
	}
	if !strings.Contains(err.Error(), "thread_id is required") {
		t.Errorf("expected 'thread_id is required' error, got: %v", err)
	}
}

func TestResume_RequiresCheckpointID(t *testing.T) {
	agent := &leadAgent{}
	ctx := context.Background()
	state := &ThreadState{}
	cfg := RunConfig{ThreadID: "thread-1"}

	_, err := agent.Resume(ctx, state, cfg, "")
	if err == nil {
		t.Error("expected error when checkpoint_id is empty")
	}
	if !strings.Contains(err.Error(), "checkpoint_id is required") {
		t.Errorf("expected 'checkpoint_id is required' error, got: %v", err)
	}
}

func TestResume_RequiresCheckpointIDWhitespace(t *testing.T) {
	agent := &leadAgent{}
	ctx := context.Background()
	state := &ThreadState{}
	cfg := RunConfig{ThreadID: "thread-1"}

	_, err := agent.Resume(ctx, state, cfg, "   ")
	if err == nil {
		t.Error("expected error when checkpoint_id is whitespace")
	}
}

func TestResume_NilAgent(t *testing.T) {
	var agent *leadAgent
	ctx := context.Background()
	state := &ThreadState{}
	cfg := RunConfig{ThreadID: "thread-1"}

	// Should return channel with error event
	ch, err := agent.Resume(ctx, state, cfg, "cp-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read error event
	ev := <-ch
	if ev.Type != EventError {
		t.Errorf("expected error event, got %v", ev.Type)
	}
	payload, ok := ev.Payload.(ErrorPayload)
	if !ok {
		t.Fatal("expected ErrorPayload")
	}
	if payload.Code != ErrorCodeNotInitialized {
		t.Errorf("expected error code %s, got %s", ErrorCodeNotInitialized, payload.Code)
	}
}

func TestResume_NilRunner(t *testing.T) {
	agent := &leadAgent{runner: nil}
	ctx := context.Background()
	state := &ThreadState{}
	cfg := RunConfig{ThreadID: "thread-1"}

	// Should return channel with error event
	ch, err := agent.Resume(ctx, state, cfg, "cp-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read error event
	ev := <-ch
	if ev.Type != EventError {
		t.Errorf("expected error event, got %v", ev.Type)
	}
	payload, ok := ev.Payload.(ErrorPayload)
	if !ok {
		t.Fatal("expected ErrorPayload")
	}
	if payload.Code != ErrorCodeNotInitialized {
		t.Errorf("expected error code %s, got %s", ErrorCodeNotInitialized, payload.Code)
	}
}

func TestResume_NilState(t *testing.T) {
	agent := &leadAgent{runner: nil}
	ctx := context.Background()
	cfg := RunConfig{ThreadID: "thread-1"}

	// Should handle nil state gracefully
	ch, err := agent.Resume(ctx, nil, cfg, "cp-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read error event (from nil runner)
	ev := <-ch
	if ev.Type != EventError {
		t.Errorf("expected error event, got %v", ev.Type)
	}
}

// --- Tests for syncSkillsOnConfigReload ---

func TestSyncSkillsOnConfigReload_NilAgent(t *testing.T) {
	var agent *leadAgent
	err := agent.syncSkillsOnConfigReload()
	if err != nil {
		t.Errorf("expected nil error for nil agent, got: %v", err)
	}
}

func TestSyncSkillsOnConfigReload_NilSkills(t *testing.T) {
	// Save and restore original
	oldGetAppConfig := getAppConfig
	defer func() { getAppConfig = oldGetAppConfig }()

	getAppConfig = func() (*config.AppConfig, error) {
		return &config.AppConfig{}, nil
	}

	// Save and restore
	oldRegisterDefaultTools := registerDefaultTools
	defer func() { registerDefaultTools = oldRegisterDefaultTools }()

	registerDefaultTools = func(_ *config.AppConfig, _ *config.ModelConfig) error {
		return nil
	}

	agent := &leadAgent{skills: nil}
	err := agent.syncSkillsOnConfigReload()
	if err != nil {
		t.Errorf("expected nil error for nil skills, got: %v", err)
	}
}

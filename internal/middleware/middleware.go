// Package middleware defines the Middleware interface used by the GoClaw lead agent.
//
// Each middleware wraps around a single agent turn, providing Before/After hooks
// analogous to DeerFlow's AgentMiddleware.before_model / after_model hooks.
//
// Middlewares are adapted to Eino's ChatModelAgentMiddleware interface via AdaptMiddlewares
// and passed to adk.ChatModelAgentConfig.Handlers for execution.
package middleware

import (
	"context"

	"github.com/bookerbai/goclaw/internal/logging"
)

// State is the mutable conversation state passed through every middleware.
// It mirrors DeerFlow's ThreadState and carries the message history, metadata,
// and optional plan/todo information for the current turn.
//
// All middlewares read from and write to State to coordinate across the chain.
type State struct {
	// ThreadID is the stable conversation identifier (UUID).
	ThreadID string

	// Messages holds the full message history for the current turn.
	// Elements are map[string]any with keys "role" and "content".
	// Role values: "system", "human", "assistant", "tool".
	Messages []map[string]any

	// Title is the auto-generated conversation title.
	// Empty until TitleMiddleware populates it after the first exchange.
	Title string

	// MemoryFacts are the injected long-term facts (≤15) prepended to the
	// system prompt by MemoryMiddleware.Before before model invocation.
	MemoryFacts []string

	// Todos holds the active plan-mode task list managed by TodoMiddleware.
	// Each entry is map[string]any with keys "id", "content", "status".
	Todos []map[string]any

	// PlanMode indicates whether the current turn runs in plan/todo mode.
	// When true, TodoMiddleware injects the write_todos tool.
	PlanMode bool

	// TokenCount is an approximate token count of Messages.
	// Populated by SummarizationMiddleware to decide whether to compress.
	TokenCount int

	// ViewedImages holds base64-encoded images for multimodal injection.
	// Key is the image path, value contains Base64 and MIMEType.
	ViewedImages map[string]ViewedImage

	// Artifacts holds the list of presented file paths (virtual paths).
	// Each middleware or tool can add new artifacts, and they will be
	// merged and deduplicated by the reducer mechanism.
	Artifacts []string

	// Extra is an open-ended bag for middleware-specific metadata that does
	// not deserve a dedicated field on State.
	Extra map[string]any
}

// ViewedImage holds base64-encoded image data for multimodal content injection.
type ViewedImage struct {
	Base64   string
	MIMEType string
}

// ToolCall represents a single tool invocation request.
// It is passed to Middleware.WrapToolCall for interception and wrapping.
type ToolCall struct {
	// ID is the unique identifier for this tool call.
	ID string

	// Name is the tool name to invoke.
	Name string

	// Input is the tool arguments (usually JSON-decoded map).
	Input map[string]any
}

// ToolResult represents the result of a tool invocation.
// It is returned by ToolHandler after executing a tool call.
type ToolResult struct {
	// ID is the tool call ID (matches ToolCall.ID).
	ID string

	// Output is the tool return value (usually JSON-encodable).
	Output any

	// Error holds the error if the tool execution failed.
	Error error
}

// ToolHandler is the function signature for executing a tool call.
// Middlewares can intercept and wrap this handler in WrapToolCall.
type ToolHandler func(ctx context.Context, toolCall *ToolCall) (*ToolResult, error)

// Response is the output produced by the agent function (next) and passed to
// Middleware.After so middlewares can inspect or annotate the result.
type Response struct {
	// FinalMessage is the last assistant text produced during the turn.
	FinalMessage string

	// ToolCalls is the list of tool invocations made during the turn.
	// Each entry is map[string]any with keys "id", "name", "input", "output".
	ToolCalls []map[string]any

	// Error holds the error if the agent function returned a non-nil error.
	// Middlewares can inspect this in After and decide how to handle it.
	Error error

	// StateUpdates holds pending state updates from tools or middlewares.
	// These updates should be applied via reducers after the After hooks.
	// Keys are state field names (e.g., "artifacts", "viewed_images").
	StateUpdates map[string]any
}

// Middleware is the hook interface implemented by every lead-agent middleware.
//
// Lifecycle hooks (in execution order):
//  1. BeforeAgent - called once at agent start (per agent run)
//  2. Before - called before each model invocation (per iteration)
//  3. (model invocation + tool execution)
//  4. After - called after each model invocation (per iteration, reverse order)
//  5. AfterAgent - called once at agent end (per agent run, reverse order)
//
// BeforeAgent/AfterAgent mirror DeerFlow's before_agent/after_agent hooks.
// Before/After mirror DeerFlow's before_model/after_model hooks.
//
// WrapToolCall is called for each tool invocation during the agent turn.
// It allows middlewares to intercept, audit, or retry individual tool calls.
// The default implementation (MiddlewareWrapper) simply calls the handler.
//
// Name returns a stable identifier used for logging and metrics.
type Middleware interface {
	// BeforeAgent runs once at the start of an agent run.
	// Use for one-time setup like creating directories, acquiring resources.
	// Returning a non-nil error aborts the entire agent run.
	BeforeAgent(ctx context.Context, state *State) error

	// BeforeModel runs before each model invocation (per iteration).
	// Implementors may modify State (e.g. inject facts into the system prompt)
	// and return an error to abort the current iteration.
	BeforeModel(ctx context.Context, state *State) error

	// AfterModel runs after each model invocation (per iteration, reverse order).
	// Implementors may read Response (e.g. queue memory updates) but SHOULD NOT
	// fail the turn for non-critical bookkeeping errors — log and continue instead.
	AfterModel(ctx context.Context, state *State, response *Response) error

	// AfterAgent runs once at the end of an agent run (reverse order).
	// Use for one-time cleanup like releasing resources, persisting state.
	// Errors are logged but do not affect the response.
	AfterAgent(ctx context.Context, state *State, response *Response) error

	// WrapToolCall wraps a single tool call execution.
	// Middlewares can intercept tool calls for auditing, error handling, or retry.
	// The handler parameter is the next stage (usually the actual tool executor).
	// Middlewares should call handler(ctx, toolCall) to proceed with execution,
	// or return a synthetic ToolResult without calling handler to short-circuit.
	WrapToolCall(ctx context.Context, state *State, toolCall *ToolCall, handler ToolHandler) (*ToolResult, error)

	// Name returns a human-readable identifier for this middleware instance.
	Name() string
}

// MiddlewareWrapper provides a default implementation of Middleware
// that does nothing in BeforeAgent/Before/After/AfterAgent and passes through in WrapToolCall.
// Embed this in your middleware struct to avoid implementing all methods.
type MiddlewareWrapper struct{}

// BeforeAgent does nothing and returns nil.
func (MiddlewareWrapper) BeforeAgent(ctx context.Context, state *State) error { return nil }

// BeforeModel does nothing and returns nil.
func (MiddlewareWrapper) BeforeModel(ctx context.Context, state *State) error { return nil }

// AfterModel does nothing and returns nil.
func (MiddlewareWrapper) AfterModel(ctx context.Context, state *State, response *Response) error {
	return nil
}

// AfterAgent does nothing and returns nil.
func (MiddlewareWrapper) AfterAgent(ctx context.Context, state *State, response *Response) error {
	return nil
}

// WrapToolCall passes through to the handler without modification.
func (MiddlewareWrapper) WrapToolCall(ctx context.Context, state *State, toolCall *ToolCall, handler ToolHandler) (*ToolResult, error) {
	return handler(ctx, toolCall)
}

// Name returns "wrapper" as a placeholder. Override in your middleware.
func (MiddlewareWrapper) Name() string { return "wrapper" }

// Reducer is a function that merges a new value into an existing state field.
// It's used to handle state updates from multiple sources (middlewares, tools)
// in a controlled way, similar to LangGraph's reducer pattern.
//
// For example, Artifacts and ViewedImages use reducers to merge and deduplicate
// values from multiple tool calls in a single turn.
type Reducer func(existing, new any) any

// typeName returns a readable type name for logging.
func typeName(v any) string {
	if v == nil {
		return "nil"
	}
	switch v.(type) {
	case string:
		return "string"
	case int, int64, float64:
		return "number"
	case bool:
		return "bool"
	case []string:
		return "[]string"
	case []any:
		return "[]any"
	case map[string]any:
		return "map[string]any"
	case map[string]ViewedImage:
		return "map[string]ViewedImage"
	default:
		return "unknown"
	}
}

// mapViewToViewedImage converts a map[string]any to ViewedImage.
// Returns nil if the conversion fails.
func mapViewToViewedImage(m map[string]any) *ViewedImage {
	img := &ViewedImage{}
	if base64, ok := m["base64"].(string); ok {
		img.Base64 = base64
	}
	if mimeType, ok := m["mime_type"].(string); ok {
		img.MIMEType = mimeType
	}
	if img.Base64 == "" && img.MIMEType == "" {
		return nil
	}
	return img
}

// MergeArtifacts is a reducer that merges and deduplicates artifact paths.
// It preserves the order of first occurrence and removes duplicates.
//
// This is the Go equivalent of DeerFlow's merge_artifacts reducer.
func MergeArtifacts(existing, new any) any {
	var existingList []string
	var newList []string

	if existing != nil {
		switch e := existing.(type) {
		case []string:
			existingList = e
		case []any:
			// Handle JSON-unmarshaled arrays
			existingList = make([]string, 0, len(e))
			for i, item := range e {
				if s, ok := item.(string); ok {
					existingList = append(existingList, s)
				} else {
					logging.Warn("MergeArtifacts: non-string item in existing array", "index", i, "type", typeName(item))
				}
			}
		default:
			logging.Warn("MergeArtifacts: unexpected existing type", "type", typeName(existing))
		}
	}
	if new != nil {
		switch n := new.(type) {
		case []string:
			newList = n
		case []any:
			// Handle JSON-unmarshaled arrays
			newList = make([]string, 0, len(n))
			for i, item := range n {
				if s, ok := item.(string); ok {
					newList = append(newList, s)
				} else {
					logging.Warn("MergeArtifacts: non-string item in new array", "index", i, "type", typeName(item))
				}
			}
		default:
			logging.Warn("MergeArtifacts: unexpected new type", "type", typeName(new))
		}
	}

	if existingList == nil {
		return newList
	}
	if newList == nil {
		return existingList
	}

	// Use map to deduplicate while preserving order
	seen := make(map[string]bool)
	result := make([]string, 0, len(existingList)+len(newList))

	for _, item := range existingList {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	for _, item := range newList {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}

	return result
}

// MergeViewedImages is a reducer that merges viewed images dictionaries.
// Special case: if new is an empty map, it clears all viewed images.
//
// This is the Go equivalent of DeerFlow's merge_viewed_images reducer.
func MergeViewedImages(existing, new any) any {
	var existingMap map[string]ViewedImage
	var newMap map[string]ViewedImage

	if existing != nil {
		switch e := existing.(type) {
		case map[string]ViewedImage:
			existingMap = e
		case map[string]any:
			// Handle JSON-unmarshaled maps
			existingMap = make(map[string]ViewedImage)
			for k, v := range e {
				if img, ok := v.(ViewedImage); ok {
					existingMap[k] = img
				} else if m, ok := v.(map[string]any); ok {
					// Try to convert from map[string]any
					if img := mapViewToViewedImage(m); img != nil {
						existingMap[k] = *img
					}
				} else {
					logging.Warn("MergeViewedImages: unexpected value type in existing map", "key", k, "type", typeName(v))
				}
			}
		default:
			logging.Warn("MergeViewedImages: unexpected existing type", "type", typeName(existing))
		}
	}
	if new != nil {
		switch n := new.(type) {
		case map[string]ViewedImage:
			newMap = n
		case map[string]any:
			// Handle JSON-unmarshaled maps
			newMap = make(map[string]ViewedImage)
			for k, v := range n {
				if img, ok := v.(ViewedImage); ok {
					newMap[k] = img
				} else if m, ok := v.(map[string]any); ok {
					// Try to convert from map[string]any
					if img := mapViewToViewedImage(m); img != nil {
						newMap[k] = *img
					}
				} else {
					logging.Warn("MergeViewedImages: unexpected value type in new map", "key", k, "type", typeName(v))
				}
			}
		default:
			logging.Warn("MergeViewedImages: unexpected new type", "type", typeName(new))
		}
	}

	if existingMap == nil {
		return newMap
	}
	if newMap == nil {
		return existingMap
	}

	// Special case: empty map means clear all
	if len(newMap) == 0 {
		return make(map[string]ViewedImage)
	}

	// Merge: new values override existing for same keys
	result := make(map[string]ViewedImage, len(existingMap)+len(newMap))
	for k, v := range existingMap {
		result[k] = v
	}
	for k, v := range newMap {
		result[k] = v
	}

	return result
}

// ApplyReducers applies all registered reducers to merge pending updates into state.
// This should be called after each middleware's After hook to ensure state consistency.
//
// The current implementation handles:
// - Artifacts: merge and deduplication
// - ViewedImages: merge with special clear semantics
func ApplyReducers(state *State, pendingUpdates map[string]any) {
	if state == nil || pendingUpdates == nil {
		return
	}

	// Apply Artifacts reducer
	if artifacts, ok := pendingUpdates["artifacts"]; ok {
		merged := MergeArtifacts(state.Artifacts, artifacts)
		if m, ok := merged.([]string); ok {
			state.Artifacts = m
		}
	}

	// Apply ViewedImages reducer
	if viewedImages, ok := pendingUpdates["viewed_images"]; ok {
		merged := MergeViewedImages(state.ViewedImages, viewedImages)
		if m, ok := merged.(map[string]ViewedImage); ok {
			state.ViewedImages = m
		}
	}
}

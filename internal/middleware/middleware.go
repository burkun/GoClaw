// Package middleware defines the Middleware interface and Chain used by the GoClaw lead agent.
//
// Each middleware wraps around a single agent turn, providing Before/After hooks
// analogous to DeerFlow's AgentMiddleware.before_model / after_model hooks.
//
// Usage:
//
//	chain := middleware.NewChain(
//	    memoryMW,
//	    titleMW,
//	    todoMW,
//	)
//	err := chain.Run(ctx, state, agentFunc)
package middleware

import "context"

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

	// Extra is an open-ended bag for middleware-specific metadata that does
	// not deserve a dedicated field on State.
	Extra map[string]any
}

// ViewedImage holds base64-encoded image data for multimodal content injection.
type ViewedImage struct {
	Base64   string
	MIMEType string
}

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
}

// Middleware is the hook interface implemented by every lead-agent middleware.
//
// Before is called before the agent's model invocation. Implementors may
// modify State (e.g. inject facts into the system prompt) and return an error
// to abort the turn.
//
// After is called after the agent's model invocation completes (or errors out).
// Implementors may read Response (e.g. queue memory updates) but SHOULD NOT
// fail the turn for non-critical bookkeeping errors — log and continue instead.
//
// Name returns a stable identifier used for logging and metrics.
type Middleware interface {
	// Before runs before the agent model invocation.
	// ctx may carry per-request values (deadlines, trace IDs).
	// Returning a non-nil error aborts the turn; the Chain stops and propagates it.
	Before(ctx context.Context, state *State) error

	// After runs after the agent model invocation regardless of success/failure.
	// Returning a non-nil error is logged but does NOT prevent the Response
	// from being delivered to the caller — it only signals a bookkeeping issue.
	After(ctx context.Context, state *State, response *Response) error

	// Name returns a human-readable identifier for this middleware instance.
	Name() string
}

// Chain executes a slice of Middlewares in order, wrapping a core agent function.
//
// Execution order:
//
//	Before[0] → Before[1] → … → Before[N-1] → next() → After[N-1] → … → After[0]
//
// If any Before returns an error, the chain stops immediately; no subsequent
// Befores or the core function are called, and all already-called After hooks
// are still invoked (in reverse) to allow cleanup.
type Chain struct {
	middlewares []Middleware
}

// NewChain constructs a Chain from the provided middlewares.
// Middlewares execute in the given order for Before, and in reverse for After.
func NewChain(middlewares ...Middleware) *Chain {
	return &Chain{middlewares: middlewares}
}

// Run executes the middleware chain around the provided next function.
//
// The next function represents the core agent work (model invocation + tool
// execution). It receives the (possibly mutated) State and returns a Response.
//
// Run guarantees that every middleware whose Before was called will also have
// its After called, even if next returns an error.
func (c *Chain) Run(ctx context.Context, state *State, next func(ctx context.Context, state *State) (*Response, error)) error {
	// TODO: Track which middlewares have run their Before, so After can be
	//       called in reverse order even when Before fails mid-chain.
	//
	// Step 1: Run Before hooks in forward order.
	//         If middleware[i].Before returns an error, skip remaining Befores
	//         and jump to step 3 (After for middlewares 0..i-1 in reverse).
	//
	// Step 2: Call next(ctx, state) to invoke the core agent logic.
	//         Capture the Response and any error.
	//
	// Step 3: Run After hooks in REVERSE order for all middlewares that
	//         successfully ran their Before.
	//         Collect After errors but do not abort; log them and continue.
	//
	// Step 4: Return the error from step 2 (next's error) if non-nil,
	//         otherwise return nil. After errors are logged but not returned.

	resp := &Response{}
	var runErr error

	// --- Step 1: Before hooks ---
	ranUpTo := -1
	for i, mw := range c.middlewares {
		if err := mw.Before(ctx, state); err != nil {
			// TODO: log "middleware %s Before failed: %v", mw.Name(), err
			runErr = err
			ranUpTo = i - 1
			goto afterHooks
		}
		ranUpTo = i
	}

	// --- Step 2: Core agent invocation ---
	{
		var err error
		resp, err = next(ctx, state)
		if err != nil {
			runErr = err
			resp = &Response{Error: err}
		}
	}

afterHooks:
	// --- Step 3: After hooks (reverse order, up to ranUpTo) ---
	for i := ranUpTo; i >= 0; i-- {
		mw := c.middlewares[i]
		if err := mw.After(ctx, state, resp); err != nil {
			// TODO: log "middleware %s After failed (non-fatal): %v", mw.Name(), err
			_ = err // After errors are non-fatal
		}
	}

	return runErr
}

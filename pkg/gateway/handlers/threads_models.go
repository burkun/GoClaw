package handlers

// RunThreadRequest is the JSON body accepted by POST /api/threads/:thread_id/runs.
type RunThreadRequest struct {
	// Input is the user message or structured input for the agent.
	Input any `json:"input"`
	// Config holds optional run-level overrides (model, tools, etc.).
	Config map[string]any `json:"config,omitempty"`
	// Metadata is passed through to the agent's ThreadState for custom use.
	Metadata map[string]any `json:"metadata,omitempty"`
	// CheckpointID resumes from an existing checkpoint when provided.
	CheckpointID string `json:"checkpoint_id,omitempty"`
	// LastEventID is the last event ID received by the client for SSE resume.
	// If provided, the server will attempt to resume from this event.
	LastEventID string `json:"last_event_id,omitempty"`
}

// SSEEvent is the unified envelope for all Server-Sent Events.
// Every event sent over the stream uses this structure.
type SSEEvent struct {
	// Type classifies the event: "message_delta", "tool_event", "completed", or "error".
	Type string `json:"type"`
	// ThreadID echoes the thread identifier from the request path.
	ThreadID string `json:"thread_id"`
	// RunID identifies this run instance.
	RunID string `json:"run_id,omitempty"`
	// CheckpointID is used for interruption persistence and resume.
	CheckpointID string `json:"checkpoint_id,omitempty"`
	// EventID is a unique identifier for this event, used for SSE resume.
	EventID string `json:"event_id,omitempty"`
	// Payload holds event-specific data (delta text, tool args, error details, etc.).
	Payload any `json:"payload"`
	// Timestamp is the UTC Unix millisecond at which the event was generated.
	Timestamp int64 `json:"timestamp"`
}

// ThreadMetadata contains basic thread information.
type ThreadMetadata struct {
	ThreadID  string         `json:"thread_id"`
	Title     string         `json:"title,omitempty"`
	Status    string         `json:"status"`
	CreatedAt int64          `json:"created_at"`
	UpdatedAt int64          `json:"updated_at"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Values    map[string]any `json:"values,omitempty"`
}

// ThreadStateResponse contains the current thread state from checkpoint.
type ThreadStateResponse struct {
	ChannelValues map[string]any `json:"channel_values"`
	CheckpointID  string         `json:"checkpoint_id,omitempty"`
	Next          []string       `json:"next,omitempty"`
	Tasks         []any          `json:"tasks,omitempty"`
}

// HistoryEntry represents a single checkpoint history entry.
type HistoryEntry struct {
	CheckpointID string `json:"checkpoint_id"`
	Timestamp    int64  `json:"timestamp"`
	ParentID     string `json:"parent_id,omitempty"`
}

// RunEntry represents a run record for list runs.
type RunEntry struct {
	RunID     string `json:"run_id"`
	ThreadID  string `json:"thread_id"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
}

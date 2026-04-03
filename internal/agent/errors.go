package agent

// Error codes for event error payloads, following the format "agent/<reason>".
// These constants ensure consistent error handling across all agent paths.
const (
	// ErrorCodeRunFailed is emitted when the agent runtime encounters an error during execution.
	ErrorCodeRunFailed = "agent/run_error"

	// ErrorCodeInterrupted is emitted when a run is explicitly cancelled or interrupted.
	ErrorCodeInterrupted = "agent/interrupted"

	// ErrorCodeEmptyStream is emitted when the event stream is unexpectedly nil or empty.
	ErrorCodeEmptyStream = "agent/empty_stream"

	// ErrorCodeContextCancelled is emitted when the request context is cancelled.
	ErrorCodeContextCancelled = "agent/context_cancelled"

	// ErrorCodeNotInitialized is emitted when the agent is not properly initialized.
	ErrorCodeNotInitialized = "agent/not_initialized"
)
